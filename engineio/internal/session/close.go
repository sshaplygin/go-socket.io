package session

import (
	"io"
	"log/slog"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// The reasons of the "engineio: session close" record.
const (
	reasonTransportClose = string(ReasonTransportClose)
	reasonPingTimeout    = string(ReasonPingTimeout)
	reasonTransportError = string(ReasonTransportError)
	reasonForcedClose    = string(ReasonForcedClose)
	reasonShutdown       = string(ReasonServerShutdown)
)

type closeCause struct {
	reason string
	err    error // set for reasonTransportError only
}

// Close starts an orderly polling close after accepted writes. It returns while
// waiting for a poll; the transport remains closing for at most 30 seconds.
// Other transports and failed sessions close immediately. The first cause wins.
func (s *Session) Close() error {
	return s.close(reasonForcedClose)
}

// Abort closes the transport immediately, interrupting an orderly Close already
// waiting for a polling request. It preserves the first close cause.
func (s *Session) Abort() error { return s.closeMode(reasonForcedClose, true) }

// Shutdown closes the transport immediately for server shutdown. It interrupts
// an orderly Close and preserves a previously observed close cause.
func (s *Session) Shutdown() error { return s.closeMode(reasonShutdown, true) }

func (s *Session) close(reason string) error {
	return s.closeMode(reason, false)
}

func (s *Session) closeMode(reason string, abort bool) error {
	s.observe(reason, nil)
	s.upgradeLocker.Lock()
	first, opened := !s.closed, s.opened
	s.closed = true
	conn, probe := s.conn, s.probe
	if first {
		close(s.stopWrites)
	}
	s.upgradeLocker.Unlock()
	c := s.cause.Load()
	graceful, canDrain := conn.(interface{ CloseGracefully() error })
	orderly := canDrain && !opened.IsZero() && c.reason == reasonForcedClose && !abort
	if !first {
		// The first closer owns synchronous Error notification and will close
		// the failed transport immediately afterwards. A hook may itself call
		// Close, Abort or Server.Close; waiting here would wait for that callback
		// to return to this same close operation.
		if c.reason == reasonTransportError && s.hooks != nil && s.hooks.Error != nil {
			select {
			case <-s.notified:
			default:
				return nil
			}
		}
		if abort {
			<-s.notified
			s.closeTransport(conn)
		} else if orderly {
			return nil
		}
		<-s.closeDone
		return s.closeErr
	}
	if c.reason == reasonTransportError && s.hooks != nil && s.hooks.Error != nil {
		s.hooks.Error(s, c.err)
	}
	close(s.notified)
	if !opened.IsZero() {
		args := []any{"reason", c.reason, slog.Duration("duration", time.Since(opened))}
		if c.err != nil {
			args = append(args, "err", c.err)
		}
		s.logger().Debug("engineio: session close", args...)
	}
	if probe != nil {
		_ = probe.Close()
	}
	if orderly {
		go func() {
			defer close(s.closeDone)
			timer := time.AfterFunc(s.closeTimeout, func() { s.closeTransport(conn) })
			defer timer.Stop()
			select {
			case <-s.transportDone:
			case <-s.writeGate:
				// The token orders CLOSE after an already admitted frame. An
				// abort or the timer also releases a blocked transport writer.
				if err := graceful.CloseGracefully(); err != nil {
					s.logger().Debug("engineio: orderly close ended", "err", err)
				}
				s.writeGate <- struct{}{}
			}
			s.closeTransport(conn)
		}()
		return nil
	}
	s.closeTransport(conn)
	close(s.closeDone)
	return s.closeErr
}

func (s *Session) closeTransport(conn transport.Conn) {
	s.transportOnce.Do(func() {
		s.closeErr = conn.Close()
		s.upgradeLocker.Lock()
		s.transportClosed = true
		s.upgradeLocker.Unlock()
		close(s.transportDone)
	})
}

// observe records reason as the close cause unless one was observed before.
func (s *Session) observe(reason string, err error) {
	s.cause.CompareAndSwap(nil, &closeCause{reason, err})
}

// fail observes a non-nil err of a transport read or write as "ping timeout" once the
// PingTimeout deadline has passed, as "transport error" before, and returns it.
func (s *Session) fail(err error) error {
	if err != nil && time.Now().UnixNano() >= s.deadline.Load() {
		s.observe(reasonPingTimeout, nil)
	} else if err != nil {
		s.observe(reasonTransportError, err)
	}
	if err != nil {
		if _, synthetic := err.(*CloseError); !synthetic {
			s.upgradeLocker.RLock()
			conn, closed := s.conn, s.closed
			s.upgradeLocker.RUnlock()
			if closed && s.cause.Load().reason == reasonForcedClose {
				s.closeTransport(conn)
			}
		}
	}
	return err
}

// closeError returns the immutable first cause; close records it before publishing closed.
func (s *Session) closeError() *CloseError {
	c := s.cause.Load()
	if c == nil {
		return &CloseError{Reason: ReasonForcedClose}
	}
	err := c.err
	if c.reason == reasonTransportClose {
		err = io.EOF
	}
	return &CloseError{Reason: CloseReason(c.reason), Err: err}
}

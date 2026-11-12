package session

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// logTooLarge logs a message past the limit of a built-in transport, which ends the
// session with a transport error and no other WARN record.
func logTooLarge(log *slog.Logger, err error) {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		log.Warn("engineio: message too large", "err", fmt.Errorf("limit %d bytes: %w", limit.Limit, err))
	}
}

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

// closingTransport publishes the final connection without requiring a failing
// frame writer to acquire upgradeLocker. A closed session cannot upgrade.
type closingTransport struct {
	conn transport.Conn
}

// Close starts an orderly polling close after accepted writes. It returns while
// waiting for a poll and while the poll response carrying CLOSE is written; the
// transport remains closing, and the session live, until that write ends, until
// Abort or Shutdown, or for at most 30 seconds. Other transports and failed
// sessions close immediately. A websocket first receives a close frame (see
// Abort). The first cause wins.
func (s *Session) Close() error {
	return s.close(reasonForcedClose)
}

// Abort closes the transport immediately, interrupting an orderly Close already
// waiting for a polling request. A websocket is dropped without the close frame
// that Close and Shutdown send, also while one is being written. It preserves
// the first close cause.
func (s *Session) Abort() error { return s.closeMode(reasonForcedClose, true, true) }

// Shutdown closes the transport immediately for server shutdown. It interrupts
// an orderly Close and preserves a previously observed close cause.
func (s *Session) Shutdown() error { return s.closeMode(reasonShutdown, true, false) }

func (s *Session) close(reason string) error {
	return s.closeMode(reason, false, false)
}

// closeMode closes the session. abort ends it without an orderly drain; immediate
// also drops the transports before anything else, without a close frame.
func (s *Session) closeMode(reason string, abort, immediate bool) error {
	s.observe(reason, nil)
	s.upgradeLocker.Lock()
	first, opened := !s.closed, s.opened
	s.closed = true
	conn, probe := s.conn, s.probe
	if probe == conn {
		// The upgrade has switched to the probe, which stays named s.probe until
		// the upgrade goroutine returns (it still runs Hooks.Upgrade and closes the
		// polling connection). It is the session's transport now and closes as one.
		probe = nil
	}
	if first {
		s.closing.Store(&closingTransport{conn: conn})
		close(s.stopWrites)
	}
	s.upgradeLocker.Unlock()
	graceful, canDrain := conn.(interface{ CloseGracefully() error })
	if immediate && !canDrain {
		// A transport that drains aborts through abortTransport below. The others
		// are cut here, ahead of a Close that may be writing a close frame.
		abortConn(conn)
	}
	if probe != nil {
		// An upgrade candidate, one the session has not switched to, is not an
		// established transport: it is dropped without a close frame in every mode,
		// and the closer does not wait for its peer. The upgrade goroutine may still
		// be configuring the probe, and a close that announced itself would read
		// the peer's frames from it.
		abortConn(probe)
	}
	c := s.cause.Load()
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
			s.abortTransport(conn)
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
		logTooLarge(s.logger(), c.err)
	}
	if probe != nil {
		_ = probe.Close()
	}
	if orderly {
		go func() {
			defer s.finishClose()
			timer := time.AfterFunc(s.closeTimeout, func() { s.abortTransport(conn) })
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
			s.closeTransport(conn, false)
			if drain, ok := conn.(interface{ Drained() <-chan struct{} }); ok {
				// The response carrying CLOSE may still be written. The timer or an
				// abort expires it where the transport can, and ends the wait anyway.
				select {
				case <-drain.Drained():
				case <-s.aborted:
				}
			}
		}()
		return nil
	}
	s.closeTransport(conn, true)
	s.finishClose()
	return s.closeErr
}

// closeTransport may run on a failing frame reader or writer. Its brief write
// lock is safe because no holder of upgradeLocker, read or write, waits for a
// frame reader or writer (see upgradeLocker). A first close that is not the end
// of an orderly one also aborts a transport that distinguishes the two (the
// polling transport then expires a response write in progress instead of
// delivering CLOSE); a connection an upgrade retired is aborted by every close.
func (s *Session) closeTransport(conn transport.Conn, abort bool) {
	s.transportOnce.Do(func() {
		if _, drains := conn.(interface{ CloseGracefully() error }); !drains {
			if c := s.cause.Load(); c != nil && c.reason == reasonTransportError {
				// A failed transport has no use for a close frame, and a peer
				// that stopped reading would delay it.
				abortConn(conn)
			}
		}
		s.closeErr = conn.Close()
		if abort {
			abortConn(conn)
		}
		s.upgradeLocker.Lock()
		s.transportClosed = true
		s.upgradeLocker.Unlock()
		close(s.transportDone)
	})
	if retired := s.retired.Load(); retired != nil {
		abortConn(retired.conn)
	}
}

// abortTransport is Abort, Shutdown and the orderly close's timer: it aborts
// conn even after the orderly close has closed it, expiring a response write
// left in progress. fail does not get here (see failRead).
func (s *Session) abortTransport(conn transport.Conn) {
	s.closeTransport(conn, true)
	abortConn(conn)
	s.abortOnce.Do(func() { close(s.aborted) })
}

func abortConn(conn transport.Conn) {
	if aborter, ok := conn.(interface{ Abort() error }); ok {
		_ = aborter.Abort()
	}
}

// observe records reason as the close cause unless one was observed before.
func (s *Session) observe(reason string, err error) {
	s.cause.CompareAndSwap(nil, &closeCause{reason, err})
}

// fail observes a non-nil err of a transport read or write as "ping timeout" once the
// deadline has expired (see timedOut), as "transport error" otherwise, and returns it.
func (s *Session) fail(err error) error {
	if timedOut(err) {
		s.observe(reasonPingTimeout, nil)
	} else if err != nil {
		s.observe(reasonTransportError, err)
	}
	if err != nil {
		if _, synthetic := err.(*CloseError); !synthetic {
			if closing := s.closing.Load(); closing != nil && s.cause.Load().reason == reasonForcedClose {
				s.closeTransport(closing.conn, true)
			}
		}
	}
	return err
}

// failRead is fail for an open frame reader. A read that fails while the
// session closes on Close failed because of that close; like readerCloseError
// for NextReader, it returns (the frame's own error) only once the close has
// finished, so the caller's cleanup cannot abort the CLOSE still being delivered.
func (s *Session) failRead(err error) error {
	err = s.fail(err)
	if _, synthetic := err.(*CloseError); err != nil && !synthetic {
		if closing := s.closing.Load(); closing != nil && s.cause.Load().reason == reasonForcedClose {
			<-s.closeDone
		}
	}
	return err
}

// timedOut reports whether err is an expired transport deadline: it matches
// os.ErrDeadlineExceeded, or an error in its chain reports Timeout, as the
// deadline errors of gorilla/websocket and of the polling payload do. The
// transports expire deadlines on the monotonic clock; the wall clock can disagree.
func timedOut(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.Is(err, os.ErrDeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout()
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

// finishClose retires the session synchronously instead of keeping an idle
// registry watcher goroutine for the lifetime of each connection.
func (s *Session) finishClose() {
	if s.onClosed != nil {
		s.onClosed()
	}
	close(s.closeDone)
}

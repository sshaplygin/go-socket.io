package session

import (
	"errors"
	"io"
	"log/slog"
	"os"
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

// closingTransport publishes the final connection without requiring a failing
// frame writer to acquire upgradeLocker. A closed session cannot upgrade.
type closingTransport struct {
	conn transport.Conn
}

// Close starts an orderly polling close after accepted writes. It returns while
// waiting for a poll and while the poll response carrying CLOSE is written; the
// transport remains closing, and the session live, for at most 30 seconds.
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
		s.closing.Store(&closingTransport{conn: conn})
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
				// The response carrying CLOSE is still being written; the timer
				// aborts it. Until then Shutdown and Abort can still reach it.
				<-drain.Drained()
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
// frame reader or writer (see upgradeLocker). Every first close but the end of
// an orderly one also aborts a transport that distinguishes the two: the
// polling transport then expires a response write in progress instead of
// letting it deliver CLOSE under the session's write deadline. A connection an
// upgrade retired has nothing left to deliver and is aborted by every close.
func (s *Session) closeTransport(conn transport.Conn, abort bool) {
	s.transportOnce.Do(func() {
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
// conn even after the orderly close has closed it, so a response write it left
// in progress is expired. A failing frame reader or writer (fail) does not get
// here: the read that Close itself expired must not interrupt CLOSE.
func (s *Session) abortTransport(conn transport.Conn) {
	s.closeTransport(conn, true)
	abortConn(conn)
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
// session closes on Close failed because that close expired the request I/O it
// waited on; like readerCloseError for NextReader, it returns only once the
// close has finished, so that the caller's cleanup cannot abort an orderly
// close still delivering CLOSE. The frame keeps its own error.
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

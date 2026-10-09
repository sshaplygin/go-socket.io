package session

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/internal"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/payload"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/logger"
)

// Pauser is connection which can be paused and resumes.
type Pauser interface {
	Pause()
	Resume()
}

type Session struct {
	conn      transport.Conn
	params    transport.ConnParameters
	transport string
	baseLog   *slog.Logger                // logger with the sid attribute
	log       atomic.Pointer[slog.Logger] // baseLog plus the current transport

	context interface{}

	upgradeLocker sync.RWMutex // guards conn, transport, closed and opened
	closed        bool         // set by Close; a closed session is never upgraded
	opened        time.Time    // set by InitSession; a session never opened logs no close

	cause    atomic.Pointer[closeCause] // the first close cause observed
	deadline atomic.Int64               // UnixNano of the PingTimeout deadline of conn
}

// The reasons of the "engineio: session close" record.
const (
	reasonTransportClose = "transport close"
	reasonPingTimeout    = "ping timeout"
	reasonTransportError = "transport error"
	reasonForcedClose    = "forced close"
	reasonShutdown       = "server shutting down"
)

type closeCause struct {
	reason string
	err    error // set for reasonTransportError only
}

func init() {
	internal.Shutdown = func(s io.Closer) error { return s.(*Session).close(reasonShutdown) }
}

// New creates a session over conn. log receives errors the session cannot
// return to a caller, with sid and transport attributes added; the transport
// attribute follows upgrades. nil means logger.Log.
func New(conn transport.Conn, sid, transport string, params transport.ConnParameters, log *slog.Logger) (*Session, error) {
	params.SID = sid
	baseLog := logger.Wrap(log).With("sid", sid)

	ses := &Session{
		transport: transport,
		conn:      conn,
		params:    params,
		baseLog:   baseLog,
	}
	ses.log.Store(baseLog.With("transport", transport))

	if err := ses.setDeadline(); err != nil {
		if closeErr := ses.Close(); closeErr != nil {
			ses.logger().Debug("engineio: session close failed", "err", closeErr)
		}

		return nil, err
	}

	return ses, nil
}

func (s *Session) SetContext(v interface{}) {
	s.context = v
}

func (s *Session) Context() interface{} {
	return s.context
}

func (s *Session) ID() string {
	return s.params.SID
}

func (s *Session) Transport() string {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()

	return s.transport
}

// Close closes the session. Its close record names the first cause observed, or
// "forced close".
func (s *Session) Close() error {
	return s.close(reasonForcedClose)
}

func (s *Session) close(reason string) error {
	s.observe(reason, nil)
	s.upgradeLocker.Lock()
	first, opened := !s.closed, s.opened
	s.closed = true
	conn := s.conn
	s.upgradeLocker.Unlock()

	if first && !opened.IsZero() {
		c := s.cause.Load()
		args := []any{"reason", c.reason, slog.Duration("duration", time.Since(opened))}
		if c.err != nil {
			args = append(args, "err", c.err)
		}
		s.logger().Debug("engineio: session close", args...)
	}
	return conn.Close()
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
	return err
}

// frameReader and frameWriter observe the failures of a frame; io.EOF ends a frame.
type frameReader struct {
	io.ReadCloser
	s *Session
}

func (r frameReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err == io.EOF {
		return n, err
	}
	return n, r.s.fail(err)
}

func (r frameReader) Close() error { return r.s.fail(r.ReadCloser.Close()) }

type frameWriter struct {
	io.WriteCloser
	s *Session
}

func (w frameWriter) Write(p []byte) (int, error) {
	n, err := w.WriteCloser.Write(p)
	return n, w.s.fail(err)
}

func (w frameWriter) Close() error { return w.s.fail(w.WriteCloser.Close()) }

// NextReader attempts to obtain a ReadCloser from the session's connection.
// When finished writing, the caller MUST Close the ReadCloser to unlock the
// connection's FramerReader.
func (s *Session) NextReader() (frame.Type, io.ReadCloser, error) {
	for {
		ft, pt, r, err := s.nextReader()
		if err != nil {
			_ = s.fail(err)
			if closeErr := s.Close(); closeErr != nil {
				s.logger().Debug("engineio: session close failed", "err", closeErr)
			}

			return 0, nil, err
		}

		switch pt {
		case packet.PING:
			// Respond to a ping with a pong.
			err := func() error {
				w, err := s.nextWriter(ft, packet.PONG)
				if err != nil {
					return err
				}
				// echo
				_, err = io.Copy(w, r)
				// unlocks the wrapped connection's FrameWriter
				if closeErr := s.fail(w.Close()); closeErr != nil {
					s.logger().Debug("engineio: close writer failed", "err", closeErr)
				}

				// unlocks the wrapped connection's FrameReader
				if closeErr := s.fail(r.Close()); closeErr != nil {
					s.logger().Debug("engineio: close reader failed", "err", closeErr)
				}

				return err
			}()

			if s.fail(err) != nil {
				if closeErr := s.Close(); closeErr != nil {
					s.logger().Debug("engineio: session close failed", "err", closeErr)
				}

				return 0, nil, err
			}
			// Read another frame.
			if err := s.fail(s.setDeadline()); err != nil {
				if closeErr := s.Close(); closeErr != nil {
					s.logger().Debug("engineio: session close failed", "err", closeErr)
				}

				return 0, nil, err
			}

		case packet.CLOSE:
			s.observe(reasonTransportClose, nil)
			// unlocks the wrapped connection's FrameReader
			if err = r.Close(); err != nil {
				s.logger().Debug("engineio: close reader failed", "err", err)
			}

			if err = s.Close(); err != nil {
				s.logger().Debug("engineio: session close failed", "err", err)
			}

			return 0, nil, io.EOF

		case packet.MESSAGE:
			// Caller must Close the ReadCloser to unlock the connection's
			// FrameReader when finished reading.
			return ft, frameReader{r, s}, nil

		default:
			// Unknown packet type. Close reader and try again.
			if err = s.fail(r.Close()); err != nil {
				s.logger().Debug("engineio: close reader failed", "err", err)
			}
		}
	}
}

func (s *Session) URL() url.URL {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()

	return s.conn.URL()
}

func (s *Session) LocalAddr() net.Addr {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()

	return s.conn.LocalAddr()
}

func (s *Session) RemoteAddr() net.Addr {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()

	return s.conn.RemoteAddr()
}

func (s *Session) RemoteHeader() http.Header {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()

	return s.conn.RemoteHeader()
}

// NextWriter attempts to obtain a WriteCloser from the session's connection.
// When finished writing, the caller MUST Close the WriteCloser to unlock the
// connection's FrameWriter.
func (s *Session) NextWriter(typ frame.Type) (io.WriteCloser, error) {
	w, err := s.nextWriter(typ, packet.MESSAGE)
	if s.fail(err) != nil {
		return nil, err
	}
	return frameWriter{w, s}, nil
}

func (s *Session) Upgrade(transport string, conn transport.Conn) {
	go s.upgrading(transport, conn)
}

func (s *Session) InitSession() error {
	w, err := s.nextWriter(frame.String, packet.OPEN)
	if err != nil {
		if closeErr := s.Close(); closeErr != nil {
			s.logger().Debug("engineio: session close failed", "err", closeErr)
		}

		return err
	}

	if _, err := s.params.WriteTo(w); err != nil {
		if closeErr := w.Close(); closeErr != nil {
			s.logger().Debug("engineio: close writer failed", "err", closeErr)
		}

		if closeErr := s.Close(); closeErr != nil {
			s.logger().Debug("engineio: session close failed", "err", closeErr)
		}

		return err
	}

	if err := w.Close(); err != nil {
		if closeErr := s.Close(); closeErr != nil {
			s.logger().Debug("engineio: session close failed", "err", closeErr)
		}

		return err
	}

	// The open record is logged under the lock: close reads opened under it, so a
	// concurrent close cannot log the session's close record before its open record.
	s.upgradeLocker.Lock()
	defer s.upgradeLocker.Unlock()
	if !s.closed {
		s.opened = time.Now()
		s.logger().Debug("engineio: session open", "remote_addr", fmt.Sprint(s.conn.RemoteAddr()))
	}
	return nil
}

func (s *Session) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.upgradeLocker.RLock()
	conn := s.conn
	s.upgradeLocker.RUnlock()

	if h, ok := conn.(http.Handler); ok {
		h.ServeHTTP(w, r)
	}
}

func (s *Session) nextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	for {
		s.upgradeLocker.RLock()
		conn := s.conn
		s.upgradeLocker.RUnlock()

		ft, pt, r, err := conn.NextReader()
		if err != nil {
			if op, ok := err.(payload.Error); ok && op.Temporary() {
				continue
			}
			if s.replaced(conn) {
				// An upgrade switched the session to a new connection and
				// closed this one while we waited on it; read from the new one.
				continue
			}
			return 0, 0, nil, err
		}
		return ft, pt, r, nil
	}
}

func (s *Session) nextWriter(ft frame.Type, pt packet.Type) (io.WriteCloser, error) {
	for {
		s.upgradeLocker.RLock()
		conn := s.conn
		s.upgradeLocker.RUnlock()

		w, err := conn.NextWriter(ft, pt)
		if err != nil {
			if op, ok := err.(payload.Error); ok && op.Temporary() {
				continue
			}
			if s.replaced(conn) {
				// See nextReader: retry on the connection that replaced conn.
				continue
			}
			return nil, err
		}
		// Caller must Close the WriteCloser to unlock the connection's
		// FrameWriter when finished writing.
		return w, nil
	}
}

func (s *Session) setDeadline() error {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()

	deadline := time.Now().Add(s.params.PingTimeout)
	s.deadline.Store(deadline.UnixNano())

	err := s.conn.SetReadDeadline(deadline)
	if err != nil {
		return err
	}

	return s.conn.SetWriteDeadline(deadline)
}

func (s *Session) upgrading(t string, conn transport.Conn) {
	// Read a ping from the client.
	err := conn.SetReadDeadline(time.Now().Add(s.params.PingTimeout))
	if err != nil {
		s.logger().Debug("engineio: upgrade probe failed", "err", err)

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Debug("engineio: close connection failed", "err", closeErr)
		}

		return
	}

	ft, pt, r, err := conn.NextReader()
	if err != nil {
		s.logger().Debug("engineio: upgrade probe failed", "err", err)

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Debug("engineio: close connection failed", "err", closeErr)
		}

		return
	}

	if pt != packet.PING {
		if err := r.Close(); err != nil {
			s.logger().Debug("engineio: close reader failed", "err", err)
		}

		if err := conn.Close(); err != nil {
			s.logger().Debug("engineio: close connection failed", "err", err)
		}

		return
	}
	// Wait to close the reader until after data is read and echoed in the reply.

	// Sent a pong in reply.
	err = conn.SetWriteDeadline(time.Now().Add(s.params.PingTimeout))
	if err != nil {
		s.logger().Debug("engineio: upgrade probe failed", "err", err)

		if closeErr := r.Close(); closeErr != nil {
			s.logger().Debug("engineio: close reader failed", "err", closeErr)
		}

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Debug("engineio: close connection failed", "err", closeErr)
		}

		return
	}

	w, err := conn.NextWriter(ft, packet.PONG)
	if err != nil {
		s.logger().Debug("engineio: upgrade probe failed", "err", err)

		if closeErr := r.Close(); closeErr != nil {
			s.logger().Debug("engineio: close reader failed", "err", closeErr)
		}

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Debug("engineio: close connection failed", "err", closeErr)
		}

		return
	}

	// echo
	if _, err = io.Copy(w, r); err != nil {
		s.logger().Debug("engineio: upgrade probe failed", "err", err)

		if closeErr := w.Close(); closeErr != nil {
			s.logger().Debug("engineio: close writer failed", "err", closeErr)
		}

		if closeErr := r.Close(); closeErr != nil {
			s.logger().Debug("engineio: close reader failed", "err", closeErr)
		}

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Debug("engineio: close connection failed", "err", closeErr)
		}

		return
	}

	if err = r.Close(); err != nil {
		s.logger().Debug("engineio: close reader failed", "err", err)

		if closeErr := w.Close(); closeErr != nil {
			s.logger().Debug("engineio: close writer failed", "err", closeErr)
		}

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Debug("engineio: close connection failed", "err", closeErr)
		}

		return
	}

	if err = w.Close(); err != nil {
		s.logger().Debug("engineio: close writer failed", "err", err)

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Debug("engineio: close connection failed", "err", closeErr)
		}

		return
	}

	// Pause the old connection.
	s.upgradeLocker.RLock()
	old := s.conn
	s.upgradeLocker.RUnlock()

	p, ok := old.(Pauser)
	if !ok {
		// old transport doesn't support upgrading
		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Debug("engineio: close connection failed", "err", closeErr)
		}

		return
	}

	p.Pause()

	// Prepare to resume the connection if upgrade fails.
	defer func() {
		if p != nil {
			p.Resume()
		}
	}()

	// Check for upgrade packet from the client.
	_, pt, r, err = conn.NextReader()
	if err != nil {
		s.logger().Debug("engineio: upgrade probe failed", "err", err)

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Debug("engineio: close connection failed", "err", closeErr)
		}

		return
	}

	if pt != packet.UPGRADE {
		if closeErr := r.Close(); closeErr != nil {
			s.logger().Debug("engineio: close reader failed", "err", closeErr)
		}

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Debug("engineio: close connection failed", "err", closeErr)
		}

		return
	}

	if err = r.Close(); err != nil {
		s.logger().Debug("engineio: close reader failed", "err", err)

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Debug("engineio: close connection failed", "err", closeErr)
		}

		return
	}

	// Successful upgrade.
	if !s.switchTransport(t, conn) {
		return
	}

	p = nil
	if err := s.setDeadline(); err != nil { // PingTimeout now runs on conn
		s.observe(reasonTransportError, err)
		_ = s.Close()
	}

	if closeErr := old.Close(); closeErr != nil {
		s.logger().Debug("engineio: close connection failed", "err", closeErr)
	}
}

// logger returns the session logger; its transport attribute follows upgrades.
func (s *Session) logger() *slog.Logger {
	return s.log.Load()
}

// switchTransport makes conn, on transport t, the session's connection and
// updates the logger's transport attribute. If the session has been closed,
// conn is closed instead and switchTransport reports false.
func (s *Session) switchTransport(t string, conn transport.Conn) bool {
	s.upgradeLocker.Lock()
	if s.closed {
		s.upgradeLocker.Unlock()
		if err := conn.Close(); err != nil {
			s.logger().Debug("engineio: close connection failed", "err", err)
		}
		return false
	}
	s.conn = conn
	s.transport = t
	s.log.Store(s.baseLog.With("transport", t))
	s.upgradeLocker.Unlock()
	return true
}

// replaced reports whether conn is no longer the session's connection
// because an upgrade switched to another one. It is false once the session
// is closed, so a closed session's operations fail instead of moving on.
func (s *Session) replaced(conn transport.Conn) bool {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	return !s.closed && s.conn != conn
}

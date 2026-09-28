package session

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/googollee/go-socket.io/engineio/frame"
	"github.com/googollee/go-socket.io/engineio/packet"
	"github.com/googollee/go-socket.io/engineio/payload"
	"github.com/googollee/go-socket.io/engineio/transport"
	"github.com/googollee/go-socket.io/logger"
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

	upgradeLocker sync.RWMutex
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
			ses.logger().Error("session close", "err", closeErr)
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

func (s *Session) Close() error {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()

	return s.conn.Close()
}

// NextReader attempts to obtain a ReadCloser from the session's connection.
// When finished writing, the caller MUST Close the ReadCloser to unlock the
// connection's FramerReader.
func (s *Session) NextReader() (FrameType, io.ReadCloser, error) {
	for {
		ft, pt, r, err := s.nextReader()
		if err != nil {
			if closeErr := s.Close(); closeErr != nil {
				s.logger().Error("close session after next reader", "err", closeErr)
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
				if closeErr := w.Close(); closeErr != nil {
					s.logger().Error("close writer after write pong packet", "err", closeErr)
				}

				// unlocks the wrapped connection's FrameReader
				if closeErr := r.Close(); closeErr != nil {
					s.logger().Error("close reader", "err", closeErr)
				}

				return err
			}()

			if err != nil {
				if closeErr := s.Close(); closeErr != nil {
					s.logger().Error("close session", "err", closeErr)
				}

				return 0, nil, err
			}
			// Read another frame.
			if err := s.setDeadline(); err != nil {
				if closeErr := s.Close(); closeErr != nil {
					s.logger().Error("close session after set deadline", "err", closeErr)
				}

				return 0, nil, err
			}

		case packet.CLOSE:
			// unlocks the wrapped connection's FrameReader
			if err = r.Close(); err != nil {
				s.logger().Error("close reader on packet close", "err", err)
			}

			if err = s.Close(); err != nil {
				s.logger().Error("close session on packet close", "err", err)
			}

			return 0, nil, io.EOF

		case packet.MESSAGE:
			// Caller must Close the ReadCloser to unlock the connection's
			// FrameReader when finished reading.
			return FrameType(ft), r, nil

		default:
			// Unknown packet type. Close reader and try again.
			if err = r.Close(); err != nil {
				s.logger().Error("close reader on unknown packet", "err", err)
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
func (s *Session) NextWriter(typ FrameType) (io.WriteCloser, error) {
	return s.nextWriter(frame.Type(typ), packet.MESSAGE)
}

func (s *Session) Upgrade(transport string, conn transport.Conn) {
	go s.upgrading(transport, conn)
}

func (s *Session) InitSession() error {
	w, err := s.nextWriter(frame.String, packet.OPEN)
	if err != nil {
		if closeErr := s.Close(); closeErr != nil {
			s.logger().Error("close session with string frame and packet open", "err", closeErr)
		}

		return err
	}

	if _, err := s.params.WriteTo(w); err != nil {
		if closeErr := w.Close(); closeErr != nil {
			s.logger().Error("close writer", "err", closeErr)
		}

		if closeErr := s.Close(); closeErr != nil {
			s.logger().Error("close session", "err", closeErr)
		}

		return err
	}

	if err := w.Close(); err != nil {
		if closeErr := s.Close(); closeErr != nil {
			s.logger().Error("close session", "err", closeErr)
		}

		return err
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
		s.logger().Error("set read deadline", "err", err)

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Error("close connect after set read deadline", "err", closeErr)
		}

		return
	}

	ft, pt, r, err := conn.NextReader()
	if err != nil {
		s.logger().Error("get next reader", "err", err)

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Error("close connect after get next reader", "err", closeErr)
		}

		return
	}

	if pt != packet.PING {
		if err := r.Close(); err != nil {
			s.logger().Error("close reader", "err", err)
		}

		if err := conn.Close(); err != nil {
			s.logger().Error("close connect", "err", err)
		}

		return
	}
	// Wait to close the reader until after data is read and echoed in the reply.

	// Sent a pong in reply.
	err = conn.SetWriteDeadline(time.Now().Add(s.params.PingTimeout))
	if err != nil {
		s.logger().Error("set write deadline", "err", err)

		if closeErr := r.Close(); closeErr != nil {
			s.logger().Error("close reader", "err", closeErr)
		}

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Error("close connect", "err", closeErr)
		}

		return
	}

	w, err := conn.NextWriter(ft, packet.PONG)
	if err != nil {
		s.logger().Error("get next writer with pong packet", "err", err)

		if closeErr := r.Close(); closeErr != nil {
			s.logger().Error("close reader", "err", closeErr)
		}

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Error("close connect", "err", closeErr)
		}

		return
	}

	// echo
	if _, err = io.Copy(w, r); err != nil {
		s.logger().Error("copy from reader to writer", "err", err)

		if closeErr := w.Close(); closeErr != nil {
			s.logger().Error("close writer", "err", closeErr)
		}

		if closeErr := r.Close(); closeErr != nil {
			s.logger().Error("close reader", "err", closeErr)
		}

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Error("close connect", "err", closeErr)
		}

		return
	}

	if err = r.Close(); err != nil {
		s.logger().Error("close reader", "err", err)

		if closeErr := w.Close(); closeErr != nil {
			s.logger().Error("close writer", "err", closeErr)
		}

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Error("close connect", "err", closeErr)
		}

		return
	}

	if err = w.Close(); err != nil {
		s.logger().Error("close writer", "err", err)

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Error("close connect", "err", closeErr)
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
			s.logger().Error("close connect after get pauser", "err", closeErr)
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
		s.logger().Error("get next reader", "err", err)

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Error("close connect", "err", closeErr)
		}

		return
	}

	if pt != packet.UPGRADE {
		if closeErr := r.Close(); closeErr != nil {
			s.logger().Error("close reader", "err", closeErr)
		}

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Error("close connect", "err", closeErr)
		}

		return
	}

	if err = r.Close(); err != nil {
		s.logger().Error("close reader", "err", err)

		if closeErr := conn.Close(); closeErr != nil {
			s.logger().Error("close connect", "err", closeErr)
		}

		return
	}

	// Successful upgrade.
	s.switchTransport(t, conn)

	p = nil

	if closeErr := old.Close(); closeErr != nil {
		s.logger().Error("close old connection", "err", closeErr)
	}
}

// logger returns the session logger; its transport attribute follows upgrades.
func (s *Session) logger() *slog.Logger {
	return s.log.Load()
}

// switchTransport makes conn, on transport t, the session's connection and
// updates the logger's transport attribute.
func (s *Session) switchTransport(t string, conn transport.Conn) {
	s.upgradeLocker.Lock()
	s.conn = conn
	s.transport = t
	s.log.Store(s.baseLog.With("transport", t))
	s.upgradeLocker.Unlock()
}

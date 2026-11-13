package session

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/internal"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/payload"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/logger"
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

	upgradeLocker sync.RWMutex // guards conn, transport, closed, opened, the upgrade and heartbeat fields
	closed        bool         // set by Close; a closed session is never upgraded
	opened        time.Time    // set by InitSession; a session never opened logs no close

	// The heartbeat of Engine.IO v4, started by InitSession and stopped by Close. The
	// server sends PING when pingTimer fires and arms pongTimer, which closes the
	// session unless a PONG stops it first; a PONG restarts pingTimer.
	pingTimer *time.Timer
	pongTimer *time.Timer // nil while no PING waits for its PONG

	candidate transport.Conn // the connection of a running upgrade; Close closes it

	// The upgrade reservation, taken by internal.ReserveUpgrade before the connection of
	// an upgrade is accepted, so that at most one upgrade runs. It is returned when the
	// upgrade does not switch the session, and kept when it does.
	upgradeReserved bool
	upgradeTimeout  time.Duration // bounds the probe and UPGRADE of the reserved upgrade

	cause    atomic.Pointer[closeCause] // the first close cause observed
	deadline atomic.Int64               // UnixNano of the deadline setDeadline set on conn
}

// The reasons of the "engineio: session close" record.
const (
	reasonTransportClose = "transport close"
	reasonPingTimeout    = "ping timeout"
	reasonTransportError = "transport error"
	reasonForcedClose    = "forced close"
	reasonShutdown       = "server shutting down"
)

// errHeartbeatDirection is the error of a PING from the client: in Engine.IO v4 only
// the server sends PING. The text is that of the Node server.
var errHeartbeatDirection = errors.New("invalid heartbeat direction")

type closeCause struct {
	reason string
	err    error // set for reasonTransportError only
}

func init() {
	internal.Shutdown = func(s io.Closer) error { return s.(*Session).close(reasonShutdown) }
	internal.ReserveUpgrade = func(s io.Closer, timeout time.Duration) bool {
		return s.(*Session).reserveUpgrade(timeout)
	}
	internal.ReleaseUpgrade = func(s io.Closer) { s.(*Session).releaseUpgrade() }
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

// close closes the session once and logs its close record. The record is logged
// under the lock, so a concurrent close, such as one of the ping timeout racing one
// of the reader, returns only after it.
func (s *Session) close(reason string) error {
	s.observe(reason, nil)
	s.upgradeLocker.Lock()
	if !s.closed && !s.opened.IsZero() {
		c := s.cause.Load()
		args := []any{"reason", c.reason, slog.Duration("duration", time.Since(s.opened))}
		if c.err != nil {
			args = append(args, "err", c.err)
		}
		s.logger().Debug("engineio: session close", args...)
	}
	s.closed = true
	conn, candidate := s.conn, s.candidate
	s.candidate = nil
	if s.pingTimer != nil {
		s.pingTimer.Stop()
	}
	s.stopPongTimer()
	s.upgradeLocker.Unlock()

	// An upgrade waiting on its connection ends with it, without switching.
	if candidate != nil {
		if err := candidate.Close(); err != nil {
			s.logger().Debug("engineio: close connection failed", "err", err)
		}
	}
	return conn.Close()
}

// observe records reason as the close cause unless one was observed before.
func (s *Session) observe(reason string, err error) {
	s.cause.CompareAndSwap(nil, &closeCause{reason, err})
}

// fail observes a non-nil err of a transport read or write as "ping timeout" once the
// deadline of setDeadline has passed, as "transport error" before, and returns it.
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

// NextReader returns the next MESSAGE of the session. When finished reading, the
// caller MUST Close the ReadCloser to unlock the connection's FrameReader.
//
// NextReader is the session's only reader: it consumes the PONGs that answer the
// heartbeat's PINGs, and the CLOSE and client PING packets that end the session. The
// PINGs and the ping timeout run on timers, so a caller that stops calling NextReader,
// or holds a reader open, for longer than PingTimeout after a PING ends the session
// with reason "ping timeout" (engineio.Conn.NextReader states the caller's contract).
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
		case packet.PONG:
			// unlocks the wrapped connection's FrameReader
			err = s.fail(r.Close())
			if err == nil {
				err = s.fail(s.onPong())
			}
			if err != nil {
				if closeErr := s.Close(); closeErr != nil {
					s.logger().Debug("engineio: session close failed", "err", closeErr)
				}

				return 0, nil, err
			}

		case packet.PING:
			// Only the server sends PING in Engine.IO v4; the session ends as the
			// Node server ends it.
			if err = r.Close(); err != nil {
				s.logger().Debug("engineio: close reader failed", "err", err)
			}

			s.observe(reasonTransportError, errHeartbeatDirection)
			if err = s.Close(); err != nil {
				s.logger().Debug("engineio: session close failed", "err", err)
			}

			return 0, nil, errHeartbeatDirection

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

// Upgrade moves the session to conn, a connection of the named transport, once the
// client has sent the probe and UPGRADE on it within the upgrade window: the timeout
// engineio.Server reserved the upgrade with, 10 s otherwise. The window starts now, so
// conn must have just been accepted. Only one upgrade of a session may run at a time:
// engineio.Server ensures it with the reservation; another caller must not start one
// before the previous upgrade has ended.
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
		s.pingTimer = time.AfterFunc(s.params.PingInterval, s.ping)
	}
	return nil
}

// ping sends a PING and arms the timer that closes the session unless a PONG
// answers within PingTimeout. It runs when pingTimer fires. The timer is armed
// before the write, so a write that blocks, as on a polling session whose client
// stopped polling, ends with the session. The transport deadline is moved before
// the write too: measured from the previous PONG alone, it would expire before this
// PING's timeout whenever pingTimer fired late, and cut off a PONG that is in time.
func (s *Session) ping() {
	s.upgradeLocker.Lock()
	if s.closed {
		s.upgradeLocker.Unlock()
		return
	}
	s.stopPongTimer()
	var t *time.Timer
	t = time.AfterFunc(s.params.PingTimeout, func() {
		s.upgradeLocker.RLock()
		missed := s.pongTimer == t // not stopped by a PONG or Close in the meantime
		s.upgradeLocker.RUnlock()
		if missed {
			if err := s.close(reasonPingTimeout); err != nil {
				s.logger().Debug("engineio: session close failed", "err", err)
			}
		}
	})
	s.pongTimer = t
	s.upgradeLocker.Unlock()

	err := s.setDeadline()
	var w io.WriteCloser
	if err == nil {
		w, err = s.nextWriter(frame.String, packet.PING)
	}
	if err == nil {
		err = w.Close()
	}
	// A failed PING ends the session; its close record carries the failure.
	if s.fail(err) != nil {
		if closeErr := s.Close(); closeErr != nil {
			s.logger().Debug("engineio: session close failed", "err", closeErr)
		}
	}
}

// onPong stops the ping timeout, schedules the next PING PingInterval from now, as
// the Node server does, and extends the transport deadlines.
func (s *Session) onPong() error {
	s.upgradeLocker.Lock()
	if !s.closed && s.pingTimer != nil {
		s.stopPongTimer()
		s.pingTimer.Reset(s.params.PingInterval)
	}
	s.upgradeLocker.Unlock()
	return s.setDeadline()
}

// stopPongTimer disarms the ping timeout. The caller holds upgradeLocker.
func (s *Session) stopPongTimer() {
	if s.pongTimer != nil {
		s.pongTimer.Stop()
		s.pongTimer = nil
	}
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

// setDeadline sets the read and write deadlines of the connection to PingInterval
// plus PingTimeout from now. It runs when the session is created, when a PING is
// sent, after each PONG and after an upgrade, so the deadline expires no earlier
// than PingInterval after the ping timeout of the outstanding PING, however late
// pingTimer fired. The deadline is a backstop for transport operations the ping
// timeout cannot interrupt, such as the body read of a polling POST or a blocked
// write.
func (s *Session) setDeadline() error {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()

	deadline := time.Now().Add(s.params.PingInterval + s.params.PingTimeout)
	s.deadline.Store(deadline.UnixNano())

	err := s.conn.SetReadDeadline(deadline)
	if err != nil {
		return err
	}

	return s.conn.SetWriteDeadline(deadline)
}

func (s *Session) upgrading(t string, conn transport.Conn) {
	if !s.trackCandidate(conn) {
		return
	}
	defer s.untrackCandidate(conn)

	// The probe and UPGRADE must both arrive within upgradeTimeout of the accept of conn,
	// which happens right before Upgrade starts this goroutine.
	s.upgradeLocker.RLock()
	timeout := s.upgradeTimeout
	s.upgradeLocker.RUnlock()
	if timeout <= 0 { // an upgrade started without the reservation
		timeout = internal.DefaultUpgradeTimeout
	}
	deadline := time.Now().Add(timeout)

	// Read a ping from the client.
	err := conn.SetReadDeadline(deadline)
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
	err = conn.SetWriteDeadline(deadline)
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

	// Only a connection that can be paused, polling, is upgraded from. It keeps
	// serving the session until UPGRADE; the NOOP completes its pending poll.
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

	stopNoops := make(chan struct{})
	defer close(stopNoops)
	go s.sendNoops(old, stopNoops)

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

	// Successful upgrade. As the Node server does, pause the old connection only
	// now: a poll still waiting is answered with NOOP and the payload stops before
	// the session moves to conn.
	p.Pause()
	if !s.switchTransport(t, conn) {
		return
	}

	if err := s.setDeadline(); err != nil { // the deadlines now apply to conn
		s.observe(reasonTransportError, err)
		_ = s.Close()
	}

	if closeErr := old.Close(); closeErr != nil {
		s.logger().Debug("engineio: close connection failed", "err", closeErr)
	}
}

// noopInterval is the least time between two NOOPs of an upgrade: the Node server
// checks for a pending poll every 100 ms.
const noopInterval = 100 * time.Millisecond

// sendNoops answers the polls of old, the connection an upgrade replaces, with NOOP
// until stop is closed: the client waits for its pending poll to end before it pauses
// polling and sends UPGRADE. Each NOOP waits for a poll, so a poll that arrives while
// one waits is answered at once, and the next NOOP follows at least noopInterval
// later, as on the Node server. After stop a NOOP already waiting still answers the
// next poll, unless UPGRADE paused old or the session closed it, which fail it; that
// failure is expected.
func (s *Session) sendNoops(old transport.Conn, stop <-chan struct{}) {
	for {
		w, err := old.NextWriter(frame.String, packet.NOOP)
		if err == nil {
			err = w.Close()
		}
		if err != nil {
			s.logger().Debug("engineio: upgrade noop failed", "err", err)
			return
		}
		pace := time.NewTimer(noopInterval)
		select {
		case <-stop:
			pace.Stop()
			return
		case <-pace.C:
		}
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
	s.candidate = nil
	s.log.Store(s.baseLog.With("transport", t))
	s.upgradeLocker.Unlock()
	return true
}

// trackCandidate records conn as the connection of the running upgrade, so that Close
// closes it. On a closed session it closes conn and reports false.
func (s *Session) trackCandidate(conn transport.Conn) bool {
	s.upgradeLocker.Lock()
	closed := s.closed
	if !closed {
		s.candidate = conn
	}
	s.upgradeLocker.Unlock()
	if closed {
		if err := conn.Close(); err != nil {
			s.logger().Debug("engineio: close connection failed", "err", err)
		}
	}
	return !closed
}

// untrackCandidate forgets conn and returns the upgrade reservation when the upgrade
// ends without switching to conn. After a switch, or once the session is closed,
// candidate is no longer conn and the reservation is kept.
func (s *Session) untrackCandidate(conn transport.Conn) {
	s.upgradeLocker.Lock()
	if s.candidate == conn {
		s.candidate = nil
		s.upgradeReserved = false
	}
	s.upgradeLocker.Unlock()
}

// reserveUpgrade takes the upgrade reservation for an upgrade bounded by timeout, unless
// the session is closed or an upgrade holds the reservation or has switched the session.
func (s *Session) reserveUpgrade(timeout time.Duration) bool {
	s.upgradeLocker.Lock()
	defer s.upgradeLocker.Unlock()
	if s.closed || s.upgradeReserved {
		return false
	}
	s.upgradeReserved, s.upgradeTimeout = true, timeout
	return true
}

// releaseUpgrade returns the upgrade reservation of an upgrade that never started.
func (s *Session) releaseUpgrade() {
	s.upgradeLocker.Lock()
	s.upgradeReserved = false
	s.upgradeLocker.Unlock()
}

// replaced reports whether conn is no longer the session's connection
// because an upgrade switched to another one. It is false once the session
// is closed, so a closed session's operations fail instead of moving on.
func (s *Session) replaced(conn transport.Conn) bool {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	return !s.closed && s.conn != conn
}

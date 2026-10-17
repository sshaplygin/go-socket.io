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
	"github.com/sshaplygin/go-socket.io/internal/engineclose"
	"github.com/sshaplygin/go-socket.io/logger"
)

// Pauser is connection which can be paused and resumes.
type Pauser interface {
	Pause()
	Resume()
}

type Session struct {
	hooks         *Hooks
	headers       func(*http.Request, http.Header)
	closeDone     chan struct{}
	notified      chan struct{} // the first closer has finished its synchronous Error hook
	closeErr      error
	transportOnce sync.Once
	transportDone chan struct{}
	writeGate     chan struct{}
	stopWrites    chan struct{}
	closeTimeout  time.Duration
	conn          transport.Conn
	params        transport.ConnParameters
	transport     string
	baseLog       *slog.Logger                // logger with the sid attribute
	log           atomic.Pointer[slog.Logger] // baseLog plus the current transport

	context        interface{}
	request        *http.Request
	maxMessageSize int64
	upgradeTimeout time.Duration
	probe          transport.Conn // guarded by upgradeLocker; closed with the session

	upgradeLocker   sync.RWMutex // guards conn, transport, closed and opened
	closed          bool         // set by Close; a closed session is never upgraded
	opened          time.Time    // set by InitSession; a session never opened logs no close
	transportClosed bool         // set after the transport's Close returns
	upgraded        bool         // set when switchTransport commits an upgrade

	cause    atomic.Pointer[closeCause] // the first close cause observed
	writers  atomic.Int64               // open frame writers returned to callers
	deadline atomic.Int64               // UnixNano of the liveness deadline of conn
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

func init() {
	internal.Shutdown = func(s io.Closer) error { return s.(*Session).closeMode(reasonShutdown, true) }
	engineclose.AbortSession = func(c io.Closer) (bool, error) {
		s, ok := c.(*Session)
		if !ok {
			return false, nil
		}
		return true, s.closeMode(reasonForcedClose, true)
	}
}

// New creates a session over conn. log receives errors the session cannot
// return to a caller, with sid and transport attributes added; the transport
// attribute follows upgrades. nil means logger.Log.
func New(conn transport.Conn, sid, transport string, params transport.ConnParameters, log *slog.Logger) (*Session, error) {
	params.SID = sid
	baseLog := logger.Wrap(log).With("sid", sid)

	ses := &Session{
		closeDone:      make(chan struct{}),
		notified:       make(chan struct{}),
		transportDone:  make(chan struct{}),
		writeGate:      make(chan struct{}, 1),
		stopWrites:     make(chan struct{}),
		closeTimeout:   30 * time.Second,
		transport:      transport,
		conn:           conn,
		params:         params,
		baseLog:        baseLog,
		maxMessageSize: 1_000_000,
		upgradeTimeout: 10 * time.Second,
	}
	ses.writeGate <- struct{}{}
	ses.log.Store(baseLog.With("transport", transport))
	ses.SetLimits(ses.maxMessageSize, ses.upgradeTimeout)
	if target, ok := conn.(interface{ SetSessionID(string) }); ok {
		target.SetSessionID(sid)
	}

	if err := ses.setDeadline(); err != nil {
		if closeErr := ses.Close(); closeErr != nil {
			ses.logger().Debug("engineio: session close failed", "err", closeErr)
		}

		return nil, err
	}

	return ses, nil
}

// SetLimits configures message and upgrade bounds before InitSession. Nonpositive
// values use 1,000,000 bytes and 10 seconds. It also configures the transport.
func (s *Session) SetLimits(maxMessageSize int64, upgradeTimeout time.Duration) {
	if maxMessageSize <= 0 {
		maxMessageSize = 1_000_000
	}
	if upgradeTimeout <= 0 {
		upgradeTimeout = 10 * time.Second
	}
	s.maxMessageSize, s.upgradeTimeout = maxMessageSize, upgradeTimeout
	if c, ok := s.conn.(interface{ SetReadLimit(int64) }); ok {
		c.SetReadLimit(maxMessageSize)
	}
}

// SetRequest retains the handshake request before InitSession and ConnInitor.
func (s *Session) SetRequest(r *http.Request) { s.request = r }

// Request returns the retained handshake request, nil before SetRequest. Its Body
// is consumed and its Context is canceled after the handshake handler returns;
// only its metadata should be used afterwards.
func (s *Session) Request() *http.Request { return s.request }

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

// Close starts an orderly polling close after accepted writes. It returns while
// waiting for a poll; the transport remains closing for at most 30 seconds.
// Other transports and failed sessions close immediately. The first cause wins.
func (s *Session) Close() error {
	return s.close(reasonForcedClose)
}

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
		if abort {
			// Abort interrupts an established orderly wait, but cannot pass
			// the first closer's synchronous error notification.
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
	s    *Session
	once sync.Once
	err  error
}

func (w *frameWriter) Write(p []byte) (int, error) {
	n, err := w.WriteCloser.Write(p)
	return n, w.s.fail(err)
}

func (w *frameWriter) Close() error {
	w.once.Do(func() {
		defer w.s.writers.Add(-1)
		w.err = w.s.fail(w.WriteCloser.Close())
	})
	return w.err
}

// NextReader attempts to obtain a ReadCloser from the session's connection.
// When finished writing, the caller MUST Close the ReadCloser to unlock the
// connection's FramerReader.
func (s *Session) NextReader() (FrameType, io.ReadCloser, error) {
	for {
		ft, pt, r, err := s.nextReader()
		if err != nil {
			_ = s.fail(err)
			if closeErr := s.Close(); closeErr != nil {
				s.logger().Debug("engineio: session close failed", "err", closeErr)
			}

			return 0, nil, s.readerCloseError()
		}
		// Every received packet, including MESSAGE, renews the liveness window.
		if s.hooks != nil && s.hooks.Packet != nil {
			s.hooks.Packet(s, pt, ft)
		}
		if err := s.fail(s.setDeadline()); err != nil {
			_ = r.Close()
			_ = s.Close()
			return 0, nil, s.readerCloseError()
		}

		switch pt {
		case packet.PING:
			if s.hooks != nil && s.hooks.Heartbeat != nil {
				s.hooks.Heartbeat(s)
			}
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

				return 0, nil, s.readerCloseError()
			}
		case packet.CLOSE:
			s.observe(reasonTransportClose, nil)
			// unlocks the wrapped connection's FrameReader
			if err = r.Close(); err != nil {
				s.logger().Debug("engineio: close reader failed", "err", err)
			}

			if err = s.closeMode(reasonTransportClose, true); err != nil {
				s.logger().Debug("engineio: session close failed", "err", err)
			}

			return 0, nil, s.readerCloseError()

		case packet.MESSAGE:
			// Caller must Close the ReadCloser to unlock the connection's
			// FrameReader when finished reading.
			return FrameType(ft), frameReader{r, s}, nil

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
func (s *Session) NextWriter(typ FrameType) (io.WriteCloser, error) {
	return s.NextWriterCompress(typ, true)
}

// NextWriterCompress writes a MESSAGE with its per-packet compression preference.
func (s *Session) NextWriterCompress(typ FrameType, compress bool) (io.WriteCloser, error) {
	w, err := s.nextWriterCompress(frame.Type(typ), packet.MESSAGE, compress)
	if s.fail(err) != nil {
		_ = s.Close()
		return nil, fmt.Errorf("next writer: %w", s.closeError())
	}
	s.writers.Add(1)
	return &frameWriter{WriteCloser: w, s: s}, nil
}

func (s *Session) Upgrade(transport string, conn transport.Conn) {
	started := time.Now()
	go s.upgradingAt(transport, conn, started)
}

func (s *Session) InitSession() error {
	w, err := s.nextWriter(frame.String, packet.OPEN)
	if err != nil {
		_ = s.fail(err)
		if closeErr := s.Close(); closeErr != nil {
			s.logger().Debug("engineio: session close failed", "err", closeErr)
		}

		return err
	}

	if _, err := s.params.WriteTo(w); err != nil {
		_ = s.fail(err)
		if closeErr := w.Close(); closeErr != nil {
			s.logger().Debug("engineio: close writer failed", "err", closeErr)
		}

		if closeErr := s.Close(); closeErr != nil {
			s.logger().Debug("engineio: session close failed", "err", closeErr)
		}

		return err
	}

	if err := w.Close(); err != nil {
		_ = s.fail(err)
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
	if s.closed {
		return io.ErrClosedPipe
	}
	return nil
}

func (s *Session) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.upgradeLocker.RLock()
	conn := s.conn
	s.upgradeLocker.RUnlock()

	if h, ok := conn.(http.Handler); ok {
		if s.hooks == nil && s.headers == nil {
			h.ServeHTTP(w, r)
			return
		}
		observed := &responseObserver{ResponseWriter: w, session: s, request: r}
		defer observed.drain()
		h.ServeHTTP(observed, r)
	}
}

// A root reader ending also starts forced cleanup. Keep it alive while an orderly
// transport close is still draining, so that cleanup cannot abort accepted writes.
func (s *Session) readerCloseError() error {
	<-s.closeDone
	return fmt.Errorf("next reader: %w", s.closeError())
}

func (s *Session) nextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	for {
		s.upgradeLocker.RLock()
		conn, closed := s.conn, s.closed
		s.upgradeLocker.RUnlock()
		if closed {
			return 0, 0, nil, s.closeError()
		}

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
	return s.nextWriterCompress(ft, pt, true)
}

func (s *Session) nextWriterCompress(ft frame.Type, pt packet.Type, compress bool) (io.WriteCloser, error) {
	select {
	case <-s.stopWrites:
		return nil, s.closeError()
	case <-s.writeGate:
	}
	owned := true
	defer func() {
		if owned {
			s.writeGate <- struct{}{}
		}
	}()
	notified := false
	for {
		s.upgradeLocker.RLock()
		conn, closed := s.conn, s.closed
		s.upgradeLocker.RUnlock()
		if closed {
			return nil, s.closeError()
		}

		if !notified && s.hooks != nil && s.hooks.PacketCreate != nil {
			s.hooks.PacketCreate(s, pt, ft)
		}
		notified = true
		var w io.WriteCloser
		var err error
		if target, ok := conn.(transport.CompressWriter); ok {
			w, err = target.NextWriterCompress(ft, pt, compress)
		} else {
			w, err = conn.NextWriter(ft, pt)
		}
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
		owned = false
		return &orderedWriter{WriteCloser: w, release: s.writeGate}, nil
	}
}

type orderedWriter struct {
	io.WriteCloser
	release chan struct{}
	once    sync.Once
	err     error
}

func (w *orderedWriter) Close() error {
	w.once.Do(func() {
		w.err = w.WriteCloser.Close()
		w.release <- struct{}{}
	})
	return w.err
}

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
	s.upgradingAt(t, conn, time.Now())
}

func (s *Session) upgradingAt(t string, conn transport.Conn, started time.Time) {
	s.upgradeLocker.Lock()
	if s.closed || s.probe != nil {
		s.upgradeLocker.Unlock()
		_ = conn.Close()
		return
	}
	old := s.conn
	s.probe = conn
	s.upgradeLocker.Unlock()
	defer func() {
		s.upgradeLocker.Lock()
		s.probe = nil
		s.upgradeLocker.Unlock()
	}()
	if c, ok := conn.(interface{ SetReadLimit(int64) }); ok {
		c.SetReadLimit(s.maxMessageSize)
	}

	// Closing the candidate also bounds transports whose deadline methods do not
	// interrupt a pending operation. The mutex orders expiry against the switch.
	var state sync.Mutex
	expired, switched := false, false
	deadline := started.Add(s.upgradeTimeout)
	timer := time.AfterFunc(time.Until(deadline), func() {
		state.Lock()
		defer state.Unlock()
		expired = true
		_ = conn.Close()
	})
	defer timer.Stop()
	defer func() {
		if !switched {
			_ = conn.Close()
		}
	}()
	if err := conn.SetReadDeadline(deadline); err != nil {
		return
	}
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return
	}
	ft, pt, r, err := conn.NextReader()
	if err != nil {
		return
	}
	if pt != packet.PING {
		_ = r.Close()
		return
	}
	w, err := conn.NextWriter(ft, packet.PONG)
	if err != nil {
		_ = r.Close()
		return
	}
	_, copyErr := io.Copy(w, r)
	readErr := r.Close()
	writeErr := w.Close()
	if copyErr != nil || readErr != nil || writeErr != nil {
		return
	}
	if s.hooks != nil && s.hooks.Upgrading != nil {
		s.hooks.Upgrading(s, t)
	}
	p, ok := old.(Pauser)
	if !ok {
		return
	}

	// Polling remains writable throughout the probe. A tick answers only a poll
	// currently waiting for a session writer, so an idle client cannot spin.
	stopChecks := make(chan struct{})
	checksDone := make(chan struct{})
	if poll, ok := old.(interface{ AnswerPoll() bool }); ok {
		go func() {
			defer close(checksDone)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stopChecks:
					return
				case <-ticker.C:
					poll.AnswerPoll()
				}
			}
		}()
	} else {
		close(checksDone)
	}
	stopPollChecks := sync.OnceFunc(func() { close(stopChecks); <-checksDone })
	defer stopPollChecks()

	_, pt, r, err = conn.NextReader()
	if err != nil {
		return
	}
	readErr = r.Close()
	if pt != packet.UPGRADE || readErr != nil {
		return
	}

	// The timeout ends when UPGRADE is read. Waiting for an in-flight POST
	// afterwards must not let its timer close the newly selected websocket.
	state.Lock()
	if expired || !timer.Stop() {
		state.Unlock()
		return
	}
	state.Unlock()
	stopPollChecks()
	// Pause only after UPGRADE, waiting for any in-flight POST to finish.
	p.Pause()
	if !s.switchTransport(t, conn) {
		p.Resume()
		return
	}
	switched = true
	if s.hooks != nil && s.hooks.Upgrade != nil {
		s.hooks.Upgrade(s, t)
	}
	if err := s.setDeadline(); err != nil {
		s.observe(reasonTransportError, err)
		_ = s.Close()
	}
	if err := old.Close(); err != nil {
		s.logger().Debug("engineio: close connection failed", "err", err)
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
	s.upgraded = true
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

// Writable reports whether an open session can take a frame now. It is false
// while a returned writer is open or the transport reports itself not writable.
// A transport without a Writable method counts as writable.
func (s *Session) Writable() bool {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	if s.closed || s.writers.Load() != 0 {
		return false
	}
	if c, ok := s.conn.(interface{ Writable() bool }); ok {
		return c.Writable()
	}
	return true
}

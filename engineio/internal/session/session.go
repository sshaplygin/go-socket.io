// Package session implements the private server Engine.IO session lifecycle.
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

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/logger"
)

// Session owns one server connection across heartbeat, transport upgrade and close.
type Session struct {
	hooks         *Hooks
	headers       func(*http.Request, http.Header)
	onClosed      func() // installed before publication; runs after transport shutdown
	closeDone     chan struct{}
	notified      chan struct{} // the first closer has finished its synchronous Error hook
	closeErr      error         // written by transportOnce; published by closeDone/transportDone
	transportOnce sync.Once
	transportDone chan struct{}
	writeGate     chan struct{}
	stopWrites    chan struct{}
	closeTimeout  time.Duration
	conn          transport.Conn
	params        eioparser.ConnParameters
	transport     string
	baseLog       *slog.Logger                // logger with the sid attribute
	log           atomic.Pointer[slog.Logger] // baseLog plus the current transport

	context        interface{}
	request        *http.Request
	maxMessageSize int64
	upgradeTimeout time.Duration
	probe          transport.Conn // guarded by upgradeLocker; closed with the session

	// Holders of upgradeLocker, read or write, must not call transport methods
	// that can wait for an open frame reader or writer, such as SetWriteDeadline:
	// a failing frame reader or writer reaches closeTransport, which takes the
	// write lock. Snapshot conn and release the lock first, as setDeadline,
	// nextReader and nextWriterCompress do.
	upgradeLocker   sync.RWMutex // guards conn, transport, probe, closed, opened, transportClosed and upgraded
	closed          bool         // set by Close; a closed session is never upgraded
	opened          time.Time    // set by Init; a session never opened logs no close
	transportClosed bool         // set after the transport's Close returns
	upgraded        bool         // set when switchTransport commits an upgrade

	cause   atomic.Pointer[closeCause]       // the first close cause observed
	closing atomic.Pointer[closingTransport] // immutable connection snapshot once closed
	retired atomic.Pointer[closingTransport] // the connection an upgrade replaced
	writers atomic.Int64                     // open frame writers returned to callers
}

// Done closes when session shutdown starts, before an orderly transport drain.
// Upper-layer owners can cancel their work without waiting for their reader.
func (s *Session) Done() <-chan struct{} { return s.stopWrites }

// Closed closes after the transport and its close hooks have finished.
func (s *Session) Closed() <-chan struct{} { return s.closeDone }

// SetCloseCallback installs registry cleanup before the session is published.
// The callback runs once, without session locks, before Closed is signaled.
func (s *Session) SetCloseCallback(callback func()) { s.onClosed = callback }

// New creates a session over conn. log receives errors the session cannot
// return to a caller, with sid and transport attributes added; the transport
// attribute follows upgrades. nil means logger.Log.
func New(conn transport.Conn, sid, transport string, params eioparser.ConnParameters, log *slog.Logger) (*Session, error) {
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

// SetLimits configures message and upgrade bounds before Init. Nonpositive
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

// SetRequest retains the handshake request before Init and ConnInitor.
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
	return n, r.s.failRead(err)
}

func (r frameReader) Close() error { return r.s.failRead(r.ReadCloser.Close()) }

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
func (s *Session) NextReader() (eioparser.FrameType, io.ReadCloser, error) {
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
		case eioparser.PING:
			if s.hooks != nil && s.hooks.Heartbeat != nil {
				s.hooks.Heartbeat(s)
			}
			// Respond to a ping with a pong.
			err := func() error {
				// Release the POST before waiting for a GET to carry its pong.
				// This also lets an upgrade pause finish an in-flight POST.
				body, err := io.ReadAll(r)
				closeErr := r.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
				w, err := s.nextWriter(ft, eioparser.PONG)
				if err != nil {
					return err
				}
				_, err = w.Write(body)
				_ = s.fail(err)
				if closeErr := w.Close(); err == nil {
					err = closeErr
				}

				return err
			}()

			if s.fail(err) != nil {
				if closeErr := s.Close(); closeErr != nil {
					s.logger().Debug("engineio: session close failed", "err", closeErr)
				}

				return 0, nil, s.readerCloseError()
			}
		case eioparser.CLOSE:
			// unlocks the wrapped connection's FrameReader
			if err = s.fail(r.Close()); err != nil {
				s.logger().Debug("engineio: close reader failed", "err", err)
			}
			s.observe(reasonTransportClose, nil) // a failed drain remains a transport error

			if err = s.closeMode(reasonTransportClose, true); err != nil {
				s.logger().Debug("engineio: session close failed", "err", err)
			}

			return 0, nil, s.readerCloseError()

		case eioparser.MESSAGE:
			// Caller must Close the ReadCloser to unlock the connection's
			// FrameReader when finished reading.
			return eioparser.FrameType(ft), frameReader{r, s}, nil

		default:
			// Known control packets can be ignored here, but an invalid wire
			// type is a parser failure and must terminate the session.
			if pt < eioparser.OPEN || pt > eioparser.NOOP {
				// Drain through the bounded reader before recording the parse
				// error so an inflated-size failure keeps its transport cause.
				if s.fail(r.Close()) == nil {
					_ = s.fail(fmt.Errorf("invalid Engine.IO packet type %d", pt))
				}
				_ = s.Close()
				return 0, nil, s.readerCloseError()
			}
			if err = s.fail(r.Close()); err != nil {
				s.logger().Debug("engineio: close reader failed", "err", err)
				_ = s.Close()
				return 0, nil, s.readerCloseError()
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
func (s *Session) NextWriter(typ eioparser.FrameType) (io.WriteCloser, error) {
	return s.NextWriterCompress(typ, true)
}

// NextWriterCompress writes a MESSAGE with its per-packet compression preference.
func (s *Session) NextWriterCompress(typ eioparser.FrameType, compress bool) (io.WriteCloser, error) {
	w, err := s.nextWriterCompress(eioparser.FrameType(typ), eioparser.MESSAGE, compress)
	if s.fail(err) != nil {
		_ = s.Close()
		return nil, fmt.Errorf("next writer: %w", s.closeError())
	}
	s.writers.Add(1)
	return &frameWriter{WriteCloser: w, s: s}, nil
}

func (s *Session) Init() error {
	w, err := s.nextWriter(eioparser.String, eioparser.OPEN)
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

func (s *Session) nextReader() (eioparser.FrameType, eioparser.PacketType, io.ReadCloser, error) {
	for {
		s.upgradeLocker.RLock()
		conn, closed := s.conn, s.closed
		s.upgradeLocker.RUnlock()
		if closed {
			return 0, 0, nil, s.closeError()
		}

		ft, pt, r, err := conn.NextReader()
		if err != nil {
			if op, ok := err.(polling.Error); ok && op.Temporary() && !timedOut(err) {
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

func (s *Session) nextWriter(ft eioparser.FrameType, pt eioparser.PacketType) (io.WriteCloser, error) {
	return s.nextWriterCompress(ft, pt, true)
}

func (s *Session) nextWriterCompress(ft eioparser.FrameType, pt eioparser.PacketType, compress bool) (io.WriteCloser, error) {
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
			if op, ok := err.(polling.Error); ok && op.Temporary() && !timedOut(err) {
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
	deadline := time.Now().Add(s.params.PingInterval + s.params.PingTimeout)

	// Deadline setters can wait for an open transport writer. Do not retain
	// the session lock while waiting: closing must be able to interrupt it.
	s.upgradeLocker.RLock()
	conn := s.conn
	s.upgradeLocker.RUnlock()
	err := conn.SetReadDeadline(deadline)
	if err != nil {
		return err
	}

	return conn.SetWriteDeadline(deadline)
}

// logger returns the session logger; its transport attribute follows upgrades.
func (s *Session) logger() *slog.Logger {
	return s.log.Load()
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

// ReadyState returns the current session state.
func (s *Session) ReadyState() ReadyState {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	switch {
	case s.transportClosed:
		return StateClosed
	case s.closed || s.cause.Load() != nil:
		return StateClosing
	case s.opened.IsZero():
		return StateOpening
	default:
		return StateOpen
	}
}

// Upgraded reports whether the session completed a transport upgrade.
func (s *Session) Upgraded() bool {
	s.upgradeLocker.RLock()
	defer s.upgradeLocker.RUnlock()
	return s.upgraded
}

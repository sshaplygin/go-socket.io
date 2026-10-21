package client

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/logger"
)

// Opener is client connection which need receive open message first.
type Opener interface {
	Open() (eioparser.ConnParameters, error)
}

type client struct {
	maxMessageSize int64
	conn           transport.Conn
	params         eioparser.ConnParameters
	transport      string
	context        interface{}
	close          chan struct{}
	closeOnce      sync.Once
	writers        atomic.Int64
	mu             sync.RWMutex
	initOnce       sync.Once
	writeGate      chan struct{}
	upgraded       bool
	upgrading      bool
	probe          transport.Conn
	cause          *CloseError
	readDeadline   time.Time
	writeDeadline  time.Time
	lastPing       time.Time
	onPing         func()
	onPong         func(time.Duration)
}

// Done closes when shutdown starts, before the transport finishes closing.
func (c *client) Done() <-chan struct{} {
	c.initialize()
	return c.close
}

func (c *client) initialize() {
	c.initOnce.Do(func() {
		c.writeGate = make(chan struct{}, 1)
		c.writeGate <- struct{}{}
		if c.close == nil {
			c.close = make(chan struct{})
		}
	})
}

func (c *client) SetContext(v interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.context = v
}

func (c *client) Context() interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.context
}

func (c *client) ID() string {
	return c.params.SID
}

func (c *client) Transport() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.transport
}

// ReadyState is open after Dial and closed after Close or the ping loop exits.
func (c *client) ReadyState() ReadyState {
	c.initialize()
	select {
	case <-c.close:
		return StateClosed
	default:
		return StateOpen
	}
}

// Upgraded reports whether the client has completed a transport upgrade.
func (c *client) Upgraded() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.upgraded
}

func (c *client) Close() error {
	return c.terminate(ReasonForcedClose, nil)
}

func (c *client) terminate(reason CloseReason, err error) error {
	c.initialize()
	var result error
	c.closeOnce.Do(func() {
		c.mu.Lock()
		if c.cause == nil {
			c.cause = &CloseError{Reason: reason, Err: err}
		}
		conn, probe := c.conn, c.probe
		close(c.close)
		c.mu.Unlock()
		if probe != nil {
			_ = probe.Close()
		}
		if p, ok := conn.(interface{ Resume() }); ok {
			p.Resume() // release an upgrade's cancellable polling pause
		}
		result = conn.Close()
	})
	return result
}

func (c *client) closeError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cause
}

func (c *client) connection() transport.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

func (c *client) failure(err error) error {
	c.mu.RLock()
	read, write := c.readDeadline, c.writeDeadline
	c.mu.RUnlock()
	now := time.Now()
	reason := ReasonTransportError
	if c.params.PingInterval+c.params.PingTimeout > 0 &&
		(!read.IsZero() && !now.Before(read) || !write.IsZero() && !now.Before(write)) {
		reason, err = ReasonPingTimeout, nil
	}
	// Keep the dialer's existing ping-loop lifecycle: a reader reports the first
	// cause immediately, while Close or the failed ping ends the engine client.
	c.mu.Lock()
	if c.cause == nil {
		c.cause = &CloseError{Reason: reason, Err: err}
	}
	c.mu.Unlock()
	return c.closeError()
}

func (c *client) NextReader() (eioparser.FrameType, io.ReadCloser, error) {
	c.initialize()
	for {
		select {
		case <-c.close:
			return 0, nil, c.closeError()
		default:
		}
		conn := c.connection()
		ft, pt, r, err := conn.NextReader()
		if err != nil {
			if conn != c.connection() {
				continue // the old polling reader was released by the switch
			}
			return 0, nil, c.failure(err)
		}

		switch pt {
		case eioparser.PONG:
			deadline := time.Now().Add(c.params.PingInterval + c.params.PingTimeout)
			if err = conn.SetReadDeadline(deadline); err != nil {
				_ = r.Close()
				return 0, nil, c.failure(err)
			}
			c.mu.Lock()
			c.readDeadline = deadline
			ping := c.lastPing
			c.mu.Unlock()
			if c.onPong != nil && !ping.IsZero() {
				c.onPong(time.Since(ping))
			}

		case eioparser.CLOSE:
			if err = r.Close(); err != nil {
				return 0, nil, c.failure(err)
			}
			if err = c.terminate(ReasonTransportClose, io.EOF); err != nil {
				logger.Log.Debug("engineio: close connection failed", "err", err)
			}

			return 0, nil, c.closeError()

		case eioparser.MESSAGE:
			return eioparser.FrameType(ft), &clientReader{ReadCloser: r, c: c}, nil
		}

		// Transports keep a read failure, so the next NextReader returns it: DEBUG.
		if err = r.Close(); err != nil {
			logger.Log.Debug("engineio: close reader failed", "err", err)
		}
	}
}

func (c *client) NextWriter(typ eioparser.FrameType) (io.WriteCloser, error) {
	return c.NextWriterCompress(typ, true)
}

// NextWriterCompress writes a MESSAGE with its per-packet compression preference.
func (c *client) NextWriterCompress(typ eioparser.FrameType, compress bool) (io.WriteCloser, error) {
	if err := c.acquireWrite(nil); err != nil {
		return nil, err
	}
	select {
	case <-c.close:
		c.releaseWrite()
		return nil, c.closeError()
	default:
	}
	var w io.WriteCloser
	var err error
	if target, ok := c.connection().(transport.CompressWriter); ok {
		w, err = target.NextWriterCompress(eioparser.FrameType(typ), eioparser.MESSAGE, compress)
	} else {
		w, err = c.connection().NextWriter(eioparser.FrameType(typ), eioparser.MESSAGE)
	}
	if err != nil {
		c.releaseWrite()
		return nil, c.failure(err)
	}
	c.writers.Add(1)
	return &clientWriter{WriteCloser: w, c: c}, nil
}

func (c *client) URL() url.URL {
	return c.connection().URL()
}

func (c *client) LocalAddr() net.Addr {
	return c.connection().LocalAddr()
}

func (c *client) RemoteAddr() net.Addr {
	return c.connection().RemoteAddr()
}

func (c *client) RemoteHeader() http.Header {
	return c.connection().RemoteHeader()
}

func (c *client) serve() {
	c.initialize()
	defer func() {
		if closeErr := c.Close(); closeErr != nil {
			logger.Log.Debug("engineio: close connection failed", "err", closeErr)
		}
	}()

	for {
		select {
		case <-c.close:
			return
		case <-time.After(c.params.PingInterval):
		}

		if err := c.acquireWrite(nil); err != nil {
			return
		}
		conn := c.connection()
		w, err := conn.NextWriter(eioparser.String, eioparser.PING)
		if err != nil {
			c.releaseWrite()
			c.pingFailed(err)
			_ = c.failure(err)
			return
		}

		c.mu.Lock()
		c.lastPing = time.Now()
		c.mu.Unlock()
		if err = w.Close(); err != nil {
			c.releaseWrite()
			c.pingFailed(err)
			_ = c.failure(err)
			return
		}

		deadline := time.Now().Add(c.params.PingInterval + c.params.PingTimeout)
		err = conn.SetWriteDeadline(deadline)
		c.mu.Lock()
		c.writeDeadline = deadline
		c.mu.Unlock()
		c.releaseWrite()
		if c.onPing != nil {
			c.onPing()
		}
		if err != nil {
			c.pingFailed(err)
			_ = c.failure(err)
			return
		}
	}
}

// pingFailed logs expected closure at DEBUG: a Close the client started,
// io.EOF or a closed connection, a
// *net.OpError (a peer close, reset or passed deadline), a websocket close frame from
// the peer (websocket.ErrCloseSent after the reply to it, or a *websocket.CloseError),
// or a polling *polling.OpError that is not temporary (the transport stores a request
// failure and closes itself before a writer sees it, or the deadline passed). It is WARN
// otherwise: no caller receives it.
func (c *client) pingFailed(err error) {
	level := slog.LevelWarn
	var netErr *net.OpError
	var payloadErr *polling.OpError
	var closeErr *websocket.CloseError
	select {
	case <-c.close:
		level = slog.LevelDebug
	default:
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.As(err, &netErr) ||
			errors.Is(err, websocket.ErrCloseSent) || errors.As(err, &closeErr) ||
			(errors.As(err, &payloadErr) && !payloadErr.Temporary()) {
			level = slog.LevelDebug
		}
	}
	logger.Log.Log(context.Background(), level, "engineio: ping failed", "err", err)
}

// Writable reports, for an engine client, the same state as Session.Writable.
func (c *client) Writable() bool {
	c.initialize()
	select {
	case <-c.close:
		return false
	default:
	}
	if c.writers.Load() != 0 {
		return false
	}
	c.mu.RLock()
	upgrading := c.upgrading
	conn := c.conn
	c.mu.RUnlock()
	if upgrading {
		return false
	}
	if conn, ok := conn.(interface{ Writable() bool }); ok {
		return conn.Writable()
	}
	return true
}

type clientWriter struct {
	io.WriteCloser
	c    *client
	once sync.Once
	err  error
}

// Frame-body failures arrive after NextReader/NextWriter succeeded. Record the
// first cause before cleanup can close the engine, without replacing the error
// returned by the acquired reader or writer. EOF only ends a normal read frame.
type clientReader struct {
	io.ReadCloser
	c *client
}

func (r *clientReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		_ = r.c.failure(err)
	}
	return n, err
}

func (r *clientReader) Close() error {
	err := r.ReadCloser.Close()
	if err != nil {
		_ = r.c.failure(err)
	}
	return err
}

func (w *clientWriter) Write(p []byte) (int, error) {
	n, err := w.WriteCloser.Write(p)
	if err != nil {
		_ = w.c.failure(err)
	}
	return n, err
}

func (w *clientWriter) Close() error {
	w.once.Do(func() {
		defer w.c.writers.Add(-1)
		defer w.c.releaseWrite()
		w.err = w.WriteCloser.Close()
		if w.err != nil {
			_ = w.c.failure(w.err)
		}
	})
	return w.err
}

// The token covers one whole frame, including Close, and an upgrade's final
// pause/switch. Waiting writers are released by close or probe cancellation.
func (c *client) acquireWrite(cancel <-chan struct{}) error {
	c.initialize()
	select {
	case <-c.close:
		return c.closeError()
	case <-cancel:
		return context.DeadlineExceeded
	case <-c.writeGate:
		return nil
	}
}

func (c *client) releaseWrite() { c.writeGate <- struct{}{} }

type clientProbe struct {
	mu         sync.Mutex
	conn       transport.Conn
	err        error
	committing bool
	done       bool
	cancel     chan struct{}
}

func (c *client) upgradeTransports(started time.Time, u url.URL, header http.Header, transports []transport.Transport, bound time.Duration) {
	if bound <= 0 {
		bound = 10 * time.Second
	}
	if window := c.params.PingInterval + c.params.PingTimeout; window > 0 && window < bound {
		bound = window
	}
	for _, candidate := range transports {
		advertised := false
		for _, name := range c.params.Upgrades {
			advertised = advertised || name == candidate.Name()
		}
		if !advertised {
			continue
		}
		if err := c.probeTransport(started.Add(bound), u, header, candidate); err != nil {
			logger.Log.Debug("engineio: upgrade probe failed", "transport", candidate.Name(), "err", err)
		} else {
			return
		}
		select {
		case <-c.close:
			return
		default:
		}
	}
}

// probeTransport owns the candidate until the switch. Cancellation closes a late
// dial result as well as an established probe, without holding application writes
// during the transport dial or the PING/PONG exchange.
func (c *client) probeTransport(deadline time.Time, u url.URL, header http.Header, candidate transport.Transport) (result error) {
	p := &clientProbe{cancel: make(chan struct{})}
	finished := make(chan struct{})
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	abort := func(err error) {
		p.mu.Lock()
		if p.done || p.err != nil {
			p.mu.Unlock()
			return
		}
		p.err = err
		conn, committing := p.conn, p.committing
		close(p.cancel)
		p.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		if committing {
			_ = c.terminate(ReasonTransportError, err)
		}
	}
	go func() {
		select {
		case <-finished:
		case <-timer.C:
			abort(context.DeadlineExceeded)
		case <-c.close:
			abort(io.ErrClosedPipe)
		}
	}()
	var paused chan struct{}
	var pauser interface {
		Pause()
		Resume()
	}
	held, switched := false, false
	defer func() {
		p.mu.Lock()
		p.done = true
		probe := p.conn
		p.mu.Unlock()
		close(finished)
		if !switched {
			if probe != nil {
				_ = probe.Close()
			}
			if paused != nil {
				// Resume must also cover a pause goroutine scheduled after the
				// cancellation. Wait for its completion before another probe or
				// an application writer can use this transport again.
				tick := time.NewTicker(time.Millisecond)
				defer tick.Stop()
				for {
					pauser.Resume()
					select {
					case <-paused:
						pauser.Resume()
						goto resumed
					case <-tick.C:
					}
				}
			}
		}
	resumed:
		c.mu.Lock()
		if c.probe == probe {
			c.probe = nil
		}
		c.upgrading = false
		c.mu.Unlock()
		if held {
			c.releaseWrite()
		}
	}()

	query := u.Query()
	query.Set("sid", c.params.SID)
	u.RawQuery = query.Encode()
	type dialResult struct {
		conn transport.Conn
		err  error
	}
	dialed := make(chan dialResult, 1)
	go func() {
		conn, err := candidate.Dial(&u, header)
		p.mu.Lock()
		if p.done || p.err != nil {
			p.mu.Unlock()
			if conn != nil {
				_ = conn.Close()
			}
			return
		}
		p.conn = conn
		dialed <- dialResult{conn, err}
		p.mu.Unlock()
	}()
	var probe transport.Conn
	select {
	case <-p.cancel:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.err
	case dial := <-dialed:
		if dial.err != nil {
			return dial.err
		}
		probe = dial.conn
		if limited, ok := probe.(interface{ SetReadLimit(int64) }); ok {
			limited.SetReadLimit(c.maxMessageSize)
		}
	}
	c.mu.Lock()
	select {
	case <-c.close:
		c.mu.Unlock()
		return io.ErrClosedPipe
	default:
	}
	c.probe = probe
	c.mu.Unlock()
	if err := probe.SetReadDeadline(deadline); err != nil {
		return err
	}
	if err := probe.SetWriteDeadline(deadline); err != nil {
		return err
	}
	w, err := probe.NextWriter(eioparser.String, eioparser.PING)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(w, "probe")
	closeErr := w.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	_, pt, r, err := probe.NextReader()
	if err != nil {
		return err
	}
	answer, readErr := io.ReadAll(io.LimitReader(r, 16))
	closeErr = r.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if pt != eioparser.PONG || string(answer) != "probe" {
		return errors.New("engineio: invalid upgrade probe answer")
	}
	if err := c.acquireWrite(p.cancel); err != nil {
		return err
	}
	held = true
	old := c.connection()
	pauser, _ = old.(interface {
		Pause()
		Resume()
	})
	if pauser == nil {
		return errors.New("engineio: current transport cannot pause")
	}
	c.mu.Lock()
	c.upgrading = true
	c.mu.Unlock()
	paused = make(chan struct{})
	go func() { pauser.Pause(); close(paused) }()
	select {
	case <-p.cancel:
		return context.DeadlineExceeded
	case <-paused:
	}
	p.mu.Lock()
	if p.err != nil {
		err := p.err
		p.mu.Unlock()
		return err
	}
	p.committing = true
	p.mu.Unlock()
	w, err = probe.NextWriter(eioparser.String, eioparser.UPGRADE)
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		_ = c.terminate(ReasonTransportError, err)
		return err
	}
	nextDeadline := time.Now().Add(c.params.PingInterval + c.params.PingTimeout)
	if err := probe.SetReadDeadline(nextDeadline); err != nil {
		_ = c.terminate(ReasonTransportError, err)
		return err
	}
	if err := probe.SetWriteDeadline(nextDeadline); err != nil {
		_ = c.terminate(ReasonTransportError, err)
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	c.mu.Lock()
	if c.cause != nil {
		c.mu.Unlock()
		return c.closeError()
	}
	c.conn, c.transport, c.upgraded = probe, candidate.Name(), true
	c.readDeadline, c.writeDeadline = nextDeadline, nextDeadline
	p.done, switched = true, true
	c.mu.Unlock()
	_ = old.Close()
	return nil
}

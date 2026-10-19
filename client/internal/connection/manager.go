package connection

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/sshaplygin/go-socket.io/client/internal/namespace"
	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/parser"
)

type connectAttempt struct {
	stop     chan struct{}
	finished chan struct{}
	once     sync.Once
	err      error
}

func (a *connectAttempt) cancel() { a.once.Do(func() { close(a.stop) }) }

type clientState struct {
	observersMu     sync.RWMutex
	observers       clientObservers
	mu              sync.Mutex
	client          *Manager
	conn            *conn
	sockets         map[string]*Socket
	order           []*Socket
	attempt         *connectAttempt
	retry           *reconnectRun
	reconnectWorker *reconnectRun
	stopped         bool
	closingAll      bool
	rootReady       bool
	buffered        int
}

func (m *clientState) connect(c *Manager) (result error) {
	return m.open(c, nil, 0)
}

func (m *clientState) open(c *Manager, retry *reconnectRun, number int) (result error) {
	m.mu.Lock()
	if retry != nil && (m.retry != retry || m.stopped) {
		m.mu.Unlock()
		return io.ErrClosedPipe
	}
	if attempt := m.attempt; attempt != nil {
		m.mu.Unlock()
		<-attempt.finished
		return attempt.err
	}
	if m.conn != nil && !isDone(m.conn.closing) {
		m.mu.Unlock()
		return nil
	}
	if retry == nil {
		m.cancelRetryLocked()
	}
	previous := m.conn
	previousWorker := m.reconnectWorker
	a := &connectAttempt{stop: make(chan struct{}), finished: make(chan struct{})}
	m.attempt, m.stopped, m.closingAll = a, false, false
	// An explicit Connect starts a new lifecycle. A Socket.Close after this
	// point must remain effective when this attempt's dial eventually returns.
	if retry == nil {
		for _, socket := range m.order {
			socket.active = true
		}
	}
	m.mu.Unlock()
	finishAttempt := func() {
		a.cancel()
		m.mu.Lock()
		if m.attempt == a {
			a.err = result
			m.attempt = nil
			close(a.finished)
		}
		m.mu.Unlock()
	}
	defer finishAttempt()
	type dialResult struct {
		conn engineclient.Conn
		err  error
	}
	dialed := make(chan dialResult)
	go func() {
		conn, err := c.engineDial()
		select {
		case dialed <- dialResult{conn, err}:
		case <-a.stop:
			if conn != nil {
				_ = conn.Close()
			}
		}
	}()
	var timeout <-chan time.Time
	if c.options.Timeout > 0 {
		timer := time.NewTimer(c.options.Timeout)
		defer timer.Stop()
		timeout = timer.C
	}
	var engine engineclient.Conn
	select {
	case <-a.stop:
		return io.ErrClosedPipe
	case <-timeout:
		return ErrConnectTimeout
	case dial := <-dialed:
		if dial.err != nil {
			return dial.err
		}
		engine = dial.conn
	}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		_ = engine.Close()
		return io.ErrClosedPipe
	}
	current := newConn(engine, c.handlers, c.limits, c.log.With("sid", engine.ID()))
	current.owner = m
	// An inactive root has unsubscribed from manager lifecycle notifications.
	// Keep the cache for an active root or one whose Socket does not exist yet.
	if root := m.sockets[rootNamespace]; root == nil || root.active {
		if err := current.connectClient(); err != nil {
			m.mu.Unlock()
			current.connected(err)
			return err
		}
	}
	m.conn, m.rootReady = current, false
	for _, socket := range m.order {
		if !socket.active {
			continue
		}
		socket.connected, socket.requested = false, false
		socket.nc, socket.connectPending, socket.connectQueued = nil, false, false
	}
	m.mu.Unlock()
	current.connected(nil)
	// Release callers joining this attempt before invoking a callback that may
	// itself call Connect or Close. No connection workers have started yet.
	if retry != nil {
		finishAttempt()
		if f := c.observerSnapshot().reconnect; f != nil {
			f(number)
		}
	}
	m.mu.Lock()
	if m.conn == current && !m.stopped {
		for _, socket := range m.order {
			if !socket.active || socket.requested {
				continue
			}
			socket.requested = true
			if socket.namespace != rootNamespace {
				current.writeControl(socketConnectHeader(socket))
			}
		}
	}
	if retry != nil && m.retry == retry {
		m.retry = nil
	}
	m.mu.Unlock()
	// Publish completion after CONNECT controls are queued but before a worker
	// can observe EOF and decide whether this connection needs recovery.
	finishAttempt()
	start := func() {
		go current.observeEngineClose()
		go current.serveError()
		go current.serveWrite()
		go current.serveRead(clientConnectPacketHandler, clientDisconnectPacketHandler)
		go m.connectionEnded(current)
	}
	if previous != nil || previousWorker != nil {
		go func() {
			if previous != nil {
				<-previous.readStopped
				<-previous.writeStopped
				<-previous.errorStopped
			}
			if previousWorker != nil {
				<-previousWorker.done
			}
			start()
		}()
	} else {
		start()
	}
	return nil
}

// ErrConnectTimeout means the engine connection did not open within options.Timeout.
var ErrConnectTimeout = errors.New("client: connect timeout")

// Manager owns namespace sockets, connection generations and reconnect work.
type Manager struct {
	namespace string
	url       string
	handlers  *namespace.Registry
	limits    connLimits
	log       *slog.Logger
	options   Config
	state     *clientState
	dial      func(string) (engineclient.Conn, error)
}

// Connect returns once Engine.IO is open and namespace CONNECT packets are queued.
// A failed or timed-out attempt can be retried; late dial results are closed.
func (c *Manager) Connect() error { return c.state.connect(c) }

// Close disconnects all namespace sockets and drains their queued packets before
// closing Engine.IO, bounded by WithCloseTimeout.
func (c *Manager) Close() error { return c.state.closeAll() }

// Emit sends through the namespace selected by the address passed to New.
func (c *Manager) Emit(event string, args ...interface{}) { c.Socket(c.namespace).Emit(event, args...) }

// OnConnect sets the default namespace's connection callback.
func (c *Manager) OnConnect(f func(namespace.Conn) error) { c.Socket(c.namespace).OnConnect(f) }

// OnDisconnect sets the default namespace's disconnect callback.
func (c *Manager) OnDisconnect(f func(namespace.Conn, string)) { c.Socket(c.namespace).OnDisconnect(f) }

// OnError sets the default namespace's error callback.
func (c *Manager) OnError(f func(namespace.Conn, error)) { c.Socket(c.namespace).OnError(f) }

// OnEvent replaces the default namespace's handler for event.
func (c *Manager) OnEvent(event string, f interface{}) { c.Socket(c.namespace).OnEvent(event, f) }

func (c *Manager) createNamespace(ns string) *namespace.Handler {
	if h := c.getNamespace(ns); h != nil {
		return h
	}
	h := namespace.NewHandler(ns)
	c.handlers.Set(ns, h)
	return h
}

func (c *Manager) getNamespace(ns string) *namespace.Handler {
	h, _ := c.handlers.Get(ns)
	return h
}

func (c *conn) connectClient() error {
	rootHandler, ok := c.handlers.Get(rootNamespace)
	if !ok {
		return errUnavailableRootHandler
	}
	root := newNamespaceConn(c, aliasRootNamespace, rootHandler.Rooms())
	c.namespaces.Set(rootNamespace, root)
	root.Join(c.Conn.ID())
	root.SetContext(c.Conn.Context())
	return nil
}

func (c *Manager) engineDial() (engineclient.Conn, error) {
	if c.dial != nil {
		return c.dial(c.url)
	}
	d := engineclient.Dialer{MaxMessageSize: c.options.MaxMessageSize, Transports: c.options.Transports, Upgrade: true, OnPing: c.ping, OnPong: c.pong}
	return d.Dial(c.url, c.options.Header)
}

func socketConnectHeader(s *Socket) parser.Header {
	nsp := s.namespace
	if s.query != "" {
		nsp += "?" + s.query
	}
	return parser.Header{Type: parser.Connect, Namespace: nsp}
}

// Config contains required, already resolved per-instance manager inputs.
type Config struct {
	MaxMessageSize       int64
	Transports           []transport.Transport
	Header               http.Header
	Timeout              time.Duration
	WriteBufferSize      int
	CloseTimeout         time.Duration
	DisableReconnection  bool
	ReconnectionAttempts int
	ReconnectionDelay    time.Duration
	ReconnectionDelayMax time.Duration
	RandomizationFactor  float64
}

// NewManager binds resolved configuration and the optional custom dial dependency.
// A nil dial selects the Engine.IO client dialer.
func NewManager(address, defaultNamespace string, config Config, log *slog.Logger, dial func(string) (engineclient.Conn, error)) *Manager {
	c := &Manager{namespace: defaultNamespace, url: address, handlers: namespace.NewRegistry(),
		limits: connLimits{config.WriteBufferSize, config.CloseTimeout}, log: log, options: config, dial: dial}
	c.state = &clientState{client: c, sockets: make(map[string]*Socket)}
	c.createNamespace(rootNamespace)
	c.Socket(defaultNamespace)
	return c
}

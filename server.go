package socketio

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/engineio"

	"github.com/sshaplygin/go-socket.io/logger"
)

// Server is a go-socket.io server and HTTP handler. Run Serve to dispatch
// connections and events; HTTP handshakes can succeed without it and do not
// report the missing dispatcher as an error.
type Server struct {
	engine *engineio.Server

	handlers *namespaceRegistry
	views    map[string]*ServerNamespace
	creating map[string]chan struct{}

	adapter    adapter.Factory
	parentsMu  sync.RWMutex
	parents    []*dynamicNamespace
	createMu   sync.Mutex            // serialises handler creation, not dispatch; see createNamespace
	closed     atomic.Bool           // set by Close; read under createMu by createNamespace
	adapterErr atomic.Pointer[error] // the first adapter construction error, read by Serve
	served     chan struct{}         // tests set it; Serve closes it after its entry check

	closeOnce     sync.Once
	closeDone     chan struct{}
	closeErr      error
	activeMu      sync.Mutex
	active        map[*connection]struct{}
	activeChanged chan struct{}

	limits connLimits
	log    *slog.Logger
}

// NewServer returns a server.
func NewServer(opts ...Option) *Server {
	config := resolveOptions(opts...)
	return &Server{
		handlers:      newNamespaceRegistry(),
		views:         make(map[string]*ServerNamespace),
		creating:      make(map[string]chan struct{}),
		adapter:       config.Adapter,
		active:        make(map[*connection]struct{}),
		activeChanged: make(chan struct{}),
		closeDone:     make(chan struct{}),
		engine:        engineio.NewServer(config.EngineOptions...),
		limits:        newConnLimits(config),
		log:           loggerFrom(config),
	}
}

// loggerFrom wraps this server's configured logger or selects logger.Log.
func loggerFrom(opts *options) *slog.Logger {
	if opts != nil && opts.Logger != nil {
		return logger.Wrap(opts.Logger)
	}
	return logger.Log
}

// Close closes engine sessions, waits for OnDisconnect handlers and closes namespace
// adapters implementing io.Closer. It waits for in-progress namespace construction.
// The wait is bounded by ten seconds; on timeout it logs remaining connections.
// A factory configured after Close is not invoked for new namespaces. Serve returns nil.
// Completed shutdown returns the engine and adapter close errors joined together.
func (s *Server) Close() error { return s.closeWithTimeout(10 * time.Second) }

func (s *Server) closeWithTimeout(timeout time.Duration) error {
	s.closed.Store(true)
	s.closeOnce.Do(func() {
		go func() {
			s.closeErr = s.engine.Close()
			s.activeMu.Lock()
			active := make([]*connection, 0, len(s.active))
			for c := range s.active {
				active = append(active, c)
			}
			s.activeMu.Unlock()
			for _, c := range active {
				_ = c.Conn.Close()
			}
			s.createMu.Lock()
			building := make([]chan struct{}, 0, len(s.creating))
			for _, ready := range s.creating {
				building = append(building, ready)
			}
			s.createMu.Unlock()
			for _, ready := range building {
				<-ready
			}
			s.createMu.Lock()
			var closers []io.Closer
			s.handlers.Range(func(h *namespaceHandler) {
				if closer, ok := h.Adapter().(io.Closer); ok {
					closers = append(closers, closer)
				}
			})
			s.createMu.Unlock()
			// External adapters may call back into namespace or configuration APIs.
			for _, closer := range closers {
				s.closeErr = errors.Join(s.closeErr, closer.Close())
			}
			close(s.closeDone)
		}()
	})
	return s.waitForConnections(timeout)
}

// ServeHTTP handles Engine.IO handshakes and transport requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.engine.ServeHTTP(w, r)
}

// OnConnect set a handler function f to handle open event for namespace.
func (s *Server) OnConnect(namespace string, f func(Conn) error) {
	h := s.getNamespace(namespace)
	if h == nil {
		h = s.createNamespace(namespace)
	}

	h.OnConnect(f)
}

// OnDisconnect sets the namespace's disconnect handler. Its reason is the peer's
// namespace disconnect, the engine's transport close/error or ping timeout, or
// forced close for an application close, shutdown or parser failure. Rooms have
// been left before it runs; OnEvent(namespace, "disconnecting", func(Conn, string))
// observes the same reason while the rooms are still joined.
func (s *Server) OnDisconnect(namespace string, f func(Conn, string)) {
	h := s.getNamespace(namespace)
	if h == nil {
		h = s.createNamespace(namespace)
	}

	h.OnDisconnect(f)
}

// OnError sets the namespace error handler. Root connect and connection-middleware
// failures pass a nil Conn; errors for other unjoined namespaces pass an inactive
// Conn. A panic in the error handler is recovered and logged at WARN.
func (s *Server) OnError(namespace string, f func(Conn, error)) {
	h := s.getNamespace(namespace)
	if h == nil {
		h = s.createNamespace(namespace)
	}

	h.OnError(f)
}

// OnEvent sets the handler f for event in namespace.
//
// f must be a func whose first parameter is Conn; the remaining parameters are
// decoded from the event arguments. If the client asked for an
// acknowledgement, the return values are sent back as the ACK payload. A final
// AckFunc parameter takes ownership of the ACK and suppresses automatic returns;
// it may be called later, once, from another goroutine. OnEvent replaces every
// listener of this event (see ServerNamespace.On and Once).
// f is called on the read goroutine of the connection, so a blocking f delays only
// that client. OnEvent panics if f is not a func or its first parameter is not Conn.
func (s *Server) OnEvent(namespace, event string, f interface{}) {
	h := s.getNamespace(namespace)
	if h == nil {
		h = s.createNamespace(namespace)
	}

	h.OnEvent(event, f)
}

// Serve accepts and serves connections until Close is called, then returns nil.
// Run it alongside the HTTP server: without Serve, connections and events are not
// dispatched, even though HTTP handshakes can succeed without reporting an error.
// If a namespace's adapter could not be created before Serve was called (see
// SetAdapter) and Close has not been called, Serve returns that error at once
// without accepting a connection. The engine.io server then still completes
// handshakes, which nobody serves, until Close is called.
func (s *Server) Serve() error {
	if s.closed.Load() {
		return nil
	}
	if err := s.adapterErr.Load(); err != nil {
		return *err
	}
	if s.served != nil {
		close(s.served)
	}
	for {
		conn, err := s.engine.Accept()
		if errors.Is(err, io.EOF) {
			// Accept reports io.EOF once Close has been called.
			return nil
		}
		if err != nil {
			return err
		}

		go s.serveConn(conn)
	}
}

// broadcastOf returns the broadcast of namespace, or nil when the namespace has no
// handler or its adapter could not be created.
func (s *Server) broadcastOf(namespace string) adapter.Adapter {
	if h := s.getNamespace(namespace); h != nil && h.Err() == nil {
		return h.Adapter()
	}
	return nil
}

// JoinRoom joins a live connection owned by this server to the room.
func (s *Server) JoinRoom(namespace string, room string, connection Conn) bool {
	nc := s.roomSocket(namespace, connection)
	if nc == nil {
		return false
	}
	defer nc.endRoomOperation()
	nc.broadcast.Join(room, nc)
	return true
}

// LeaveRoom removes a live connection owned by this server from the room.
func (s *Server) LeaveRoom(namespace string, room string, connection Conn) bool {
	nc := s.roomSocket(namespace, connection)
	if nc == nil {
		return false
	}
	defer nc.endRoomOperation()
	nc.broadcast.Leave(room, nc)
	return true
}

// LeaveAllRooms removes a live connection owned by this server from all rooms.
func (s *Server) LeaveAllRooms(namespace string, connection Conn) bool {
	nc := s.roomSocket(namespace, connection)
	if nc == nil {
		return false
	}
	defer nc.endRoomOperation()
	nc.broadcast.LeaveAll(nc)
	return true
}

// roomSocket admits an operation against this server's concrete namespace
// generation. Retirement waits for the operation before reusing its socket ID.
func (s *Server) roomSocket(nsp string, conn Conn) *namespaceConn {
	nc, ok := conn.(*namespaceConn)
	h := s.getNamespace(nsp)
	if !ok || nc == nil || h == nil || h.Err() != nil {
		return nil
	}
	h.connectedMu.RLock()
	owned := h.connected[nc.ID()] == nc
	h.connectedMu.RUnlock()
	if owned && nc.Connected() && nc.beginRoomOperation() {
		return nc
	}

	return nil
}

// ClearRoom clears the room.
func (s *Server) ClearRoom(namespace string, room string) bool {
	bc := s.broadcastOf(namespace)
	if bc != nil {
		bc.Clear(room)
	}
	return bc != nil
}

// BroadcastToRoom broadcasts to the room. The result reports only that the namespace
// is registered and its adapter was constructed, even if the adapter is now closed;
// it does not report delivery. The memory adapter registers a trailing callback per
// socket. The Redis adapter rejects a broadcast with a trailing callback before any
// delivery; its failures are logged at WARN, or at DEBUG once the adapter is closed.
// With Redis, a stalled PUBLISH can block the call up to the
// [github.com/sshaplygin/go-socket.io/adapter/redis.WithRequestsTimeout] bound per I/O
// phase, as for SendTo.
func (s *Server) BroadcastToRoom(namespace string, room, event string, args ...interface{}) bool {
	bc := s.broadcastOf(namespace)
	if bc != nil {
		bc.Send(room, event, args...)
	}
	return bc != nil
}

// BroadcastToNamespace broadcasts to the namespace. The result reports only that the
// namespace is registered and its adapter was constructed, even if the adapter is now
// closed; it does not report delivery. The memory adapter registers a trailing callback
// per socket. The Redis adapter rejects a broadcast with a trailing callback before any
// delivery; its failures are logged at WARN, or at DEBUG once the adapter is closed.
// With Redis, a stalled PUBLISH can block the call up to the
// [github.com/sshaplygin/go-socket.io/adapter/redis.WithRequestsTimeout] bound per I/O
// phase, as for SendTo.
func (s *Server) BroadcastToNamespace(namespace string, event string, args ...interface{}) bool {
	bc := s.broadcastOf(namespace)
	if bc != nil {
		bc.SendAll(event, args...)
	}
	return bc != nil
}

// RoomLen delegates the room count to its namespace adapter. It returns -1 when
// there is no namespace handler or adapter construction failed.
func (s *Server) RoomLen(namespace string, room string) int {
	if bc := s.broadcastOf(namespace); bc != nil {
		return bc.Len(room)
	}
	return -1
}

// Rooms delegates the namespace room query to its adapter. It returns nil when
// there is no namespace handler or adapter construction failed.
func (s *Server) Rooms(namespace string) []string {
	if bc := s.broadcastOf(namespace); bc != nil {
		return bc.Rooms(nil)
	}
	return nil
}

// Count number of connections.
func (s *Server) Count() int {
	return s.engine.Count()
}

// Engine returns the engine.io server created by NewServer; never nil.
func (s *Server) Engine() *engineio.Server { return s.engine }

// Remove unregisters an Engine.IO session without closing its transport.
// Server.Close still closes unregistered sessions that remain open.
func (s *Server) Remove(sid string) {
	s.engine.Remove(sid)
}

// ForEach visits the local members of room through their minimal adapter.Socket contract.
func (s *Server) ForEach(namespace string, room string, f adapter.EachFunc) bool {
	bc := s.broadcastOf(namespace)
	if bc != nil {
		bc.ForEach(room, f)
	}
	return bc != nil
}

// createNamespace serializes creation per name while factories run without the
// server registry lock. Waiters share the completed handler and adapter.
func (s *Server) createNamespace(nsp string) *namespaceHandler {
	return s.createNamespaceWithParent(serverNamespaceName(nsp), nil)
}

func (s *Server) createNamespaceWithParent(nsp string, parent *namespaceHandler) *namespaceHandler {
	s.createMu.Lock()
	if handler, ok := s.handlers.Get(nsp); ok {
		s.createMu.Unlock()
		return handler
	}
	if ready := s.creating[nsp]; ready != nil {
		s.createMu.Unlock()
		<-ready
		handler, _ := s.handlers.Get(nsp)
		return handler
	}
	ready := make(chan struct{})
	s.creating[nsp] = ready
	factory, closed := s.adapter, s.closed.Load()
	s.createMu.Unlock()
	handler := newNamespaceHandler()
	handler.name, handler.log = nsp, s.log
	if closed && factory != nil {
		handler.SetAdapter(nil, errServerClosed)
	} else if factory != nil {
		bc, err := factory(nsp)
		if err == nil && bc == nil {
			err = errors.New("adapter returned nil")
		}
		if err != nil {
			handler.SetAdapter(nil, fmt.Errorf("socketio: broadcast of namespace %q: %w", cmp.Or(nsp, aliasRootNamespace), err))
		} else {
			handler.SetAdapter(bc, nil)
		}
	}
	if parent != nil {
		parent.CopyRegistrations(handler)
	}
	s.createMu.Lock()
	defer s.createMu.Unlock()
	if err := handler.Err(); err != nil && !s.closed.Load() && s.adapterErr.Load() == nil {
		s.adapterErr.Store(&err)
	}
	s.views[nsp] = &ServerNamespace{server: s, name: nsp}
	s.handlers.Set(nsp, handler)
	delete(s.creating, nsp)
	close(ready)
	return handler
}

// SetAdapter installs the factory for namespaces created after this call. Nil
// restores the memory adapter. Existing namespaces keep their adapter.
func (s *Server) SetAdapter(f adapter.Factory) {
	s.createMu.Lock()
	defer s.createMu.Unlock()
	s.adapter = f
}

func (s *Server) resolveNamespace(name string, query url.Values) *namespaceHandler {
	s.parentsMu.RLock()
	parents := append([]*dynamicNamespace(nil), s.parents...)
	s.parentsMu.RUnlock()
	for _, parent := range parents {
		if parent.match == nil || !parent.match(name, cloneQuery(query)) {
			continue
		}
		h := s.createNamespaceWithParent(name, parent.registration)
		s.createMu.Lock()
		view := s.views[name]
		s.createMu.Unlock()
		parent.mu.Lock()
		parent.children[name] = view
		parent.mu.Unlock()
		return h
	}
	return nil
}

func (s *Server) getNamespace(nsp string) *namespaceHandler {
	nsp = serverNamespaceName(nsp)
	ret, ok := s.handlers.Get(nsp)
	if !ok {
		return nil
	}

	return ret
}

func (s *Server) releaseConnection(c *connection) {
	// finish has closed registration before this wait, so every disconnect has
	// already registered its callback, including calls from application goroutines.
	c.Wait()
	s.activeMu.Lock()
	delete(s.active, c)
	close(s.activeChanged)
	s.activeChanged = make(chan struct{})
	s.activeMu.Unlock()
}

func (s *Server) waitForConnections(timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	engineDone := s.closeDone
	closed := false
	for {
		s.activeMu.Lock()
		remaining, changed := len(s.active), s.activeChanged
		s.activeMu.Unlock()
		if closed && remaining == 0 {
			return s.closeErr
		}
		select {
		case <-engineDone:
			closed = true
			engineDone = nil
		case <-changed:
		case <-timer.C:
			s.log.Warn("socketio: close timed out", "err", fmt.Errorf("%d connections still open", remaining))
			return nil
		}
	}
}

func (s *Server) serveConn(conn engineio.Conn) {
	s.createNamespace(rootNamespace)
	c := newConnection(conn, s.handlers, s.limits.writeBufferSize, s.limits.drainTimeout, s.log.With("sid", conn.ID()))
	s.activeMu.Lock()
	if s.closed.Load() {
		s.activeMu.Unlock()
		_ = conn.Close()
		return
	}
	s.active[c] = struct{}{}
	s.activeMu.Unlock()
	if !c.Start(s.resolveNamespace) {
		s.engine.Remove(conn.ID())
		s.releaseConnection(c)
		return
	}
	go func() {
		defer s.releaseConnection(c)
		c.Read()
		s.engine.Remove(c.Conn.ID())
	}()
}

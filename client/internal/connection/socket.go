package connection

import (
	"reflect"
	"strings"
	"sync"

	"github.com/sshaplygin/go-socket.io/client/internal/namespace"
	"github.com/sshaplygin/go-socket.io/parser"
)

type namespaceConn struct {
	*conn
	broadcast *namespace.RoomIndex
	namespace string
	context   interface{}
	ack       sync.Map
	outMu     sync.Mutex
}

func newNamespaceConn(conn *conn, namespace string, broadcast *namespace.RoomIndex) *namespaceConn {
	if namespace == rootNamespace {
		namespace = aliasRootNamespace
	}
	return &namespaceConn{conn: conn, namespace: namespace, broadcast: broadcast}
}

// Handshake returns zero metadata on a client connection.
func (nc *namespaceConn) Handshake() namespace.Handshake { return namespace.Handshake{} }

func (nc *namespaceConn) SetContext(ctx interface{}) {
	nc.context = ctx
}

func (nc *namespaceConn) Context() interface{} {
	return nc.context
}

func (nc *namespaceConn) Namespace() string {
	return nc.namespace
}

// Connected reports the state of this connection generation's namespace.
func (nc *namespaceConn) Connected() bool {
	if m := nc.conn.owner; m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		s := m.sockets[fmtNS(nc.namespace)]
		return s != nil && s.nc == nc && s.connected
	}
	current, ok := nc.conn.namespaces.Get(fmtNS(nc.namespace))
	return ok && current == nc && !isDone(nc.conn.closing)
}

func (nc *namespaceConn) Emit(eventName string, v ...interface{}) {
	if socket := nc.socket(); socket != nil {
		socket.emit(false, eventName, v...)
		return
	}
	nc.emit(false, eventName, v...)
}

func (nc *namespaceConn) socket() *Socket {
	if m := nc.conn.owner; m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.sockets[fmtNS(nc.namespace)]
	}
	return nil
}

func (nc *namespaceConn) writable() bool {
	if c, ok := nc.Conn.(interface{ Writable() bool }); ok {
		return c.Writable()
	}
	return true
}

func (nc *namespaceConn) emit(volatile bool, eventName string, v ...interface{}) {
	nc.emitFlags(volatile, false, eventName, v...)
}

func (nc *namespaceConn) emitFlags(volatile, noCompress bool, eventName string, v ...interface{}) {
	if volatile && !nc.writable() {
		return
	}
	nc.queueEventFlags(volatile, noCompress, eventName, v...)
}

func (nc *namespaceConn) queueEventFlags(volatile, noCompress bool, eventName string, v ...interface{}) {
	header := parser.Header{
		Type: parser.Event,
	}

	if nc.namespace != aliasRootNamespace {
		header.Namespace = nc.namespace
	}

	var ack *namespace.Func
	if l := len(v); l > 0 {
		last := v[l-1]
		lastV := reflect.TypeOf(last)

		if lastV != nil && lastV.Kind() == reflect.Func {
			ack = namespace.NewAckFunc(last)
			v = v[:l-1]
		}
	}

	args := make([]reflect.Value, len(v)+1)
	args[0] = reflect.ValueOf(eventName)

	for i := 1; i < len(args); i++ {
		args[i] = reflect.ValueOf(v[i-1])
	}

	if !volatile {
		nc.registerAck(&header, ack)
		nc.conn.writeFlags(header, noCompress, args...)
		return
	}
	// The transport callback runs outside queue locks; admission rechecks the
	// queue atomically afterwards so a concurrent normal/volatile emit cannot
	// turn this best-effort packet into an overflow or retain a dropped ACK.
	nc.outMu.Lock()
	defer nc.outMu.Unlock()
	nc.conn.mu.Lock()
	defer nc.conn.mu.Unlock()
	c := nc.conn
	if len(c.writeChan) != 0 || len(c.controls) != 0 ||
		isDone(c.closing) && (!c.draining || c.appSealed || isDone(c.discard)) {
		return
	}
	nc.registerAck(&header, ack)
	c.queueFlagsLocked(header, noCompress, args...)
}

func (nc *namespaceConn) registerAck(header *parser.Header, ack *namespace.Func) {
	if ack != nil {
		header.ID = nc.conn.nextID()
		header.NeedAck = true
		nc.ack.Store(header.ID, ack)
	}
}

func (nc *namespaceConn) Join(rooms ...string) {
	for _, room := range rooms {
		nc.broadcast.Join(room, nc)
	}
}

func (nc *namespaceConn) Leave(room string) {
	nc.broadcast.Leave(room, nc)
}

func (nc *namespaceConn) LeaveAll() {
	nc.broadcast.LeaveAll(nc)
}

func (nc *namespaceConn) Rooms() []string {
	return nc.broadcast.Rooms(nc)
}

func (nc *namespaceConn) ID() string {
	if namespace := fmtNS(nc.namespace); namespace != rootNamespace {
		return namespace + "#" + nc.Conn.ID()
	}
	return nc.Conn.ID()
}

// Close disconnects every namespace and closes the engine connection.
func (nc *namespaceConn) Close() error {
	if nc.conn.owner != nil {
		return nc.conn.owner.closeAll()
	}
	return nc.conn.Close()
}

func (nc *namespaceConn) Send(args ...interface{}) { nc.Emit("message", args...) }

// Disconnect sends DISCONNECT for this namespace, leaving the other sockets open.
func (nc *namespaceConn) Disconnect() {
	if socket := nc.socket(); socket != nil {
		_ = socket.Close()
	}
}

// Use does not register middleware on a client connection.
func (nc *namespaceConn) Use(func(string, []interface{}) error) {
	return //nolint:staticcheck // S1023: an explicit statement lets coverage verify this no-op API.
}

const aliasRootNamespace, rootNamespace = "/", ""

func fmtNS(ns string) string {
	if ns == aliasRootNamespace {
		return rootNamespace
	}
	return ns
}

type namespaces struct {
	namespaces map[string]*namespaceConn
	mu         sync.RWMutex
}

func newNamespaces() *namespaces {
	return &namespaces{
		namespaces: make(map[string]*namespaceConn),
	}
}

func (n *namespaces) Get(ns string) (*namespaceConn, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	namespace, ok := n.namespaces[ns]
	return namespace, ok
}

func (n *namespaces) Set(ns string, conn *namespaceConn) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.namespaces[ns] = conn
}

func (n *namespaces) Delete(ns string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	delete(n.namespaces, ns)
}

func (n *namespaces) Range(fn func(ns string, nc *namespaceConn)) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	for ns, nc := range n.namespaces {
		fn(ns, nc)
	}
}

type bufferedEvent struct {
	event      string
	args       []interface{}
	noCompress bool
}

// Socket is one namespace on a Client. Its callbacks receive that namespace's namespace.Conn.
type Socket struct {
	state          *clientState
	namespace      string
	query          string
	handler        *namespace.Handler
	nc             *namespaceConn
	active         bool
	connected      bool
	requested      bool
	connectPending bool
	connectQueued  bool
	buffer         []bufferedEvent
}

// Socket returns the namespace socket, creating it. A namespace query is carried
// by its CONNECT packet; a socket added after Connect starts joining immediately.
func (c *Manager) Socket(nsp string) *Socket {
	namespace, query, _ := strings.Cut(nsp, "?")
	if namespace != "" && !strings.HasPrefix(namespace, "/") {
		namespace = "/" + namespace
	}
	namespace = fmtNS(namespace)
	m := c.state
	m.mu.Lock()
	defer m.mu.Unlock()
	if socket := m.sockets[namespace]; socket != nil {
		return socket
	}
	s := &Socket{state: m, namespace: namespace, query: query, active: true, handler: c.createNamespace(namespace)}
	m.sockets[namespace] = s
	m.order = append(m.order, s)
	if current := m.conn; current != nil && !m.stopped && !isDone(current.closing) {
		s.requested = true
		if namespace != rootNamespace {
			current.writeControl(socketConnectHeader(s))
		}
		if namespace == rootNamespace && m.rootReady {
			s.nc, _ = current.namespaces.Get(rootNamespace)
			s.connected, s.connectPending = true, true
		}
	}
	return s
}

// Namespace returns the namespace path, without its CONNECT query.
func (s *Socket) Namespace() string {
	if s.namespace == rootNamespace {
		return aliasRootNamespace
	}
	return s.namespace
}

// ID is the server-assigned namespace ID, or empty while disconnected.
func (s *Socket) ID() string {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if s.connected && s.nc != nil {
		return s.nc.ID()
	}
	return ""
}

// Connected reports whether the server has acknowledged this namespace.
func (s *Socket) Connected() bool {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return s.connected
}

// On appends a listener and returns a function removing just that registration.
func (s *Socket) On(event string, f interface{}) (off func()) {
	return s.handler.AddListener(event, f, false)
}

// Once appends a listener removed before its first call.
func (s *Socket) Once(event string, f interface{}) (off func()) {
	return s.handler.AddListener(event, f, true)
}

// Off removes every listener of event, including the OnEvent registration.
func (s *Socket) Off(event string) { s.handler.Off(event) }

// Emit queues an event until the namespace CONNECT reply. A full pre-connect
// buffer drops the event and logs a warning; connected writes use the normal queue.
func (s *Socket) Emit(event string, args ...interface{}) { s.emit(false, event, args...) }

func (s *Socket) emit(volatile bool, event string, args ...interface{}) {
	s.emitFlags(volatile, false, event, args...)
}

func (s *Socket) emitFlags(volatile, noCompress bool, event string, args ...interface{}) {
	if volatile {
		s.emitVolatileFlags(noCompress, event, args...)
		return
	}
	m := s.state
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.connected && s.nc != nil {
		s.nc.emitFlags(volatile, noCompress, event, args...)
		return
	}
	if m.buffered >= m.client.limits.writeBufferSize {
		m.client.log.Warn("socketio: emit buffer full", nspAttr(s.namespace), "event", event)
		return
	}
	s.buffer = append(s.buffer, bufferedEvent{event: event, args: append([]interface{}(nil), args...), noCompress: noCompress})
	m.buffered++
}

// A custom transport's Writable method may reenter this Socket. Snapshot it
// without holding state.mu, then recheck the namespace generation before admission.
func (s *Socket) emitVolatileFlags(noCompress bool, event string, args ...interface{}) {
	m := s.state
	m.mu.Lock()
	nc, connected := s.nc, s.connected
	m.mu.Unlock()
	if !connected || nc == nil || !nc.writable() {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.connected && s.nc == nc {
		nc.queueEventFlags(true, noCompress, event, args...)
	}
}

// Send is Emit("message", args...).
func (s *Socket) Send(args ...interface{}) { s.Emit("message", args...) }

// OnConnect sets the namespace's connection callback.
func (s *Socket) OnConnect(f func(namespace.Conn) error) {
	s.handler.OnConnect(f)
	m := s.state
	m.mu.Lock()
	current := m.conn
	queue := f != nil && s.connected && s.connectPending && !s.connectQueued && current != nil
	if queue {
		s.connectQueued = true
	}
	m.mu.Unlock()
	if queue {
		current.queueReadTask(func() {
			if err := m.deliverConnect(current, s); err != nil {
				current.report(s.namespace, err, nil)
				current.stop()
			}
		})
	}
}

// OnDisconnect sets the namespace's disconnect callback.
func (s *Socket) OnDisconnect(f func(namespace.Conn, string)) { s.handler.OnDisconnect(f) }

// OnError sets the namespace's error callback.
func (s *Socket) OnError(f func(namespace.Conn, error)) { s.handler.OnError(f) }

// OnEvent replaces the namespace's handler for event.
func (s *Socket) OnEvent(event string, f interface{}) { s.handler.OnEvent(event, f) }

// Close sends DISCONNECT for this namespace. The last socket closes Engine.IO.
func (s *Socket) Close() error { return s.state.closeSocket(s, "io client disconnect", true) }

func (m *clientState) namespaceConnected(current *conn, namespace string, nc *namespaceConn) error {
	m.mu.Lock()
	if current != m.conn {
		m.mu.Unlock()
		return nil
	}
	if namespace == rootNamespace {
		m.rootReady = true
	}
	s := m.sockets[namespace]
	if s == nil || !s.active {
		m.mu.Unlock()
		if s != nil {
			current.claim(namespace, nc)
			nc.LeaveAll()
		}
		return nil
	}
	if s.connected {
		m.mu.Unlock()
		return nil
	}
	s.nc, s.connected, s.connectPending = nc, true, true
	buffer := s.buffer
	s.buffer = nil
	m.buffered -= len(buffer)
	for _, event := range buffer {
		nc.emitFlags(false, event.noCompress, event.event, event.args...)
	}
	m.mu.Unlock()
	return m.deliverConnect(current, s)
}

func (m *clientState) deliverConnect(current *conn, s *Socket) error {
	m.mu.Lock()
	f := s.handler.ConnectHandler()
	if current != m.conn || !s.connected || !s.connectPending || f == nil {
		s.connectQueued = false
		m.mu.Unlock()
		return nil
	}
	nc := s.nc
	s.connectPending, s.connectQueued = false, false
	m.mu.Unlock()
	return f(nc)
}

func (m *clientState) namespaceDisconnected(current *conn, namespace string) error {
	m.mu.Lock()
	if m.conn != current {
		m.mu.Unlock()
		return nil
	}
	if namespace == rootNamespace {
		m.rootReady = false
	}
	socket := m.sockets[namespace]
	if socket == nil {
		// Root is established by the server before its Socket may exist.
		// Release its cached namespace.Conn as soon as that namespace is disconnected.
		nc, claimed := current.claim(namespace)
		m.mu.Unlock()
		if claimed {
			nc.LeaveAll()
		}
		return nil
	}
	m.mu.Unlock()
	return m.closeSocket(socket, "io server disconnect", true, current)
}

func (m *clientState) closeSocket(s *Socket, reason string, closeLast bool, expected ...*conn) error {
	m.mu.Lock()
	if len(expected) > 0 && m.conn != expected[0] {
		m.mu.Unlock()
		return nil
	}
	if !s.active {
		m.mu.Unlock()
		return nil
	}
	current, nc, connected := m.conn, s.nc, s.connected
	s.active, s.connected, s.connectPending = false, false, false
	s.nc = nil
	claimed := false
	if current != nil && !isDone(current.closing) {
		var pending *namespaceConn
		pending, claimed = current.claim(s.namespace)
		if nc == nil {
			nc = pending
		}
		if s.requested && reason == "io client disconnect" {
			current.writeControl(parser.Header{Type: parser.Disconnect, Namespace: s.namespace})
		}
	}
	last := true
	for _, other := range m.order {
		last = last && !other.active
	}
	if last && closeLast {
		m.stopped = true
		m.cancelRetryLocked()
		if m.attempt != nil {
			m.attempt.cancel()
		}
	}
	m.mu.Unlock()
	if nc != nil {
		nc.LeaveAll()
	}
	if connected && claimed {
		if f := s.handler.DisconnectHandler(); f != nil {
			f(nc, reason)
		}
	}
	if last && closeLast && current != nil {
		return current.Close()
	}
	return nil
}

func (m *clientState) closeAll() error {
	m.mu.Lock()
	if m.closingAll {
		m.mu.Unlock()
		return nil
	}
	m.stopped, m.closingAll = true, true
	m.cancelRetryLocked()
	if m.attempt != nil {
		m.attempt.cancel()
	}
	sockets := append([]*Socket(nil), m.order...)
	current := m.conn
	m.mu.Unlock()
	for _, socket := range sockets {
		_ = m.closeSocket(socket, "io client disconnect", false)
	}
	if current != nil {
		return current.Close()
	}
	return nil
}

func (m *clientState) detach(nc *namespaceConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sockets[fmtNS(nc.namespace)]; s != nil && m.conn == nc.conn && s.nc == nc {
		s.nc, s.connected, s.connectPending = nil, false, false
	}
}

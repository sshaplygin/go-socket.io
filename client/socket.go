package client

import (
	"context"
	"reflect"
	"strings"
	"sync"

	"github.com/sshaplygin/go-socket.io/internal/event"
	"github.com/sshaplygin/go-socket.io/parser"
)

type namespaceConn struct {
	*conn
	namespace      string
	context        interface{}
	ack            sync.Map
	outMu          sync.Mutex
	lifetime       context.Context
	cancelLifetime context.CancelFunc
	typedAcks      map[uint64]*typedRequest
	draining       bool // guarded by conn.mu; this namespace participates in Close's drain
}

func newNamespaceConn(conn *conn, namespace string) *namespaceConn {
	if namespace == rootNamespace {
		namespace = aliasRootNamespace
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &namespaceConn{conn: conn, namespace: namespace, lifetime: ctx, cancelLifetime: cancel}
}

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
	nc.emit(false, eventName, v...)
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
	if volatile && (!nc.Connected() || !nc.writable()) {
		return
	}
	prepared := prepareEvent(nc.namespace, noCompress, eventName, v)
	if m := nc.conn.owner; m != nil && !volatile {
		m.mu.Lock()
		s := m.sockets[fmtNS(nc.namespace)]
		// The cached root may emit before its CONNECT reply. Only that live
		// generation may put an event into the stable Socket's buffer.
		if m.conn == nc.conn && s != nil && !s.connected {
			nc.conn.mu.Lock()
			current, _ := nc.conn.namespaces.Get(fmtNS(nc.namespace))
			buffer := current == nc && !isDone(nc.conn.closing)
			nc.conn.mu.Unlock()
			if buffer {
				prepared.origin = nc
				s.bufferEventLocked(prepared, eventName)
				m.mu.Unlock()
				return
			}
		}
		m.mu.Unlock()
	}
	nc.queuePreparedEvent(volatile, prepared)
}

func prepareEvent(nsp string, noCompress bool, event string, args []interface{}) bufferedEvent {
	header := parser.Header{Type: parser.Event, Namespace: fmtNS(nsp)}
	var ack *reflectedFunc
	if l := len(args); l > 0 {
		if typ := reflect.TypeOf(args[l-1]); typ != nil && typ.Kind() == reflect.Func {
			ack = newAckFunc(args[l-1])
			args = args[:l-1]
		}
	}
	data := make([]interface{}, len(args)+1)
	data[0] = event
	copy(data[1:], args)
	packet, err := parser.Prepare(header, data)
	return bufferedEvent{prepared: packet, encodeErr: err, ack: ack, noCompress: noCompress}
}

func (nc *namespaceConn) queuePreparedEvent(volatile bool, event bufferedEvent) {
	nc.outMu.Lock()
	defer nc.outMu.Unlock()
	c := nc.conn
	c.mu.Lock()
	defer c.mu.Unlock()
	header := parser.Header{Type: parser.Event, Namespace: fmtNS(nc.namespace)}
	if isDone(c.closing) {
		if !nc.draining || !c.draining || c.appSealed || isDone(c.discard) {
			return
		}
	} else if current, _ := c.namespaces.Get(fmtNS(nc.namespace)); current != nc {
		return
	}
	if volatile && (len(c.writeChan) != 0 || len(c.controls) != 0 || cap(c.writeChan) == 0) {
		return
	}
	if len(c.writeChan) >= cap(c.writeChan) {
		c.overflowWrite(header)
		return
	}
	if event.encodeErr == nil {
		nc.registerAck(&header, event.ack)
		event.prepared = event.prepared.WithHeader(header)
	}
	c.queuePreparedLocked(header, event.noCompress, event.prepared, event.encodeErr)
}

func (nc *namespaceConn) registerAck(header *parser.Header, ack *reflectedFunc) {
	if ack != nil {
		header.ID = nc.conn.nextID()
		header.NeedAck = true
		nc.ack.Store(header.ID, ack)
	}
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
		return nc.conn.owner.closeAll(nc)
	}
	return nc.conn.Close()
}

func (nc *namespaceConn) Send(args ...interface{}) { nc.Emit("message", args...) }

// Disconnect sends DISCONNECT for this namespace, leaving the other sockets open.
func (nc *namespaceConn) Disconnect() {
	if m := nc.conn.owner; m != nil {
		m.mu.Lock()
		s := m.sockets[fmtNS(nc.namespace)]
		m.mu.Unlock()
		if s != nil {
			_ = m.closeSocket(s, "io client disconnect", true, nc.conn, nc)
		}
	}
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
	origin     *namespaceConn // nil for stable Socket sends; concrete Conn sends cannot cross generations
	typed      *typedEmission
	prepared   *parser.PreparedPacket
	encodeErr  error
	ack        *reflectedFunc
	noCompress bool
}

// Socket is one namespace on a Client. Its callbacks receive that namespace's Conn.
type Socket struct {
	state          *clientState
	namespace      string
	query          string
	handler        *namespaceHandler
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
func (c *manager) Socket(nsp string) *Socket {
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
	prepared := prepareEvent(s.namespace, noCompress, event, args)
	m := s.state
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.connected && s.nc != nil {
		s.nc.queuePreparedEvent(false, prepared)
		return
	}
	s.bufferEventLocked(prepared, event)
}

// bufferEventLocked requires state.mu.
func (s *Socket) bufferEventLocked(prepared bufferedEvent, event string) {
	m := s.state
	if m.buffered >= m.client.limits.writeBufferSize {
		m.client.log.Warn("socketio: emit buffer full", nspAttr(s.namespace), "event", event)
		return
	}
	s.buffer = append(s.buffer, prepared)
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
	prepared := prepareEvent(s.namespace, noCompress, event, args)
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.connected && s.nc == nc {
		nc.queuePreparedEvent(true, prepared)
	}
}

// Send is Emit("message", args...).
func (s *Socket) Send(args ...interface{}) { s.Emit("message", args...) }

// OnConnect sets the namespace's connection callback.
func (s *Socket) OnConnect(f func(Conn) error) {
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
func (s *Socket) OnDisconnect(f func(Conn, string)) { s.handler.OnDisconnect(f) }

// OnError sets the namespace's error callback.
func (s *Socket) OnError(f func(Conn, error)) { s.handler.OnError(f) }

// OnEvent replaces the namespace's handler for event.
func (s *Socket) OnEvent(event string, f interface{}) { s.handler.OnEvent(event, f) }

// Close sends DISCONNECT for this namespace. The last socket closes Engine.IO.
func (s *Socket) Close() error {
	return s.state.closeSocket(s, "io client disconnect", true, nil, nil)
}

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
	for _, record := range buffer {
		if record.origin != nil && record.origin != nc {
			continue
		}
		if record.typed == nil {
			nc.queuePreparedEvent(false, record)
			continue
		}
		emission := record.typed
		if emission.stop != nil {
			emission.stop()
		}
		err := nc.admitTyped(emission.ctx, record.prepared, emission.request)
		if err != nil && emission.request != nil {
			emission.request.complete(event.Payload{}, err)
		}
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
		// Release its cached Conn as soon as that namespace is disconnected.
		current.claim(namespace)
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	return m.closeSocket(socket, "io server disconnect", true, current, nil)
}

// ownsNamespaceLocked validates a concrete Conn, including the cached root
// before its CONNECT reply. The caller holds m.mu to fence replacement dials.
func (m *clientState) ownsNamespaceLocked(nc *namespaceConn) bool {
	if m.conn != nc.conn {
		return false
	}
	nc.conn.mu.Lock()
	defer nc.conn.mu.Unlock()
	current, _ := nc.conn.namespaces.Get(fmtNS(nc.namespace))
	return !isDone(nc.conn.closing) && current == nc
}

func (m *clientState) closeSocket(s *Socket, reason string, closeLast bool, expected *conn, expectedNamespace *namespaceConn) error {
	m.mu.Lock()
	if expected != nil && m.conn != expected || expectedNamespace != nil && !m.ownsNamespaceLocked(expectedNamespace) {
		m.mu.Unlock()
		return nil
	}
	if !s.active {
		m.mu.Unlock()
		return nil
	}
	s.cancelBufferedTypedLocked()
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

func (m *clientState) closeAll(expected ...*namespaceConn) error {
	m.mu.Lock()
	if len(expected) > 0 {
		if !m.ownsNamespaceLocked(expected[0]) {
			m.mu.Unlock()
			return nil
		}
	}
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
		_ = m.closeSocket(socket, "io client disconnect", false, current, nil)
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

package client

import (
	"strings"

	"github.com/sshaplygin/go-socket.io/parser"
)

type bufferedEvent struct {
	event string
	args  []interface{}
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
func (c *Client) Socket(nsp string) *Socket {
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
	return s.handler.addListener(event, f, false)
}

// Once appends a listener removed before its first call.
func (s *Socket) Once(event string, f interface{}) (off func()) {
	return s.handler.addListener(event, f, true)
}

// Off removes every listener of event, including the OnEvent registration.
func (s *Socket) Off(event string) { s.handler.Off(event) }

// Emit queues an event until the namespace CONNECT reply. A full pre-connect
// buffer drops the event and logs a warning; connected writes use the normal queue.
func (s *Socket) Emit(event string, args ...interface{}) { s.emit(false, event, args...) }

func (s *Socket) emit(volatile bool, event string, args ...interface{}) {
	m := s.state
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.connected && s.nc != nil {
		s.nc.emit(volatile, event, args...)
		return
	}
	if volatile {
		return
	}
	if m.buffered >= m.client.limits.writeBufferSize {
		m.client.log.Warn("socketio: emit buffer full", nspAttr(s.namespace), "event", event)
		return
	}
	s.buffer = append(s.buffer, bufferedEvent{event, append([]interface{}(nil), args...)})
	m.buffered++
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
		nc.emit(false, event.event, event.args...)
	}
	m.mu.Unlock()
	return m.deliverConnect(current, s)
}

func (m *clientState) deliverConnect(current *conn, s *Socket) error {
	m.mu.Lock()
	f := s.handler.connectHandler()
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
		_, claimed = current.claim(s.namespace)
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
		if f := s.handler.disconnectHandler(); f != nil {
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

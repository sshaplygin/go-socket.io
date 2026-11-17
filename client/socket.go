package client

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

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
	localDispatch  atomic.Bool
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
	if localEvent(eventName) {
		nc.emitLocal(eventName, v...)
		return
	}
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
	state     *clientState
	namespace string
	query     string
	handler   *namespaceHandler
	nc        *namespaceConn
	active    bool
	connected bool
	requested bool
	// sent reports that the current connection carries a CONNECT of this
	// Socket; rejoining, that the CONNECT in flight follows an earlier one on
	// that connection.
	sent, rejoining bool
	connectPending  bool
	connectQueued   bool
	buffer          []bufferedEvent
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
		s.requested, s.sent = true, true
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

// Emit queues an event until the namespace CONNECT reply, also while the Socket
// is closed (see Connect). A full pre-connect buffer drops the event and logs a
// warning; connected writes use the normal queue.
// A name the client keeps local (see Namespace.Emit) is neither sent nor
// buffered: Emit calls this namespace's listeners as Conn.Emit does, with the
// Socket's current Conn, and does nothing while the Socket has none, before its
// namespace connects or after it disconnects.
func (s *Socket) Emit(event string, args ...interface{}) {
	m := s.state
	if localEvent(event) {
		m.mu.Lock()
		nc := s.nc
		m.mu.Unlock()
		if nc != nil {
			nc.emitLocal(event, args...)
		}
		return
	}
	prepared := prepareEvent(s.namespace, false, event, args)
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

// Send is Emit("message", args...).
func (s *Socket) Send(args ...interface{}) { s.Emit("message", args...) }

// OnConnect sets the namespace's connection callback. A Close that runs while
// the server's CONNECT reply is being handled can end the join before the
// callback is called; OnDisconnect is still called for that join.
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
				current.report(s.namespace, err)
				current.stop()
			}
		})
	}
}

// OnDisconnect sets the namespace's disconnect callback. Its reason is one of:
//
//   - "io server disconnect": the server disconnected the namespace, for example
//     with socket.disconnect() or a Go server's Conn.Disconnect or Conn.Close.
//     The namespace stays closed until Connect, or until a Client.Connect that
//     dials a new connection.
//   - "io client disconnect": Close, Client.Close, Conn.Close or Conn.Disconnect.
//   - "transport close": the server ended the Engine.IO session in an orderly way.
//   - "transport error": the Engine.IO connection reported a transport failure,
//     for example a broken read, or a received message exceeded
//     WithMaxMessageSize.
//   - "ping timeout": no PONG arrived from the server within its ping interval
//     plus its ping timeout.
//   - "forced close": the client ended the session itself, for example when its
//     write of a Socket.IO packet to the connection failed or after
//     ErrWriteBufferFull, or the session ended without one of the causes above.
//
// After the last four the Client reconnects, unless WithReconnection(false).
// A namespace the server rejected was never connected and gets no callback.
func (s *Socket) OnDisconnect(f func(Conn, string)) { s.handler.OnDisconnect(f) }

// OnError sets the namespace's error callback. The end of the Engine.IO session is
// not an error; see Client.OnError.
func (s *Socket) OnError(f func(Conn, error)) { s.handler.OnError(f) }

// OnEvent replaces the namespace's handler for event. If the event's arguments do
// not decode into the parameter types of f, f is not called and the error goes to
// OnError. What follows depends on the error, not on the JSON value. When it is a
// *json.UnmarshalTypeError, which encoding/json returns for a value that does not
// fit the Go type, such as a string for an int parameter or struct field, a
// requested acknowledgement is answered with an error ACK and the connection stays
// open. Any other decode error, such as the one Time.UnmarshalJSON returns for
// "bad", ends the connection without an ACK; OnDisconnect reports forced close.
// Which error a value produces is decided by encoding/json and can differ under
// GOEXPERIMENT=jsonv2.
//
// f runs on the reader of the client's Engine.IO connection, which serves every
// namespace socket of the Client and also reads the heartbeat's PONG packets, and
// the client renews its heartbeat window only when it reads a PONG. While f runs,
// that Engine.IO connection dispatches nothing else, in any namespace. An f that
// runs longer than the server's ping timeout (20 seconds by default) can therefore
// end the Engine.IO connection, with all its namespaces, with a ping timeout on the
// client or the server, and one that runs longer than the ping interval plus the
// ping timeout (45 seconds by default) normally does; neither the end nor the
// survival of the connection can be relied on. When the connection ends this way,
// OnError is not called, and the ACK of f may still reach the server or be lost.
// For long work, f starts a goroutine and returns, and the goroutine acknowledges
// through a final AckFunc parameter.
func (s *Socket) OnEvent(event string, f interface{}) { s.handler.OnEvent(event, f) }

// Close sends DISCONNECT for this namespace. The last socket closes Engine.IO.
// The Socket stays closed, also across reconnections, until Connect, or until a
// Client.Connect that dials a new connection.
func (s *Socket) Close() error {
	return s.state.closeSocket(s, "io client disconnect", true, nil, nil)
}

// Connect reopens a namespace that the server disconnected (reason
// "io server disconnect") or that Close closed, as socket.connect() does in
// socket.io-client 2.x. On an open Engine.IO connection it sends the namespace
// CONNECT and returns; OnConnect runs when the server accepts it. While the
// Client is reconnecting, or when its connection has ended and its reconnection
// has not started yet, Connect dials nothing and the namespace joins the
// connection the reconnection restores. When the Client has no connection and
// is not reconnecting, as after Client.Close, after the last namespace closed,
// after the reconnection gave up (OnReconnectFailed) or after a connection
// ended with WithReconnection(false), Connect dials one as Client.Connect does
// and returns its error, but it reopens only this Socket: other closed Sockets
// stay closed, and Sockets that are not closed join the new connection as they
// do with Client.Connect. A dial that fails starts no reconnection. A Connect
// that runs while Client.Close or the Close of the last namespace is closing
// the connection waits for that close and then dials. While a dial is in
// progress, Connect makes none: it waits for that dial and returns its result,
// which is io.ErrClosedPipe when Client.Close cancelled the dial.
//
// A connected or joining Socket is left as it is, so concurrent calls send one
// CONNECT. A namespace the server rejected (OnError receives the server's
// message) counts as joining: for a namespace other than the root, Close
// followed by Connect sends a new CONNECT.
//
// The server of this module sends the CONNECT reply of a namespace socket
// before its DISCONNECT, and socket.io 2.5.0 sends no DISCONNECT for a namespace
// socket that has not connected. When the Socket has sent an earlier CONNECT on
// the same connection, Connect therefore takes a DISCONNECT that arrives before
// its reply as the end of the earlier namespace socket, for example one that
// the server disconnected while Close and Connect were on their way: the Socket
// keeps waiting, gets no OnDisconnect for it and joins with the reply.
// socket.io 2.0.0 to 2.4.1 send a DISCONNECT and no reply when a namespace
// middleware calls socket.disconnect(). When that DISCONNECT answers a Connect
// that follows an earlier CONNECT on the connection, the Socket keeps waiting;
// Close followed by Connect sends a new CONNECT.
// Before the reply to the first CONNECT of a Socket on a connection, a
// DISCONNECT closes the Socket, without OnDisconnect.
//
// Events emitted while the Socket is closed are buffered as before its first
// CONNECT and sent once the server accepts it; the buffer is shared with the
// Client's other namespaces (see WithWriteBufferSize).
//
// On an open connection a Close of a joined or joining Socket queues one
// DISCONNECT and a Connect of a closed Socket other than the root queues one
// CONNECT; the connection writes them in call order. WithWriteBufferSize does
// not bound these packets and nothing merges them: Close and Connect calls
// that outrun the connection add two queued packets per round.
//
// The server joins the root namespace when the Engine.IO connection opens, and
// protocol v4 has no client CONNECT for it. On an open connection Connect
// therefore returns ErrRootNamespaceRejoin for a closed root Socket and changes
// nothing. Once that connection has closed, after Client.Close or when its last
// namespace closed, Connect on the root Socket dials a new one.
func (s *Socket) Connect() error {
	return s.state.open(s.state.client, nil, 0, s)
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
		if s != nil {
			current.claim(namespace, nc)
			if namespace != rootNamespace && s.requested {
				// The Socket closed without a DISCONNECT after its last CONNECT:
				// the server disconnected an earlier namespace socket. The server
				// holds the one this reply announces until it is told to drop it,
				// and a server that has it may not answer another CONNECT.
				s.requested = false
				current.writeControl(parser.Header{Type: parser.Disconnect, Namespace: namespace})
			}
		}
		m.mu.Unlock()
		return nil
	}
	// A Close that ran after the reader registered nc has unregistered it and
	// sent DISCONNECT. A Socket reopened since then joins with the reply to its
	// own CONNECT; a Conn the connection no longer routes would drop its events.
	if s.connected || !m.ownsNamespaceLocked(nc) {
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
	if m.supersededLocked(current) {
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
	if namespace != rootNamespace && socket.active && socket.rejoining && socket.requested && !socket.connected && socket.nc == nil {
		// The Socket has sent an earlier CONNECT on this connection. This
		// module's server and socket.io 2.5.0 send no DISCONNECT before a
		// namespace socket's CONNECT reply, so from them this DISCONNECT ends
		// the earlier namespace socket, which the server disconnected while
		// Close and Connect were on their way. The first CONNECT of a Socket on
		// a connection has no earlier namespace socket: a DISCONNECT before its
		// reply closes the Socket.
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	return m.closeSocket(socket, "io server disconnect", true, current, nil)
}

// supersededLocked reports whether a DISCONNECT that c's reader read, or the end
// of c, no longer concerns the Sockets. The reader of a connection that a close
// retired keeps running while the connection drains, and a Socket that is open
// after that close was opened for the next connection. The caller holds m.mu.
func (m *clientState) supersededLocked(c *conn) bool {
	return m.conn != c || m.retired == c
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
	notify, err := m.detachSocket(s, reason, closeLast, expected, expectedNamespace)
	if notify != nil {
		notify()
	}
	return err
}

func (m *clientState) detachSocket(s *Socket, reason string, closeLast bool, expected *conn, expectedNamespace *namespaceConn) (func(), error) {
	m.mu.Lock()
	if expected != nil && m.supersededLocked(expected) || expectedNamespace != nil && !m.ownsNamespaceLocked(expectedNamespace) {
		m.mu.Unlock()
		return nil, nil
	}
	if !s.active {
		m.mu.Unlock()
		return nil, nil
	}
	current := m.conn
	notify := m.detachLocked(s, reason)
	last := true
	for _, other := range m.order {
		last = last && !other.active
	}
	if last && closeLast {
		m.stopped, m.retired = true, current
		m.cancelRetryLocked()
		if m.attempt != nil {
			m.attempt.cancel()
		}
	}
	m.mu.Unlock()
	var closeErr error
	// Mark the old engine closing before application callbacks can Connect again.
	// Its namespace has already been claimed, so Close cannot repeat this callback.
	if last && closeLast && current != nil {
		if m.testHookClosing != nil {
			m.testHookClosing()
		}
		closeErr = current.Close()
	}
	return notify, closeErr
}

// detachLocked closes the active Socket s on the current connection and returns
// the OnDisconnect call it is owed, if any. The caller holds m.mu.
func (m *clientState) detachLocked(s *Socket, reason string) func() {
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
			// While s is closed, requested means that its last CONNECT is not
			// followed by a DISCONNECT; see namespaceConnected.
			s.requested = false
			current.writeControl(parser.Header{Type: parser.Disconnect, Namespace: s.namespace})
		}
	}
	if connected && claimed {
		if f := s.handler.DisconnectHandler(); f != nil {
			return func() { f(nc, reason) }
		}
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
	m.stopped, m.closingAll, m.retired = true, true, m.conn
	m.cancelRetryLocked()
	if m.attempt != nil {
		m.attempt.cancel()
	}
	// Detach every Socket under the lock that stops the client, so that a
	// Connect that follows cannot see a Socket this close detaches later.
	current := m.conn
	var callbacks []func()
	for _, socket := range m.order {
		if !socket.active {
			continue
		}
		if notify := m.detachLocked(socket, "io client disconnect"); notify != nil {
			callbacks = append(callbacks, notify)
		}
	}
	m.mu.Unlock()
	var err error
	if current != nil {
		if m.testHookClosing != nil {
			m.testHookClosing()
		}
		err = current.Close()
	}
	for _, notify := range callbacks {
		notify()
	}
	return err
}

func (m *clientState) detach(nc *namespaceConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sockets[fmtNS(nc.namespace)]; s != nil && m.conn == nc.conn && s.nc == nc {
		s.nc, s.connected, s.connectPending = nil, false, false
	}
}

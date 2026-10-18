package client

import (
	"reflect"
	"sync"

	"github.com/sshaplygin/go-socket.io/parser"
)

type namespaceConn struct {
	*conn
	broadcast *roomIndex
	namespace string
	context   interface{}
	ack       sync.Map
	outMu     sync.Mutex
}

func newNamespaceConn(conn *conn, namespace string, broadcast *roomIndex) *namespaceConn {
	if namespace == rootNamespace {
		namespace = aliasRootNamespace
	}
	return &namespaceConn{conn: conn, namespace: namespace, broadcast: broadcast}
}

// Handshake returns zero metadata on a client connection.
func (nc *namespaceConn) Handshake() Handshake { return Handshake{} }

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

	var ack *funcHandler
	if l := len(v); l > 0 {
		last := v[l-1]
		lastV := reflect.TypeOf(last)

		if lastV != nil && lastV.Kind() == reflect.Func {
			ack = newAckFunc(last)
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

func (nc *namespaceConn) registerAck(header *parser.Header, ack *funcHandler) {
	if ack != nil {
		header.ID = nc.conn.nextID()
		header.NeedAck = true
		nc.ack.Store(header.ID, ack)
	}
}

func (nc *namespaceConn) Join(rooms ...string) {
	for _, room := range rooms {
		nc.broadcast.join(room, nc)
	}
}

func (nc *namespaceConn) Leave(room string) {
	nc.broadcast.leaveRoom(room, nc)
}

func (nc *namespaceConn) LeaveAll() {
	nc.broadcast.leaveAll(nc)
}

func (nc *namespaceConn) Rooms() []string {
	return nc.broadcast.socketRooms(nc)
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

type emitter struct {
	socket              *namespaceConn
	broadcast, volatile bool
	noCompress          bool
}

func (e emitter) To(rooms ...string) Emitter { e.broadcast = true; return e }

func (e emitter) Except(ids ...string) Emitter { e.broadcast = true; return e }

func (e emitter) Volatile() Emitter { e.volatile = true; return e }

func (e emitter) Local() Emitter { return e }

func (e emitter) Send(args ...interface{}) error { return e.Emit("message", args...) }

func (e emitter) Emit(event string, args ...interface{}) error {
	if e.broadcast {
		return ErrServerOnly
	}
	if socket := e.socket.socket(); socket != nil {
		socket.emitFlags(e.volatile, e.noCompress, event, args...)
	} else {
		e.socket.emitFlags(e.volatile, e.noCompress, event, args...)
	}
	return nil
}

func (nc *namespaceConn) To(rooms ...string) Emitter { return (emitter{socket: nc}).To(rooms...) }

func (nc *namespaceConn) Broadcast() Emitter { return (emitter{socket: nc, broadcast: true}) }

func (nc *namespaceConn) Volatile() Emitter { return (emitter{socket: nc}).Volatile() }

func (e emitter) Compress(on bool) Emitter { e.noCompress = !on; return e }

func (nc *namespaceConn) Compress(on bool) Emitter { return (emitter{socket: nc}).Compress(on) }

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

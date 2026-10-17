package client

import (
	"github.com/sshaplygin/go-socket.io/parser"
	"reflect"
	"sync"
)

// Namespace describes a communication channel that allows you to split the logic of your application
// over a single shared connection.
type Namespace interface {
	// Context of this connection. You can save one context for one
	// connection, and share it between all handlers. The handlers
	// are called in one goroutine, so no need to lock context if it
	// only accessed in one connection.
	Context() interface{}
	SetContext(ctx interface{})

	Namespace() string
	// Emit sends eventName with arguments v to the client. If the last element of v
	// is a func, it is not sent but registered as the acknowledgement callback: the
	// client's ACK arguments are decoded into its parameters and it is called on the
	// read goroutine of the connection. Emit never blocks: it queues the packet, and Emits
	// from OnConnect are written once the writer starts. A queue already holding
	// engineio.Options.WriteBufferSize packets not yet started drops the packet and every
	// later one and closes the connection (see ErrWriteBufferFull). Once a close started,
	// Emit drops the packet without a report, except that Close still queues packets until
	// the OnDisconnect calls it runs have returned (see Conn.Close).
	Emit(eventName string, v ...interface{})

	// Join joins each room in order, ignoring rooms already joined.
	Join(rooms ...string)
	// To broadcasts to a union of rooms except this socket. Client emits return ErrServerOnly.
	To(rooms ...string) Emitter
	// Broadcast targets the namespace except this socket. Client emits return ErrServerOnly.
	Broadcast() Emitter
	// Volatile starts a single-socket emit that drops when its connection is not writable.
	Volatile() Emitter
	Leave(room string)
	LeaveAll()
	Rooms() []string
	// Send is Emit("message", args...), including a trailing acknowledgement callback.
	Send(args ...interface{})
	// Disconnect writes DISCONNECT for this namespace, leaves its rooms and calls
	// OnDisconnect with the server namespace reason. Other namespaces stay open.
	// On an experimental client connection, Disconnect is a no-op.
	Disconnect()
	// Use appends event middleware on a server socket; on a client it is a no-op.
	Use(func(event string, args []interface{}) error)
}

type namespaceConn struct {
	*conn
	broadcast *roomIndex
	namespace string
	context   interface{}
	ack       sync.Map
	outMu     sync.Mutex
}

func newNamespaceConn(conn *conn, namespace string, broadcast *roomIndex) *namespaceConn {
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

func (nc *namespaceConn) Emit(eventName string, v ...interface{}) { nc.emit(false, eventName, v...) }

func (nc *namespaceConn) emit(volatile bool, eventName string, v ...interface{}) {
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
		nc.send(header, args...)
		return
	}
	// The transport callback runs outside queue locks; admission rechecks the
	// queue atomically afterwards so a concurrent normal/volatile emit cannot
	// turn this best-effort packet into an overflow or retain a dropped ACK.
	if c, ok := nc.Conn.(interface{ Writable() bool }); ok && !c.Writable() {
		return
	}
	nc.outMu.Lock()
	defer nc.outMu.Unlock()
	nc.conn.mu.Lock()
	defer nc.conn.mu.Unlock()
	c := nc.conn
	if len(c.writeChan) != 0 ||
		isDone(c.closing) && (!c.draining || c.appSealed || isDone(c.discard)) {
		return
	}
	nc.registerAck(&header, ack)
	c.queueLocked(header, args...)
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

func (nc *namespaceConn) ID() string { return nc.Conn.ID() }

func (nc *namespaceConn) Send(args ...interface{}) { nc.Emit("message", args...) }

// Emitter is an immutable pending target and its flags. Each builder returns a new
// value, so an Emitter can be reused. To or Except converts a single-socket target
// to a broadcast excluding its sender; Volatile and Local preserve its target.
type Emitter interface {
	To(rooms ...string) Emitter
	Except(ids ...string) Emitter
	Volatile() Emitter
	Local() Emitter
	// Emit returns ErrBroadcastAck without sending for a broadcast with a trailing
	// func, or an adapter error. A dynamic parent may have delivered to earlier
	// children before a child returns an error. A single-socket emit never fails;
	// a client broadcast returns ErrServerOnly without sending.
	Emit(event string, args ...interface{}) error
	// Send is Emit("message", args...).
	Send(args ...interface{}) error
}

// Disconnect does nothing on a client connection.
func (nc *namespaceConn) Disconnect() {}

// Use does not register middleware on a client connection.
func (nc *namespaceConn) Use(func(string, []interface{}) error) {}
func (nc *namespaceConn) send(header parser.Header, args ...reflect.Value) {
	nc.conn.write(header, args...)
}

type emitter struct {
	socket              *namespaceConn
	broadcast, volatile bool
}

func (e emitter) To(rooms ...string) Emitter     { e.broadcast = true; return e }
func (e emitter) Except(ids ...string) Emitter   { e.broadcast = true; return e }
func (e emitter) Volatile() Emitter              { e.volatile = true; return e }
func (e emitter) Local() Emitter                 { return e }
func (e emitter) Send(args ...interface{}) error { return e.Emit("message", args...) }
func (e emitter) Emit(event string, args ...interface{}) error {
	if e.broadcast {
		return ErrServerOnly
	}
	e.socket.emit(e.volatile, event, args...)
	return nil
}
func (nc *namespaceConn) To(rooms ...string) Emitter { return (emitter{socket: nc}).To(rooms...) }
func (nc *namespaceConn) Broadcast() Emitter         { return (emitter{socket: nc, broadcast: true}) }
func (nc *namespaceConn) Volatile() Emitter          { return (emitter{socket: nc}).Volatile() }

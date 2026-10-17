package socketio

import (
	"reflect"
	"sync"

	"github.com/sshaplygin/go-socket.io/parser"
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

	Join(room string)
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
	broadcast Broadcast

	namespace string
	context   interface{}

	ack        sync.Map
	outMu      sync.Mutex
	connecting bool
	rejected   bool
	pending    []pendingPacket
	middleware []func(string, []interface{}) error
}

func newNamespaceConn(conn *conn, namespace string, broadcast Broadcast) *namespaceConn {
	return &namespaceConn{
		conn:      conn,
		namespace: namespace,
		broadcast: broadcast,
	}
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

func (nc *namespaceConn) Emit(eventName string, v ...interface{}) {
	header := parser.Header{
		Type: parser.Event,
	}

	if nc.namespace != aliasRootNamespace {
		header.Namespace = nc.namespace
	}

	if l := len(v); l > 0 {
		last := v[l-1]
		lastV := reflect.TypeOf(last)

		if lastV != nil && lastV.Kind() == reflect.Func {
			f := newAckFunc(last)

			header.ID = nc.conn.nextID()
			header.NeedAck = true

			nc.ack.Store(header.ID, f)
			v = v[:l-1]
		}
	}

	args := make([]reflect.Value, len(v)+1)
	args[0] = reflect.ValueOf(eventName)

	for i := 1; i < len(args); i++ {
		args[i] = reflect.ValueOf(v[i-1])
	}

	nc.send(header, args...)
}

func (nc *namespaceConn) Join(room string) {
	nc.broadcast.Join(room, nc)
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

// ID returns the engine ID in the root and <namespace>#<engine ID> elsewhere.
func (nc *namespaceConn) ID() string {
	id := nc.Conn.ID()
	if nc.conn.boundary && nc.namespace != "" && nc.namespace != "/" {
		return nc.namespace + "#" + id
	}
	return id
}

func (nc *namespaceConn) Send(args ...interface{}) { nc.Emit("message", args...) }

func (nc *namespaceConn) Disconnect() {
	if !nc.conn.boundary {
		return
	}
	if claimed, ok := nc.conn.claim(fmtNS(nc.namespace), nc); ok && claimed == nc {
		nc.acceptPending()
		nc.conn.write(parser.Header{Type: parser.Disconnect, Namespace: fmtNS(nc.namespace)})
		nc.conn.disconnectNamespace(nc, "server namespace disconnect")
	}
}

func (nc *namespaceConn) Use(f func(string, []interface{}) error) {
	if !nc.conn.boundary {
		return
	}
	nc.outMu.Lock()
	nc.middleware = append(nc.middleware, f)
	nc.outMu.Unlock()
}

func (nc *namespaceConn) runMiddleware(event string, args []reflect.Value) error {
	nc.outMu.Lock()
	chain := append([]func(string, []interface{}) error(nil), nc.middleware...)
	nc.outMu.Unlock()
	if len(chain) == 0 {
		return nil
	}
	values := make([]interface{}, len(args))
	for i, arg := range args {
		values[i] = arg.Interface()
	}
	for _, f := range chain {
		if err := f(event, values); err != nil {
			return err
		}
	}
	return nil
}

type pendingPacket struct {
	header parser.Header
	args   []reflect.Value
}

func (nc *namespaceConn) beginConnect() {
	nc.outMu.Lock()
	nc.connecting = true
	nc.outMu.Unlock()
}

func (nc *namespaceConn) send(header parser.Header, args ...reflect.Value) {
	nc.outMu.Lock()
	if nc.rejected {
		nc.outMu.Unlock()
		return
	}
	if nc.connecting {
		nc.conn.mu.Lock()
		if (!isDone(nc.conn.closing) || nc.conn.draining && !isDone(nc.conn.seal) && !isDone(nc.conn.discard)) && nc.conn.pendingWrites+len(nc.conn.writeChan)+nc.conn.drainQueued < cap(nc.conn.writeChan) {
			nc.conn.pendingWrites++
			nc.pending = append(nc.pending, pendingPacket{header, args})
		} else {
			nc.conn.overflowWrite(header)
		}
		nc.conn.mu.Unlock()
		nc.outMu.Unlock()
		return
	}
	nc.outMu.Unlock()
	nc.conn.write(header, args...)
}

func (nc *namespaceConn) acceptPending() {
	nc.outMu.Lock()
	defer nc.outMu.Unlock()
	if !nc.connecting || nc.rejected {
		return
	}
	pending := nc.pending
	nc.pending = nil
	nc.connecting = false
	nc.Join(nc.ID())
	if nh := nc.conn.namespace(fmtNS(nc.namespace)); nh != nil {
		nh.addSocket(nc)
	}
	nc.conn.write(parser.Header{Type: parser.Connect, Namespace: fmtNS(nc.namespace)})
	for _, p := range pending {
		nc.conn.writeReserved(p.header, p.args...)
	}
}

func (nc *namespaceConn) reject() {
	nc.outMu.Lock()
	nc.rejected = true
	nc.connecting = false
	nc.conn.mu.Lock()
	nc.conn.pendingWrites -= len(nc.pending)
	nc.conn.mu.Unlock()
	nc.pending = nil
	nc.outMu.Unlock()
	nc.LeaveAll()
}

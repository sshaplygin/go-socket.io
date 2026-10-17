package socketio

import (
	"net/url"
	"reflect"
	"sync"
	"time"

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
	broadcast Broadcast

	namespace string
	handshake Handshake
	context   interface{}

	ack        sync.Map
	outMu      sync.Mutex
	connecting bool
	rejected   bool
	pending    []pendingPacket
	middleware []func(string, []interface{}) error
}

func newNamespaceConn(conn *conn, namespace string, broadcast Broadcast, query ...string) *namespaceConn {
	nc := &namespaceConn{conn: conn, namespace: namespace, broadcast: broadcast}
	if r := conn.Request(); r != nil {
		h := Handshake{Headers: r.Header.Clone(), Time: time.Now(), Address: r.RemoteAddr, XDomain: r.Header.Get("Origin") != "", Secure: r.TLS != nil, Query: make(url.Values)}
		if r.URL != nil {
			h.URL = *r.URL
			h.Query = r.URL.Query()
		}
		if len(query) != 0 {
			extra, _ := url.ParseQuery(query[0])
			for key, values := range extra {
				h.Query[key] = append([]string(nil), values...)
			}
		}
		nc.handshake = h
	}
	return nc
}

func (nc *namespaceConn) Handshake() Handshake {
	h := nc.handshake
	h.Headers = h.Headers.Clone()
	if h.Query != nil {
		h.Query = cloneQuery(h.Query)
	}
	return h
}

func cloneQuery(query url.Values) url.Values {
	result := make(url.Values, len(query))
	for key, values := range query {
		result[key] = append([]string(nil), values...)
	}
	return result
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
	if nc.rejected || c.pendingWrites+len(c.writeChan)+len(c.drainQueue) != 0 ||
		isDone(c.closing) && (!c.draining || c.appSealed || isDone(c.discard)) {
		return
	}
	nc.registerAck(&header, ack)
	if nc.connecting {
		c.pendingWrites++
		nc.pending = append(nc.pending, pendingPacket{header, args})
	} else {
		c.queueLocked(header, false, args...)
	}
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
	if claimed, ok := nc.conn.claimControlDisconnect(fmtNS(nc.namespace), nc); ok && claimed == nc {
		nc.acceptPending()
		nc.conn.write(parser.Header{Type: parser.Disconnect, Namespace: fmtNS(nc.namespace)})
		nc.conn.controlEnqueued()
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
		if (!isDone(nc.conn.closing) || nc.conn.draining && !nc.conn.appSealed && !isDone(nc.conn.discard)) && nc.conn.pendingWrites+len(nc.conn.writeChan)+nc.conn.drainQueued < cap(nc.conn.writeChan) {
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

type emitter struct {
	socket    *namespaceConn
	adapter   Broadcast
	broadcast bool
	opts      BroadcastOptions
}

func (e emitter) asBroadcast() emitter {
	if !e.broadcast {
		e.broadcast = true
		e.adapter = e.socket.broadcast
		if !e.socket.conn.boundary {
			e.adapter = nil
		}
		e.opts.Except = append(append([]string(nil), e.opts.Except...), e.socket.ID())
	}
	return e
}

func (e emitter) To(rooms ...string) Emitter {
	e = e.asBroadcast()
	e.opts.Rooms = append(append([]string(nil), e.opts.Rooms...), rooms...)
	return e
}

func (e emitter) Except(ids ...string) Emitter {
	e = e.asBroadcast()
	e.opts.Except = append(append([]string(nil), e.opts.Except...), ids...)
	return e
}

func (e emitter) Volatile() Emitter              { e.opts.Volatile = true; return e }
func (e emitter) Local() Emitter                 { e.opts.Local = true; return e }
func (e emitter) Send(args ...interface{}) error { return e.Emit("message", args...) }

func (e emitter) Emit(event string, args ...interface{}) error {
	if e.broadcast {
		if e.adapter == nil {
			return ErrServerOnly
		}
		if hasBroadcastAck(args) {
			return ErrBroadcastAck
		}
		return e.adapter.SendTo(e.opts, event, args...)
	}
	e.socket.emit(e.opts.Volatile, event, args...)
	return nil
}

func hasBroadcastAck(args []interface{}) bool {
	if len(args) == 0 {
		return false
	}
	t := reflect.TypeOf(args[len(args)-1])
	return t != nil && t.Kind() == reflect.Func
}

func (nc *namespaceConn) To(rooms ...string) Emitter { return (emitter{socket: nc}).To(rooms...) }
func (nc *namespaceConn) Broadcast() Emitter         { return (emitter{socket: nc}).asBroadcast() }
func (nc *namespaceConn) Volatile() Emitter          { return (emitter{socket: nc}).Volatile() }

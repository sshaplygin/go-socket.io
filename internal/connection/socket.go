package connection

import (
	"net/url"
	"reflect"
	"sync"
	"time"

	"github.com/sshaplygin/go-socket.io/adapter"
	namespaceImpl "github.com/sshaplygin/go-socket.io/internal/namespace"
	"github.com/sshaplygin/go-socket.io/parser"
)

type namespaceConn struct {
	*Connection
	broadcast adapter.Adapter

	namespace string
	handshake namespaceImpl.Handshake
	context   interface{}

	ack        sync.Map
	outMu      sync.Mutex
	connecting bool
	rejected   bool
	pending    []pendingPacket
	middleware []func(string, []interface{}) error
}

func newNamespaceConn(conn *Connection, namespace string, broadcast adapter.Adapter, query ...string) *namespaceConn {
	nc := &namespaceConn{Connection: conn, namespace: namespace, broadcast: broadcast}
	if r := conn.Request(); r != nil {
		h := namespaceImpl.Handshake{Headers: r.Header.Clone(), Time: time.Now(), Address: r.RemoteAddr, XDomain: r.Header.Get("Origin") != "", Secure: r.TLS != nil, Query: make(url.Values)}
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

func (nc *namespaceConn) Handshake() namespaceImpl.Handshake {
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

// Connected reports whether this namespace still belongs to this connection.
func (nc *namespaceConn) Connected() bool {
	current, ok := nc.Connection.namespaces.Get(fmtNS(nc.namespace))
	return ok && current == nc && !isDone(nc.Connection.closing)
}

func (nc *namespaceConn) Emit(eventName string, v ...interface{}) { nc.emit(false, eventName, v...) }

func (nc *namespaceConn) emit(volatile bool, eventName string, v ...interface{}) {
	nc.emitFlags(volatile, false, eventName, v...)
}

func (nc *namespaceConn) emitFlags(volatile, noCompress bool, eventName string, v ...interface{}) {
	header := parser.Header{
		Type: parser.Event,
	}

	if nc.namespace != aliasRootNamespace {
		header.Namespace = nc.namespace
	}

	var ack *namespaceImpl.Func
	if l := len(v); l > 0 {
		last := v[l-1]
		lastV := reflect.TypeOf(last)

		if lastV != nil && lastV.Kind() == reflect.Func {
			ack = namespaceImpl.NewAckFunc(last)
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
		nc.sendFlags(header, noCompress, args...)
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
	nc.Connection.mu.Lock()
	defer nc.Connection.mu.Unlock()
	c := nc.Connection
	if nc.rejected || c.pendingWrites+len(c.writeChan)+len(c.drainQueue) != 0 ||
		isDone(c.closing) && (!c.draining || c.appSealed || isDone(c.discard)) {
		return
	}
	nc.registerAck(&header, ack)
	if nc.connecting {
		c.pendingWrites++
		nc.pending = append(nc.pending, pendingPacket{header: header, args: args, noCompress: noCompress})
	} else {
		c.queueFlagsLocked(header, false, noCompress, args...)
	}
}

func (nc *namespaceConn) registerAck(header *parser.Header, ack *namespaceImpl.Func) {
	if ack != nil {
		header.ID = nc.Connection.nextID()
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
	if nc.Connection.boundary && nc.namespace != "" && nc.namespace != "/" {
		return nc.namespace + "#" + id
	}
	return id
}

func (nc *namespaceConn) Send(args ...interface{}) { nc.Emit("message", args...) }

func (nc *namespaceConn) Disconnect() {
	if !nc.Connection.boundary {
		return
	}
	if claimed, ok := nc.Connection.claimControlDisconnect(fmtNS(nc.namespace), nc); ok && claimed == nc {
		nc.acceptPending()
		nc.Connection.write(parser.Header{Type: parser.Disconnect, Namespace: fmtNS(nc.namespace)})
		nc.Connection.controlEnqueued()
		nc.Connection.disconnectNamespace(nc, "server namespace disconnect")
	}
}

func (nc *namespaceConn) Use(f func(string, []interface{}) error) {
	if !nc.Connection.boundary {
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
	header     parser.Header
	args       []reflect.Value
	noCompress bool
}

func (nc *namespaceConn) beginConnect() {
	nc.outMu.Lock()
	nc.connecting = true
	nc.outMu.Unlock()
}

func (nc *namespaceConn) sendFlags(header parser.Header, noCompress bool, args ...reflect.Value) {
	nc.outMu.Lock()
	if nc.rejected {
		nc.outMu.Unlock()
		return
	}
	if nc.connecting {
		nc.Connection.mu.Lock()
		if (!isDone(nc.Connection.closing) || nc.Connection.draining && !nc.Connection.appSealed && !isDone(nc.Connection.discard)) && nc.Connection.pendingWrites+len(nc.Connection.writeChan)+nc.Connection.drainQueued < cap(nc.Connection.writeChan) {
			nc.Connection.pendingWrites++
			nc.pending = append(nc.pending, pendingPacket{header: header, args: args, noCompress: noCompress})
		} else {
			nc.Connection.overflowWrite(header)
		}
		nc.Connection.mu.Unlock()
		nc.outMu.Unlock()
		return
	}
	nc.outMu.Unlock()
	nc.Connection.writeFlags(header, false, noCompress, args...)
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
	if nh := nc.Connection.namespace(fmtNS(nc.namespace)); nh != nil {
		nh.AddSocket(nc)
	}
	nc.Connection.write(parser.Header{Type: parser.Connect, Namespace: fmtNS(nc.namespace)})
	for _, p := range pending {
		nc.Connection.writeFlags(p.header, true, p.noCompress, p.args...)
	}
}

func (nc *namespaceConn) reject() {
	nc.outMu.Lock()
	nc.rejected = true
	nc.connecting = false
	nc.Connection.mu.Lock()
	nc.Connection.pendingWrites -= len(nc.pending)
	nc.Connection.mu.Unlock()
	nc.pending = nil
	nc.outMu.Unlock()
	nc.LeaveAll()
}

func (nc *namespaceConn) To(rooms ...string) namespaceImpl.Emitter {
	return (emitter{socket: nc}).To(rooms...)
}

func (nc *namespaceConn) Broadcast() namespaceImpl.Emitter {
	return (emitter{socket: nc}).asBroadcast()
}

func (nc *namespaceConn) Volatile() namespaceImpl.Emitter { return (emitter{socket: nc}).Volatile() }

func (nc *namespaceConn) Compress(on bool) namespaceImpl.Emitter {
	return (emitter{socket: nc}).Compress(on)
}

// namespace
const (
	aliasRootNamespace = "/"
	rootNamespace      = ""
)

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

// message
const (
	clientDisconnectMsg = "client namespace disconnect"
)

// EmitWithOptions applies per-socket delivery flags; selection belongs to adapters.
func (nc *namespaceConn) EmitWithOptions(opts adapter.Options, event string, args ...interface{}) {
	nc.emitFlags(opts.Volatile, opts.NoCompress, event, args...)
}

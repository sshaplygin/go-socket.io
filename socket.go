package socketio

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"sync"
	"time"

	"github.com/sshaplygin/go-socket.io/adapter"

	"github.com/sshaplygin/go-socket.io/parser"
)

type namespaceConn struct {
	*connection
	broadcast adapter.Adapter

	namespace   string
	handshake   Handshake
	context     interface{}
	roomsMu     sync.Mutex
	roomsClosed bool
	roomOps     int
	roomsDone   chan struct{}

	lifetime        context.Context
	cancelLifetime  context.CancelFunc
	ackTimeout      time.Duration
	ack             sync.Map
	outMu           sync.Mutex
	connecting      bool
	acceptDone      chan struct{}
	connectDone     chan struct{}
	connectErr      error
	disconnectEmits bool
	localDispatch   bool
	rejected        bool
	pending         []outboundPayload
	middleware      []func(string, []interface{}) error
}

func newNamespaceConn(conn *connection, namespace string, broadcast adapter.Adapter, query ...string) *namespaceConn {
	nc := &namespaceConn{connection: conn, namespace: namespace, broadcast: broadcast, ackTimeout: 30 * time.Second}
	nc.lifetime, nc.cancelLifetime = context.WithCancel(context.Background())
	if isDone(conn.engineDone) {
		nc.cancelLifetime()
	}
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
	current, ok := nc.connection.namespaces.Get(fmtNS(nc.namespace))
	return ok && current == nc && !isDone(nc.connection.closing)
}

func (nc *namespaceConn) Emit(eventName string, v ...interface{}) { nc.emit(false, eventName, v...) }

func (nc *namespaceConn) emit(volatile bool, eventName string, v ...interface{}) {
	nc.emitFlags(volatile, false, eventName, v...)
}

func (nc *namespaceConn) emitFlags(volatile, noCompress bool, eventName string, v ...interface{}) {
	if reservedEvent(eventName) {
		nc.emitLocal(eventName, v...)
		return
	}
	header := parser.Header{
		Type: parser.Event,
	}

	if nc.namespace != aliasRootNamespace {
		header.Namespace = nc.namespace
	}

	var ack *callbackFunc
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

	pkg := preparePayload(header, noCompress, args...)
	if !volatile {
		nc.sendPayload(pkg, ack)
		return
	}
	nc.sendVolatile(pkg, ack)
}

func (nc *namespaceConn) sendVolatile(pkg outboundPayload, ack *callbackFunc) {
	header := pkg.Header
	// The transport callback runs outside queue locks; admission rechecks the
	// queue atomically afterwards so a concurrent normal/volatile emit cannot
	// turn this best-effort packet into an overflow or retain a dropped ACK.
	if c, ok := nc.Conn.(interface{ Writable() bool }); ok && !c.Writable() {
		return
	}
	nc.outMu.Lock()
	defer nc.outMu.Unlock()
	nc.connection.mu.Lock()
	defer nc.connection.mu.Unlock()
	c := nc.connection
	if nc.rejected || nc.lifetime.Err() != nil && !nc.disconnectEmits && !nc.connecting || c.pendingWrites+len(c.drainQueue) != 0 ||
		isDone(c.closing) && (!c.draining || c.appSealed || isDone(c.discard)) {
		return
	}
	nc.registerAck(&header, ack)
	pkg.Header = header
	if ack != nil && pkg.prepared != nil {
		pkg.prepared = pkg.prepared.WithHeader(header)
	}
	if nc.connecting {
		c.pendingWrites++
		nc.pending = append(nc.pending, pkg)
	} else {
		c.queuePayloadLocked(pkg, false)
	}
}

func (nc *namespaceConn) registerAck(header *parser.Header, ack *callbackFunc) {
	if ack != nil {
		header.ID = nc.connection.nextID()
		header.NeedAck = true
		nc.ack.Store(header.ID, ack)
	}
}

func (nc *namespaceConn) Join(rooms ...string) {
	if !nc.beginRoomOperation() {
		return
	}
	defer nc.endRoomOperation()
	for _, room := range rooms {
		nc.broadcast.Join(room, nc)
	}
}

func (nc *namespaceConn) Leave(room string) {
	if !nc.beginRoomOperation() {
		return
	}
	defer nc.endRoomOperation()
	nc.broadcast.Leave(room, nc)
}

func (nc *namespaceConn) LeaveAll() {
	if !nc.beginRoomOperation() {
		return
	}
	defer nc.endRoomOperation()
	nc.broadcast.LeaveAll(nc)
}

func (nc *namespaceConn) Rooms() []string {
	if !nc.beginRoomOperation() {
		return nil
	}
	defer nc.endRoomOperation()
	return nc.broadcast.Rooms(nc)
}

// Room operations release roomsMu before invoking an adapter.
func (nc *namespaceConn) beginRoomOperation() bool {
	nc.roomsMu.Lock()
	defer nc.roomsMu.Unlock()
	if nc.roomsClosed {
		return false
	}
	nc.roomOps++
	return true
}

func (nc *namespaceConn) endRoomOperation() {
	nc.roomsMu.Lock()
	defer nc.roomsMu.Unlock()
	nc.roomOps--
	if nc.roomOps == 0 && nc.roomsDone != nil {
		close(nc.roomsDone)
		nc.roomsDone = nil
	}
}

func (nc *namespaceConn) roomOperationsDone() <-chan struct{} {
	nc.roomsMu.Lock()
	defer nc.roomsMu.Unlock()
	return nc.roomOperationsDoneLocked()
}

func (nc *namespaceConn) roomOperationsDoneLocked() <-chan struct{} {
	if nc.roomOps > 0 && nc.roomsDone == nil {
		nc.roomsDone = make(chan struct{})
	}
	return nc.roomsDone
}

// retireRooms seals room operations before a replacement can reuse this ID.
// Disconnecting callbacks still see the old rooms until this cleanup starts.
func (nc *namespaceConn) retireRooms() {
	nc.roomsMu.Lock()
	if nc.roomsClosed {
		nc.roomsMu.Unlock()
		return
	}
	nc.roomsClosed = true
	done := nc.roomOperationsDoneLocked()
	nc.roomsMu.Unlock()
	if done != nil {
		<-done
	}
	nc.broadcast.LeaveAll(nc)
}

// ID returns the engine ID in the root and <namespace>#<engine ID> elsewhere.
func (nc *namespaceConn) ID() string {
	id := nc.Conn.ID()
	if nc.namespace != "" && nc.namespace != "/" {
		return nc.namespace + "#" + id
	}
	return id
}

func (nc *namespaceConn) Send(args ...interface{}) { nc.Emit("message", args...) }

func (nc *namespaceConn) Disconnect() {
	if claimed, ok := nc.connection.claimControlDisconnect(fmtNS(nc.namespace), nc); ok && claimed == nc {
		nc.outMu.Lock()
		done := nc.connectDone
		nc.outMu.Unlock()
		roomsDone := nc.roomOperationsDone()
		disconnect := func() {
			nc.outMu.Lock()
			rejected := nc.connectErr != nil
			nc.outMu.Unlock()
			if !rejected {
				nc.acceptPending()
				nc.connection.write(parser.Header{Type: parser.Disconnect, Namespace: fmtNS(nc.namespace)})
			}
			nc.connection.controlEnqueued()
			if rejected {
				nc.reject()
				nc.connection.mu.Lock()
				nc.connection.disconnecting--
				nc.connection.mu.Unlock()
				nc.connection.disconnects.Done()
				nc.connection.finishRetirement(nc)
				return
			}
			nc.connection.disconnectNamespace(nc, "server namespace disconnect")
		}
		if done != nil && !isDone(done) || roomsDone != nil && !isDone(roomsDone) {
			go func() {
				if done != nil {
					<-done
				}
				if roomsDone != nil {
					<-roomsDone
				}
				disconnect()
			}()
		} else {
			disconnect()
		}
	}
}

func (nc *namespaceConn) Use(f func(string, []interface{}) error) {
	if f == nil {
		return
	}
	nc.outMu.Lock()
	nc.middleware = append(nc.middleware, f)
	nc.outMu.Unlock()
}

func (nc *namespaceConn) hasMiddleware() bool {
	nc.outMu.Lock()
	defer nc.outMu.Unlock()
	return len(nc.middleware) != 0
}

func (nc *namespaceConn) runMiddleware(event string, args []reflect.Value) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("event middleware panic: %v", value)
		}
	}()
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

func (nc *namespaceConn) beginConnect() {
	nc.outMu.Lock()
	nc.connecting = true
	nc.connectDone = make(chan struct{})
	nc.outMu.Unlock()
}

func (nc *namespaceConn) sendPayload(pkg outboundPayload, ack *callbackFunc) {
	header := pkg.Header
	nc.outMu.Lock()
	defer nc.outMu.Unlock()
	nc.connection.mu.Lock()
	defer nc.connection.mu.Unlock()
	pkg.lifecycle = nc.disconnectEmits
	if nc.rejected || nc.lifetime.Err() != nil && !nc.disconnectEmits && !nc.connecting {
		return
	}
	nc.registerAck(&header, ack)
	pkg.Header = header
	if ack != nil && pkg.prepared != nil {
		pkg.prepared = pkg.prepared.WithHeader(header)
	}
	if nc.connecting {
		if (!isDone(nc.connection.closing) || nc.connection.draining && !nc.connection.appSealed && !isDone(nc.connection.discard)) && nc.connection.pendingWrites+nc.connection.drainQueued < nc.connection.writeBufferSize {
			nc.connection.pendingWrites++
			nc.pending = append(nc.pending, pkg)
		} else {
			nc.connection.overflowWrite(header)
		}
		return
	}
	nc.connection.queuePayloadLocked(pkg, false)
}

func (nc *namespaceConn) acceptPending() {
	nc.outMu.Lock()
	if done := nc.acceptDone; done != nil {
		nc.outMu.Unlock()
		<-done
		return
	}
	if !nc.connecting || nc.rejected {
		nc.outMu.Unlock()
		return
	}
	nc.acceptDone = make(chan struct{})
	nc.outMu.Unlock()
	// Adapter callbacks can emit or disconnect. Keep connecting true so their
	// packets join pending until this admission has queued CONNECT.
	nc.Join(nc.ID())
	nc.outMu.Lock()
	defer nc.outMu.Unlock()
	defer close(nc.acceptDone)
	if nc.rejected {
		return
	}
	pending := nc.pending
	nc.pending = nil
	nc.connecting = false
	if nh := nc.connection.namespace(fmtNS(nc.namespace)); nh != nil {
		nh.AddSocket(nc)
	}
	nc.connection.write(parser.Header{Type: parser.Connect, Namespace: fmtNS(nc.namespace)})
	for _, p := range pending {
		nc.connection.mu.Lock()
		nc.connection.queuePayloadLocked(p, true)
		nc.connection.mu.Unlock()
	}
}

func (nc *namespaceConn) reject() {
	nc.connection.mu.Lock()
	nc.endTyped()
	nc.connection.mu.Unlock()
	nc.outMu.Lock()
	nc.rejected = true
	nc.connecting = false
	nc.connection.mu.Lock()
	nc.connection.pendingWrites -= len(nc.pending)
	nc.connection.mu.Unlock()
	nc.pending = nil
	nc.outMu.Unlock()
	nc.retireRooms()
}

func (nc *namespaceConn) To(rooms ...string) Emitter {
	return (emitter{socket: nc}).To(rooms...)
}

func (nc *namespaceConn) Broadcast() Emitter {
	return (emitter{socket: nc}).asBroadcast()
}

func (nc *namespaceConn) Volatile() Emitter { return (emitter{socket: nc}).Volatile() }

func (nc *namespaceConn) Compress(on bool) Emitter {
	return (emitter{socket: nc}).Compress(on)
}

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

// connectReturned releases deferred lifecycle work after application admission ends.
func (nc *namespaceConn) connectReturned(err error) {
	nc.outMu.Lock()
	nc.connectErr = err
	close(nc.connectDone)
	nc.outMu.Unlock()
}

// Reserved emits notify local listeners without changing the socket lifecycle.
func (nc *namespaceConn) emitLocal(event string, args ...interface{}) {
	nc.outMu.Lock()
	if nc.localDispatch {
		nc.outMu.Unlock()
		return
	}
	nc.localDispatch = true
	nc.outMu.Unlock()
	defer func() { nc.outMu.Lock(); nc.localDispatch = false; nc.outMu.Unlock() }()
	handler := nc.connection.namespace(fmtNS(nc.namespace))
	if handler == nil {
		return
	}
	values := make([]reflect.Value, len(args))
	for i, arg := range args {
		if arg == nil {
			values[i] = reflect.Zero(reflect.TypeOf((*interface{})(nil)).Elem())
		} else {
			values[i] = reflect.ValueOf(arg)
		}
	}
	if _, err := handler.DispatchEvent(nc, event, values...); err != nil {
		if onError := handler.ErrorHandler(); onError != nil {
			onError(nc, err)
		} else {
			nc.connection.unhandled(nc.namespace, err)
		}
	}
}

package socketio

import (
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/internal/connection"
	"github.com/sshaplygin/go-socket.io/internal/namespace"
)

// ServerNamespace is a server namespace's registration and broadcast object.
type ServerNamespace struct {
	server *Server
	name   string
	parent *dynamicNamespace
}

func serverNamespaceName(name string) string {
	if name == "" || name == "/" {
		return rootNamespace
	}
	if !strings.HasPrefix(name, "/") {
		return "/" + name
	}
	return name
}

// Of returns the namespace, creating it if necessary. Empty and "/" name the
// root; a name without a leading slash gets one.
func (s *Server) Of(name string) *ServerNamespace {
	name = serverNamespaceName(name)
	s.createNamespace(name)
	s.createMu.Lock()
	defer s.createMu.Unlock()
	return s.views[name]
}

// Use registers middleware on a server namespace.
func (s *Server) Use(name string, f func(Conn) error) { s.Of(name).Use(f) }

// Name returns the namespace name, "/" for the root.
func (n *ServerNamespace) Name() string {
	if n.name == "" {
		return "/"
	}
	return n.name
}

// OnConnect sets the namespace's connect handler.
func (n *ServerNamespace) OnConnect(f func(Conn) error) { n.registration().OnConnect(f) }

// OnDisconnect sets the namespace's disconnect handler.
func (n *ServerNamespace) OnDisconnect(f func(Conn, string)) { n.registration().OnDisconnect(f) }

// OnError sets the namespace's error handler.
func (n *ServerNamespace) OnError(f func(Conn, error)) { n.registration().OnError(f) }

// OnEvent replaces every listener of the event; handler semantics are those of Server.OnEvent.
func (n *ServerNamespace) OnEvent(event string, f interface{}) { n.registration().OnEvent(event, f) }

// Use appends namespace middleware, called in registration order before OnConnect
// and before the socket joins its ID room. A rejection sends ERROR and keeps the
// engine connection and its other namespaces open.
func (n *ServerNamespace) Use(f func(Conn) error) {
	h := n.registration()
	h.Use(f)
}

// Emit broadcasts through the adapter once per socket; a trailing func returns ErrBroadcastAck.
func (n *ServerNamespace) Emit(event string, args ...interface{}) error {
	return n.emitter().Emit(event, args...)
}

// Send broadcasts the message event.
func (n *ServerNamespace) Send(args ...interface{}) error { return n.Emit("message", args...) }

// Sockets returns a snapshot of this instance's connected sockets.
func (n *ServerNamespace) Sockets() []Conn {
	if n.parent != nil {
		var result []Conn
		for _, child := range n.children() {
			result = append(result, child.Sockets()...)
		}
		return result
	}
	h := n.server.getNamespace(n.name)
	return h.Sockets()
}

// Socket finds a connected socket by its namespace ID on this instance.
func (n *ServerNamespace) Socket(id string) (Conn, bool) {
	for _, c := range n.Sockets() {
		if c.ID() == id {
			return c, true
		}
	}
	return nil, false
}

func (n *ServerNamespace) emitter() Emitter {
	if n.parent != nil {
		return connection.NewEmitter(parentBroadcast{namespace: n})
	}
	return connection.NewEmitter(n.server.getNamespace(n.name).Adapter())
}

// To starts a broadcast to a union of rooms, once per socket.
func (n *ServerNamespace) To(rooms ...string) Emitter { return n.emitter().To(rooms...) }

// Except starts a namespace broadcast excluding the given socket IDs.
func (n *ServerNamespace) Except(ids ...string) Emitter { return n.emitter().Except(ids...) }

// Volatile starts a broadcast that skips connections that cannot take a packet now.
func (n *ServerNamespace) Volatile() Emitter { return n.emitter().Volatile() }

// Local starts a broadcast restricted to this instance.
func (n *ServerNamespace) Local() Emitter { return n.emitter().Local() }

// SocketIDs lists socket IDs in any of rooms, or the whole namespace. With the
// Redis adapter it lists this instance only. The returned slice is a copy.
func (n *ServerNamespace) SocketIDs(rooms ...string) []string {
	if n.parent != nil {
		return parentBroadcast{namespace: n}.IDs(rooms...)
	}
	return n.registration().Adapter().IDs(rooms...)
}

func (n *ServerNamespace) registration() *namespace.Handler {
	if n.parent != nil {
		return n.parent.registration
	}
	return n.server.getNamespace(n.name)
}

// Adapter returns the namespace adapter, nil for a dynamic parent or failed construction.
func (n *ServerNamespace) Adapter() adapter.Adapter {
	if n.parent != nil {
		return nil
	}
	h := n.registration()
	if h.Err() != nil {
		return nil
	}
	return h.Adapter()
}

type dynamicNamespace struct {
	match        func(string, url.Values) bool
	registration *namespace.Handler
	mu           sync.RWMutex
	children     map[string]*ServerNamespace
}

// OfFunc registers a dynamic parent. Unknown namespace CONNECTs are offered to
// match functions in order, on the client's read goroutine. The first match
// creates a child with the parent's current registrations and its own adapter.
// Parent broadcasts and snapshots span its children; later registrations affect
// children created afterwards only. A parent broadcast stops at the first child
// adapter error; earlier child deliveries remain. Callback rejection sends nothing.
func (s *Server) OfFunc(match func(string, url.Values) bool) *ServerNamespace {
	s.parentsMu.Lock()
	defer s.parentsMu.Unlock()
	parent := &dynamicNamespace{match: match, registration: namespace.NewHandler(), children: make(map[string]*ServerNamespace)}
	n := &ServerNamespace{server: s, name: "/_" + strconv.Itoa(len(s.parents)), parent: parent}
	s.parents = append(s.parents, parent)
	return n
}

func (n *ServerNamespace) children() []*ServerNamespace {
	n.parent.mu.RLock()
	defer n.parent.mu.RUnlock()
	result := make([]*ServerNamespace, 0, len(n.parent.children))
	for _, child := range n.parent.children {
		result = append(result, child)
	}
	return result
}

type parentBroadcast struct {
	namespace.NopAdapter
	namespace *ServerNamespace
}

func (b parentBroadcast) SendTo(opts adapter.Options, event string, args ...interface{}) error {
	for _, child := range b.namespace.children() {
		if err := child.registration().Adapter().SendTo(opts, event, args...); err != nil {
			return err
		}
	}
	return nil
}

func (b parentBroadcast) IDs(rooms ...string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, child := range b.namespace.children() {
		for _, id := range child.registration().Adapter().IDs(rooms...) {
			if !seen[id] {
				seen[id] = true
				result = append(result, id)
			}
		}
	}
	return result
}

// On adds a listener after existing listeners; off removes just this listener.
// Listener payload parameter types must match; a mismatch or non-function panics.
func (n *ServerNamespace) On(event string, f interface{}) (off func()) {
	return n.registration().AddListener(event, f, false)
}

// Once is On for the first successfully dispatched event only.
func (n *ServerNamespace) Once(event string, f interface{}) (off func()) {
	return n.registration().AddListener(event, f, true)
}

// Off removes every listener for event, including the OnEvent handler. Changes
// during a dispatch take effect on the next event. OnEvent replaces all listeners.
func (n *ServerNamespace) Off(event string) { n.registration().Off(event) }

// Compress starts a namespace broadcast with the chosen per-packet compression flag.
func (n *ServerNamespace) Compress(on bool) Emitter { return n.emitter().Compress(on) }

const aliasRootNamespace, rootNamespace = "/", ""

func cloneQuery(query url.Values) url.Values {
	result := make(url.Values, len(query))
	for key, values := range query {
		result[key] = append([]string(nil), values...)
	}
	return result
}

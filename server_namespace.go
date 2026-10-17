package socketio

import "strings"

// ServerNamespace is a server namespace's registration and broadcast object.
type ServerNamespace struct {
	server *Server
	name   string
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
	return s.createNamespace(serverNamespaceName(name)).view
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
func (n *ServerNamespace) OnConnect(f func(Conn) error) { n.server.OnConnect(n.name, f) }

// OnDisconnect sets the namespace's disconnect handler.
func (n *ServerNamespace) OnDisconnect(f func(Conn, string)) { n.server.OnDisconnect(n.name, f) }

// OnError sets the namespace's error handler.
func (n *ServerNamespace) OnError(f func(Conn, error)) { n.server.OnError(n.name, f) }

// OnEvent sets the namespace's event handler.
func (n *ServerNamespace) OnEvent(event string, f interface{}) { n.server.OnEvent(n.name, event, f) }

// Use appends namespace middleware, called in registration order before OnConnect
// and before the socket joins its ID room. A rejection sends ERROR and keeps the
// engine connection and its other namespaces open.
func (n *ServerNamespace) Use(f func(Conn) error) {
	h := n.server.createNamespace(n.name)
	h.eventsLock.Lock()
	h.middleware = append(h.middleware, f)
	h.eventsLock.Unlock()
}

// Emit broadcasts through the adapter once per socket; a trailing func returns ErrBroadcastAck.
func (n *ServerNamespace) Emit(event string, args ...interface{}) error {
	return n.emitter().Emit(event, args...)
}

// Send broadcasts the message event.
func (n *ServerNamespace) Send(args ...interface{}) error { return n.Emit("message", args...) }

// Sockets returns a snapshot of this instance's connected sockets.
func (n *ServerNamespace) Sockets() []Conn {
	h := n.server.getNamespace(n.name)
	h.connectedMu.RLock()
	defer h.connectedMu.RUnlock()
	result := make([]Conn, 0, len(h.connected))
	for _, c := range h.connected {
		result = append(result, c)
	}
	return result
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

func (n *ServerNamespace) emitter() emitter {
	return emitter{adapter: n.server.getNamespace(n.name).broadcast, broadcast: true}
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
	return n.emitter().adapter.IDs(rooms...)
}

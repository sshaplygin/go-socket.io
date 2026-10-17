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

// Emit broadcasts through the configured adapter once per connected socket.
func (n *ServerNamespace) Emit(event string, args ...interface{}) error {
	n.server.BroadcastToNamespace(n.name, event, args...)
	return nil
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

package engineio

import (
	"net/http"
	"sync"

	sessionImpl "github.com/sshaplygin/go-socket.io/engineio/internal/session"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// Hooks observes Engine.IO connection and transport events. A nil callback is
// ignored; other callbacks run synchronously on the goroutine of the event.
// Header values are borrowed for the call: a hook retaining them must copy them.
// Install hooks with WithHooks before accepting connections.
type Hooks struct {
	// Connection fires once per session, after its handshake response and before
	// Accept returns it; c.ReadyState() is StateOpen in the hook.
	Connection func(c Conn)
	// InitialHeaders fires once per session with the handshake request and the
	// headers of its response, before they are written; a hook may add to h.
	InitialHeaders func(r *http.Request, h http.Header)
	// Headers fires with every response of a session (each polling response, the
	// websocket upgrade response once), after InitialHeaders on the handshake, before
	// the headers are written; the configured cookie is in h.
	Headers func(r *http.Request, h http.Header)
	// ConnectionError fires when ServeHTTP rejects a request, with the engine.io 3.6.2
	// code and message (0 Transport unknown, 1 Session ID unknown, 2 Bad handshake
	// method, 3 Bad request, 4 Forbidden) and, for a request checker rejection, its
	// error.
	ConnectionError func(r *http.Request, code int, message string, err error)

	// Packet fires for every packet read from the client, PacketCreate for every
	// packet about to be written, each with the packet and frame types.
	Packet       func(c Conn, pt eioparser.PacketType, ft eioparser.FrameType)
	PacketCreate func(c Conn, pt eioparser.PacketType, ft eioparser.FrameType)
	// Heartbeat fires when a PING is received, before the PONG is written.
	Heartbeat func(c Conn)
	// Upgrading fires when the probe is accepted, Upgrade when the transport has
	// switched; transport is the name of the new one.
	Upgrading func(c Conn, transport string)
	Upgrade   func(c Conn, transport string)
	// Flush fires when a polling write batch starts, Drain when it has been written.
	Flush func(c Conn)
	Drain func(c Conn)
	// Error fires with the transport error that closes the session (the cause
	// transport error), before the close; a peer close or a ping timeout fires nothing.
	Error func(c Conn, err error)
}

func (s *Server) installHooks(c *sessionImpl.Session, request *http.Request, initial bool) {
	h := s.hooks
	if h == nil {
		return
	}
	sh := &sessionImpl.Hooks{}
	if h.Packet != nil {
		sh.Packet = func(c *sessionImpl.Session, pt eioparser.PacketType, ft eioparser.FrameType) { h.Packet(c, pt, ft) }
	}
	if h.PacketCreate != nil {
		sh.PacketCreate = func(c *sessionImpl.Session, pt eioparser.PacketType, ft eioparser.FrameType) {
			h.PacketCreate(c, pt, ft)
		}
	}
	if h.Heartbeat != nil {
		sh.Heartbeat = func(c *sessionImpl.Session) { h.Heartbeat(c) }
	}
	if h.Upgrading != nil {
		sh.Upgrading = func(c *sessionImpl.Session, tr string) { h.Upgrading(c, tr) }
	}
	if h.Upgrade != nil {
		sh.Upgrade = func(c *sessionImpl.Session, tr string) { h.Upgrade(c, tr) }
	}
	if h.Flush != nil {
		sh.Flush = func(c *sessionImpl.Session) { h.Flush(c) }
	}
	if h.Drain != nil {
		sh.Drain = func(c *sessionImpl.Session) { h.Drain(c) }
	}
	if h.Error != nil {
		sh.Error = func(c *sessionImpl.Session, err error) { h.Error(c, err) }
	}
	var once sync.Once
	sh.Headers = func(r *http.Request, headers http.Header) {
		if initial && r == request {
			once.Do(func() {
				if h.InitialHeaders != nil {
					h.InitialHeaders(r, headers)
				}
			})
		}
		if h.Headers != nil {
			h.Headers(r, headers)
		}
	}
	c.SetHooks(sh)
}

func (s *Server) connectionError(r *http.Request, code int, err error) {
	if h := s.hooks; h != nil && h.ConnectionError != nil {
		h.ConnectionError(r, code, serverErrors[code], err)
	}
}

func (s *Server) writeServerError(w http.ResponseWriter, r *http.Request, code int, err error) {
	s.errorCORSHeaders(w, r)
	s.connectionError(r, code, err)
	writeServerError(w, code)
}

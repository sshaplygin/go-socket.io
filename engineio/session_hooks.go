package engineio

import (
	"bufio"
	"net"
	"net/http"
	"sync"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func (s *Server) installHooks(c *session, request *http.Request, initial bool) {
	h := s.hooks
	if h == nil {
		return
	}
	sh := &sessionHooks{}
	if h.Packet != nil {
		sh.Packet = func(c *session, pt eioparser.PacketType, ft eioparser.FrameType) { h.Packet(c, pt, ft) }
	}
	if h.PacketCreate != nil {
		sh.PacketCreate = func(c *session, pt eioparser.PacketType, ft eioparser.FrameType) { h.PacketCreate(c, pt, ft) }
	}
	if h.Heartbeat != nil {
		sh.Heartbeat = func(c *session) { h.Heartbeat(c) }
	}
	if h.Upgrading != nil {
		sh.Upgrading = func(c *session, tr string) { h.Upgrading(c, tr) }
	}
	if h.Upgrade != nil {
		sh.Upgrade = func(c *session, tr string) { h.Upgrade(c, tr) }
	}
	if h.Flush != nil {
		sh.Flush = func(c *session) { h.Flush(c) }
	}
	if h.Drain != nil {
		sh.Drain = func(c *session) { h.Drain(c) }
	}
	if h.Error != nil {
		sh.Error = func(c *session, err error) { h.Error(c, err) }
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
	c.setHooks(sh)
}

func (s *Server) websocketHeaders(w http.ResponseWriter, r *http.Request, initial bool) http.ResponseWriter {
	h := s.hooks
	hijacker, ok := w.(http.Hijacker)
	if h == nil || h.InitialHeaders == nil && h.Headers == nil || !ok {
		return w
	}
	return &websocketHeaderObserver{ResponseWriter: w, hijacker: hijacker, notify: func() {
		if initial && h.InitialHeaders != nil {
			h.InitialHeaders(r, w.Header())
		}
		if h.Headers != nil {
			h.Headers(r, w.Header())
		}
	}}
}

// Gorilla hijacks only after validating the upgrade and origin, and formats
// responseHeader afterwards. The map passed to Accept remains the same map,
// so hooks can edit the successful 101 without observing rejected requests.
type websocketHeaderObserver struct {
	http.ResponseWriter
	hijacker http.Hijacker
	notify   func()
}

func (w *websocketHeaderObserver) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffers, err := w.hijacker.Hijack()
	if err == nil && buffers.Reader.Buffered() == 0 {
		w.notify()
	}
	return conn, buffers, err
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

// Server validation failures precede transport dispatch. Use the same resolved
// origin callback as that transport; unknown transports use the server default.
func (s *Server) errorCORSHeaders(w http.ResponseWriter, r *http.Request) {
	check := s.originCheck
	if tr, ok := s.transports.Get(r.URL.Query().Get("transport")); ok {
		switch tr := tr.(type) {
		case *polling.Transport:
			check = tr.CheckOrigin
			if check == nil {
				check = polling.Default.CheckOrigin
			}
		case *websocket.Transport:
			check = tr.CheckOrigin
		}
	}
	if check == nil || !check(r) {
		return
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
}

// sessionHooks is the private session half of public Hooks: NewServer
// wraps each configured Hooks callback into the field of the same name. A nil
// field is ignored. Plumbing for the engine server, not user API.
type sessionHooks struct {
	// Headers observes a session response after the transport prepares its headers
	// and before they are written. The engine server installs it before startup;
	// nil leaves an already installed response observer unchanged.
	Headers      func(r *http.Request, h http.Header)
	Packet       func(s *session, pt eioparser.PacketType, ft eioparser.FrameType)
	PacketCreate func(s *session, pt eioparser.PacketType, ft eioparser.FrameType)
	Heartbeat    func(s *session)
	Upgrading    func(s *session, transport string)
	Upgrade      func(s *session, transport string)
	Flush        func(s *session)
	Drain        func(s *session)
	Error        func(s *session, err error)
}

// setHooks installs h; the engine server calls it after newSession and before initSession,
// on the goroutine that created the session, so every event of the session is observed.
func (s *session) setHooks(h *sessionHooks) {
	if h != nil && h.Headers != nil {
		s.headers = h.Headers
	}
	s.hooks = h
}

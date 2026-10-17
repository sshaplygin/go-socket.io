package engineio

import (
	"bufio"
	"net"
	"net/http"
	"sync"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/internal"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func (s *Server) installHooks(c *session.Session, request *http.Request, initial bool) {
	h := s.hooks
	if h == nil {
		return
	}
	sh := &session.Hooks{}
	if h.Packet != nil {
		sh.Packet = func(c *session.Session, pt packet.Type, ft frame.Type) { h.Packet(c, pt, ft) }
	}
	if h.PacketCreate != nil {
		sh.PacketCreate = func(c *session.Session, pt packet.Type, ft frame.Type) { h.PacketCreate(c, pt, ft) }
	}
	if h.Heartbeat != nil {
		sh.Heartbeat = func(c *session.Session) { h.Heartbeat(c) }
	}
	if h.Upgrading != nil {
		sh.Upgrading = func(c *session.Session, tr string) { h.Upgrading(c, tr) }
	}
	if h.Upgrade != nil {
		sh.Upgrade = func(c *session.Session, tr string) { h.Upgrade(c, tr) }
	}
	if h.Flush != nil {
		sh.Flush = func(c *session.Session) { h.Flush(c) }
	}
	if h.Drain != nil {
		sh.Drain = func(c *session.Session) { h.Drain(c) }
	}
	if h.Error != nil {
		sh.Error = func(c *session.Session, err error) { h.Error(c, err) }
	}
	c.SetHooks(sh)
	var once sync.Once
	internal.ObserveHeaders(c, func(r *http.Request, headers http.Header) {
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
	})
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

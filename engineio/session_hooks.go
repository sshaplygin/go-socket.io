package engineio

import (
	"net/http"
	"sync"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/internal"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/session"
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

func (s *Server) websocketHeaders(w http.ResponseWriter, r *http.Request, initial bool) {
	if h := s.hooks; h != nil {
		if initial && h.InitialHeaders != nil {
			h.InitialHeaders(r, w.Header())
		}
		if h.Headers != nil {
			h.Headers(r, w.Header())
		}
	}
}

func (s *Server) connectionError(r *http.Request, code int, err error) {
	if h := s.hooks; h != nil && h.ConnectionError != nil {
		h.ConnectionError(r, code, serverErrors[code], err)
	}
}

func (s *Server) writeServerError(w http.ResponseWriter, r *http.Request, code int, err error) {
	s.connectionError(r, code, err)
	writeServerError(w, code)
}

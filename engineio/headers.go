package engineio

import (
	"bufio"
	"net"
	"net/http"

	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

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

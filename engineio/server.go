package engineio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/internal"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

var _ http.Handler = (*Server)(nil)

// protocolVersion is the only value of the EIO query parameter the server serves.
const protocolVersion = "4"

// Server is instance of server
type Server struct {
	pingInterval time.Duration
	pingTimeout  time.Duration

	transports *transport.Manager
	sessions   *session.Manager

	requestChecker CheckerFunc
	connInitor     ConnInitorFunc

	connChan  chan *session.Session // hands sessions to Accept; never closed
	closed    chan struct{}         // closed by Close
	closeOnce sync.Once

	log *slog.Logger
}

// NewServer returns a server.
func NewServer(opts *Options) *Server {
	return &Server{
		transports:     transport.NewManager(opts.getTransport()),
		pingInterval:   opts.getPingInterval(),
		pingTimeout:    opts.getPingTimeout(),
		requestChecker: opts.getRequestChecker(),
		connInitor:     opts.getConnInitor(),
		sessions:       session.NewManager(opts.getSessionIDGenerator()),
		connChan:       make(chan *session.Session, 1),
		closed:         make(chan struct{}),
		log:            opts.getLogger(),
	}
}

// Close closes server. Accept returns io.EOF from then on. Every session not yet
// returned by Accept, and every session whose handshake completes later, is closed
// and removed; sessions already accepted stay open.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.dropUnaccepted()
	})
	return nil
}

// Accept accepts a connection.
func (s *Server) Accept() (Conn, error) {
	select {
	case c := <-s.connChan:
		if !isClosed(s.closed) {
			return c, nil
		}
		s.drop(c)
	case <-s.closed:
	}
	return nil, io.EOF
}

// dropUnaccepted closes the session waiting in the hand-off buffer, if any.
func (s *Server) dropUnaccepted() {
	select {
	case c := <-s.connChan:
		s.drop(c)
	default:
	}
}

func (s *Server) drop(c *session.Session) {
	_ = internal.Shutdown(c)
	s.sessions.Remove(c.ID())
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (s *Server) Addr() net.Addr {
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	reqTransport := query.Get("transport")
	if eio := query.Get("EIO"); eio != protocolVersion {
		internal.WriteError(w, http.StatusBadRequest, internal.CodeUnsupportedProto)
		s.reject(reqTransport, r.RemoteAddr, "bad eio", nil)
		return
	}

	srvTransport, ok := s.transports.Get(reqTransport)
	if !ok {
		internal.WriteError(w, http.StatusBadRequest, internal.CodeUnknownTransport)
		s.reject(reqTransport, r.RemoteAddr, "bad transport", nil)
		return
	}

	header, err := s.requestChecker(r)
	if err != nil {
		internal.WriteError(w, http.StatusForbidden, internal.CodeForbidden)
		s.reject(reqTransport, r.RemoteAddr, "checker", err)
		return
	}

	for k, v := range header {
		w.Header()[k] = v
	}

	sid := query.Get("sid")
	reqSession, ok := s.sessions.Get(sid)
	// if we can't find session in current session pool, let's create this. behaviour for new connections
	if !ok {
		if sid != "" {
			internal.WriteError(w, http.StatusBadRequest, internal.CodeUnknownSID)
			s.reject(reqTransport, r.RemoteAddr, "unknown sid", nil)
			return
		}

		// A handshake is a GET. An OPTIONS preflight keeps reaching the transport,
		// which answers it with the CORS headers.
		if r.Method != http.MethodGet && r.Method != http.MethodOptions {
			internal.WriteError(w, http.StatusBadRequest, internal.CodeBadHandshake)
			s.reject(reqTransport, r.RemoteAddr, "bad handshake method", nil)
			return
		}

		transportConn, err := srvTransport.Accept(w, r)
		if err != nil {
			s.acceptFailed(w, r, reqTransport, err, "transport accept err: ")
			return
		}

		reqSession, err = s.newSession(r.Context(), transportConn, reqTransport)
		if err != nil {
			internal.WriteError(w, http.StatusBadRequest, internal.CodeBadRequest)
			s.reject(reqTransport, r.RemoteAddr, "init", err)
			return
		}

		s.connInitor(r, reqSession)
	}

	// try upgrade current connection
	if current := reqSession.Transport(); current != reqTransport {
		if !s.canUpgrade(current, reqTransport) {
			internal.WriteError(w, http.StatusBadRequest, internal.CodeBadRequest)
			s.reject(reqTransport, r.RemoteAddr, "bad upgrade", nil)
			return
		}

		transportConn, err := srvTransport.Accept(w, r)
		if err != nil {
			s.acceptFailed(w, r, reqTransport, err, "")
			return
		}

		reqSession.Upgrade(reqTransport, transportConn)

		if handler, ok := transportConn.(http.Handler); ok {
			handler.ServeHTTP(w, r)
		}
		return
	}

	reqSession.ServeHTTP(w, r)
}

// Count counts connected
func (s *Server) Count() int {
	return s.sessions.Count()
}

// Remove session from sessions pool. Experimental API.
func (s *Server) Remove(sid string) {
	s.sessions.Remove(sid)
}

func (s *Server) newSession(_ context.Context, conn transport.Conn, reqTransport string) (*session.Session, error) {
	params := transport.ConnParameters{
		PingInterval: s.pingInterval,
		PingTimeout:  s.pingTimeout,
		Upgrades:     s.transports.UpgradeFrom(reqTransport),
	}

	sid := s.sessions.NewID()
	newSession, err := session.New(conn, sid, reqTransport, params, s.log)
	if err != nil {
		return nil, err
	}

	// Register before the handshake: the client may send its next request
	// with this sid as soon as it reads the OPEN packet.
	s.sessions.Add(newSession)

	go func(newSession *session.Session) {
		if err := newSession.InitSession(); err != nil {
			s.sessions.Remove(newSession.ID())
			s.reject(reqTransport, fmt.Sprint(conn.RemoteAddr()), "init", err)

			return
		}

		select {
		case s.connChan <- newSession:
			if isClosed(s.closed) { // Close may have emptied the buffer before this send
				s.dropUnaccepted()
			}
		case <-s.closed:
			s.drop(newSession)
		}
	}(newSession)

	return newSession, nil
}

// acceptFailed answers a request whose transport refused to accept it. A
// websocket.HandshakeError has already been answered by the transport; a response
// writer that cannot be hijacked is answered 501, logged with reason "no hijacker";
// anything else is a 502 whose body starts with prefix.
func (s *Server) acceptFailed(w http.ResponseWriter, r *http.Request, reqTransport string, err error, prefix string) {
	var handshake websocket.HandshakeError
	switch {
	case errors.As(err, &handshake):
		s.reject(reqTransport, r.RemoteAddr, "accept", err)
	case errors.Is(err, websocket.ErrNotHijacker):
		http.Error(w, "websocket upgrade is not supported by this connection", http.StatusNotImplemented)
		s.reject(reqTransport, r.RemoteAddr, "no hijacker", err)
	default:
		http.Error(w, prefix+err.Error(), http.StatusBadGateway)
		s.reject(reqTransport, r.RemoteAddr, "accept", err)
	}
}

// reject logs a request ServeHTTP rejects or a failed session initialisation: at DEBUG
// for an unknown sid, at WARN otherwise.
func (s *Server) reject(transport, addr, reason string, err error) {
	level, args := slog.LevelWarn, []any{"transport", transport, "remote_addr", addr, "reason", reason}
	if reason == "unknown sid" {
		level = slog.LevelDebug
	}
	if err != nil {
		args = append(args, "err", err)
	}
	s.log.Log(context.Background(), level, "engineio: request rejected", args...)
}

// canUpgrade reports whether a session on transport from may move to
// transport to, that is whether to comes later in the configured transport
// order. Anything else, including a request on an earlier transport with a
// live sid, is rejected by ServeHTTP instead of being treated as an upgrade.
func (s *Server) canUpgrade(from, to string) bool {
	for _, name := range s.transports.UpgradeFrom(from) {
		if name == to {
			return true
		}
	}
	return false
}

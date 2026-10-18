package engineio

import (
	"context"
	"encoding/json"
	"fmt"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// Server is instance of server
type Server struct {
	originCheck       func(*http.Request) bool
	pingInterval      time.Duration
	pingTimeout       time.Duration
	upgradeTimeout    time.Duration
	maxHTTPBufferSize int64
	disableUpgrades   bool

	transports *transport.Manager
	sessions   *session.Manager

	requestChecker CheckerFunc
	connInitor     ConnInitorFunc
	hooks          *Hooks

	connChan  chan *session.Session // hands sessions to Accept; never closed
	closed    chan struct{}         // closed by Close
	closeOnce sync.Once
	// lifecycle orders registration against shutdown; live supplies the shutdown
	// snapshot until session.Manager gains its public snapshot API.
	lifecycle sync.Mutex
	live      map[string]*session.Session

	log *slog.Logger
}

// NewServer returns a server.
func NewServer(opts *Options) *Server {
	return &Server{
		transports:        transport.NewManager(opts.getTransport()),
		pingInterval:      opts.getPingInterval(),
		pingTimeout:       opts.getPingTimeout(),
		upgradeTimeout:    opts.getUpgradeTimeout(),
		maxHTTPBufferSize: opts.getMaxHTTPBufferSize(),
		disableUpgrades:   opts != nil && opts.DisableUpgrades,
		live:              make(map[string]*session.Session),
		requestChecker:    opts.getRequestChecker(),
		originCheck:       opts.getOriginCheck(),
		connInitor:        opts.getConnInitor(),
		hooks:             opts.getHooks(),
		sessions:          session.NewManager(opts.getSessionIDGenerator()),
		connChan:          make(chan *session.Session, 1),
		closed:            make(chan struct{}),
		log:               opts.getLogger(),
	}
}

// Close closes server. Accept returns io.EOF from then on. Every session not yet
// returned by Accept, and every accepted session, is closed and removed.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.lifecycle.Lock()
		close(s.closed)
		live := s.live
		s.live = make(map[string]*session.Session)
		for id := range live {
			s.sessions.Remove(id)
		}
		s.lifecycle.Unlock()
		for _, c := range live {
			_ = c.Shutdown()
		}
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
	_ = c.Shutdown()
	s.Remove(c.ID())
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
	if isClosed(s.closed) {
		s.writeServerError(w, r, 3, nil)
		return
	}
	query := r.URL.Query()

	reqTransport := query.Get("transport")
	srvTransport, ok := s.transports.Get(reqTransport)
	if !ok {
		s.writeServerError(w, r, 0, nil)
		s.reject(reqTransport, r.RemoteAddr, "bad transport", nil)
		return
	}

	sid := query.Get("sid")
	if sid == "" && r.Method != http.MethodGet {
		s.writeServerError(w, r, 2, nil)
		return
	}
	reqSession, ok := s.sessions.Get(sid)
	// if we can't find session in current session pool, let's create this. behaviour for new connections
	if !ok {
		if sid != "" {
			s.writeServerError(w, r, 1, nil)
			s.reject(reqTransport, r.RemoteAddr, "unknown sid", nil)
			return
		}
		header, err := s.requestChecker(r)
		for k, v := range header {
			w.Header()[k] = v
		}
		if err != nil {
			s.writeServerError(w, r, 4, err)
			s.reject(reqTransport, r.RemoteAddr, "checker", err)
			return
		}

		if reqTransport == "websocket" {
			w = s.websocketHeaders(w, r, sid == "")
		}
		transportConn, err := srvTransport.Accept(w, r)
		if err != nil {
			// as on upgrade: the websocket library answered a HandshakeError itself
			if _, ok := err.(websocket.HandshakeError); !ok {
				s.writeServerError(w, r, 3, err)
			} else {
				s.connectionError(r, 3, err)
			}
			s.reject(reqTransport, r.RemoteAddr, "accept", err)
			return
		}

		reqSession, err = s.newSession(r, transportConn, reqTransport)
		if err != nil {
			if reqTransport != "websocket" {
				s.writeServerError(w, r, 3, err)
			} else {
				s.connectionError(r, 3, err)
			}
			s.reject(reqTransport, r.RemoteAddr, "init", err)
			return
		}
	}

	// try upgrade current connection
	if current := reqSession.Transport(); current != reqTransport {
		if s.disableUpgrades || !s.canUpgrade(current, reqTransport) {
			s.writeServerError(w, r, 3, nil)
			s.reject(reqTransport, r.RemoteAddr, "bad upgrade", nil)
			return
		}

		if reqTransport == "websocket" {
			w = s.websocketHeaders(w, r, sid == "")
		}
		transportConn, err := srvTransport.Accept(w, r)
		if err != nil {
			// don't call http.Error() for HandshakeErrors because
			// they get handled by the websocket library internally.
			if _, ok := err.(websocket.HandshakeError); !ok {
				s.writeServerError(w, r, 3, err)
			} else {
				s.connectionError(r, 3, err)
			}
			s.reject(reqTransport, r.RemoteAddr, "accept", err)
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

// Clients returns a snapshot of open or closing sessions keyed by session ID.
// Count includes every registered session until Remove, regardless of state.
func (s *Server) Clients() map[string]Conn {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	clients := make(map[string]Conn)
	for id, c := range s.sessions.All() {
		if state := c.ReadyState(); state == StateOpen || state == StateClosing {
			clients[id] = c
		}
	}
	return clients
}

// Remove session from sessions pool. Experimental API.
func (s *Server) Remove(sid string) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	delete(s.live, sid)
	s.sessions.Remove(sid)
}

func (s *Server) newSession(r *http.Request, conn transport.Conn, reqTransport string) (*session.Session, error) {
	params := eioparser.ConnParameters{
		PingInterval: s.pingInterval,
		PingTimeout:  s.pingTimeout,
		Upgrades:     s.transports.UpgradeFrom(reqTransport),
	}
	if s.disableUpgrades {
		params.Upgrades = []string{}
	}

	sid := s.sessions.NewID()
	if sid == "" {
		_ = conn.Close()
		return nil, fmt.Errorf("generate session id: empty identifier")
	}
	newSession, err := session.New(conn, sid, reqTransport, params, s.log)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	newSession.SetLimits(s.maxHTTPBufferSize, s.upgradeTimeout)
	newSession.SetRequest(r)
	s.installHooks(newSession, r, reqTransport != "websocket")

	// Register before the handshake: the client may send its next request
	// with this sid as soon as it reads the OPEN packet.
	s.lifecycle.Lock()
	if isClosed(s.closed) {
		s.lifecycle.Unlock()
		_ = newSession.Shutdown()
		return nil, io.EOF
	}
	s.sessions.Add(newSession)
	s.live[sid] = newSession
	s.lifecycle.Unlock()
	s.connInitor(r, newSession)

	go func(newSession *session.Session) {
		if err := newSession.InitSession(); err != nil {
			s.Remove(newSession.ID())
			s.connectionError(r, 3, err)
			s.reject(reqTransport, fmt.Sprint(conn.RemoteAddr()), "init", err)

			return
		}

		if s.hooks != nil && s.hooks.Connection != nil {
			s.hooks.Connection(newSession)
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

var serverErrors = [...]string{"Transport unknown", "Session ID unknown", "Bad handshake method", "Bad request", "Forbidden"}

func writeServerError(w http.ResponseWriter, code int) {
	status := http.StatusBadRequest
	if code == 4 {
		status = http.StatusForbidden
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{code, serverErrors[code]})
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

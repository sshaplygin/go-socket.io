package engineio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	sessionImpl "github.com/sshaplygin/go-socket.io/engineio/internal/session"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// Server is an Engine.IO server: an http.Handler that opens sessions on the
// polling and websocket transports and hands each session to Accept once its
// handshake has been written. Its methods are safe for concurrent use.
type Server struct {
	originCheck       func(*http.Request) bool
	pingInterval      time.Duration
	pingTimeout       time.Duration
	upgradeTimeout    time.Duration
	maxHTTPBufferSize int64
	disableUpgrades   bool

	transports *transport.Manager
	sessions   *sessionImpl.Manager

	requestChecker CheckerFunc
	connInitor     ConnInitorFunc
	hooks          *Hooks

	connChan  chan *sessionImpl.Session // hands sessions to Accept; never closed
	closed    chan struct{}             // closed by Close
	closeOnce sync.Once
	// lifecycle orders registration against shutdown; live supplies the shutdown
	// snapshot independently of Remove, so shutdown includes unregistered accepted sessions.
	lifecycle sync.Mutex
	live      map[string]*sessionImpl.Session

	log *slog.Logger
}

// NewServer returns a Server configured by opts; an option that is not given
// keeps its default. The Server serves requests through ServeHTTP and needs no
// start call; Close shuts it down.
func NewServer(opts ...Option) *Server {
	config := resolveOptions(opts...)
	return &Server{
		transports:        transport.NewManager(transport.WithTransports(config.getTransport()...)),
		pingInterval:      config.getPingInterval(),
		pingTimeout:       config.getPingTimeout(),
		upgradeTimeout:    config.getUpgradeTimeout(),
		maxHTTPBufferSize: config.getMaxHTTPBufferSize(),
		disableUpgrades:   config.DisableUpgrades,
		live:              make(map[string]*sessionImpl.Session),
		requestChecker:    config.getRequestChecker(),
		originCheck:       config.getOriginCheck(),
		connInitor:        config.getConnInitor(),
		hooks:             config.getHooks(),
		sessions:          sessionImpl.NewManager(config.getSessionIDGenerator()),
		connChan:          make(chan *sessionImpl.Session, 1),
		closed:            make(chan struct{}),
		log:               config.getLogger(),
	}
}

// Close closes server. Accept returns io.EOF from then on. Every session not yet
// returned by Accept, and every accepted session, is closed and removed. An
// in-progress Error hook completes its transport close when the hook returns.
// A websocket session receives a close frame with status 1000, as from Conn.Close.
// Sessions shut down concurrently, so a blocked hook does not delay other sessions;
// their Error hooks may therefore run concurrently on library goroutines.
// Once shutdown has started, repeated calls return without waiting for it to
// finish, so an Error hook can call Close during shutdown.
func (s *Server) Close() error {
	if isClosed(s.closed) {
		return nil
	}
	s.closeOnce.Do(func() {
		s.lifecycle.Lock()
		close(s.closed)
		live := s.live
		s.live = make(map[string]*sessionImpl.Session)
		for id := range s.sessions.All() {
			s.sessions.Remove(id)
		}
		s.lifecycle.Unlock()
		var shutdown sync.WaitGroup
		shutdown.Add(len(live))
		for _, c := range live {
			go func() {
				defer shutdown.Done()
				_ = c.Shutdown()
			}()
		}
		s.dropUnaccepted()
		shutdown.Wait()
	})
	return nil
}

// Accept blocks until a session has written its handshake and run the
// Connection hook, and returns it. The caller owns the returned Conn: it reads
// the messages with NextReader and ends the session with Close. A session that
// completes its handshake waits until an Accept call takes it. After Close,
// Accept returns io.EOF, and a session that was still waiting is closed. Accept
// may be called from several goroutines; each session is returned once.
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

func (s *Server) drop(c *sessionImpl.Session) {
	_ = c.Shutdown()
	s.unregister(c, false)
}

// unregister removes session c from the registry, and with live also from the
// shutdown set. It leaves an entry that another session holds: the ID of an
// ended session can be issued again, so removal by ID could hit a newer session.
func (s *Server) unregister(c *sessionImpl.Session, live bool) {
	sid := c.ID()
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if live && s.live[sid] == c {
		delete(s.live, sid)
	}
	if registered, ok := s.sessions.Get(sid); ok && registered == c {
		s.sessions.Remove(sid)
	}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// Addr returns nil. A Server does not listen on an address of its own; it is
// mounted as an http.Handler on the caller's HTTP server.
func (s *Server) Addr() net.Addr {
	return nil
}

// ServeHTTP handles one Engine.IO request. A GET request without a sid query
// parameter starts a session on the transport named by the transport
// parameter; a request with a sid continues that session or upgrades it to
// another transport. It does not check the request path, so it answers on
// whatever path the caller routes to it. A request it rejects is answered with
// the JSON error bodies described in docs/PROTOCOL.md; after Close every
// request is rejected as a bad request.
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

	// A websocket transport has no further HTTP requests once established.
	// Reject duplicate requests before dispatching to its lifetime handler.
	if sid != "" && reqTransport == "websocket" && reqSession.Transport() == "websocket" {
		s.writeServerError(w, r, 3, nil)
		s.reject(reqTransport, r.RemoteAddr, "duplicate websocket", nil)
		return
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

// Count returns the number of registered sessions, whatever their state. A
// session is registered from its handshake until its close has finished, by
// whichever path it ends: a client CLOSE, a transport error, a ping timeout,
// Conn.Close, Abort or Server.Close. A polling session closed by Conn.Close
// finishes once a poll has delivered its CLOSE packet, or when Abort,
// Server.Close or a 30-second limit ends the wait.
// The server then forgets the session's ID: a later request carrying it is
// answered as for an unknown session. Remove unregisters a session earlier.
func (s *Server) Count() int {
	return s.sessions.Count()
}

// Clients returns a snapshot of open or closing sessions keyed by session ID.
// A session is open from the start of its OPEN packet's delivery, so one whose
// client has received that packet is listed. A session that is still opening,
// or already closed, is not. Count also includes registered sessions that are
// opening, or closed and still finishing their close.
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

// Remove unregisters the session registered under sid without closing it.
// Server.Close still closes the session if its transport remains open. A
// session unregisters itself when its close finishes, so an ended session needs
// no Remove call. After that, sid names no session and Remove does nothing,
// unless an IDGenerator has issued sid again: Remove then unregisters the newer
// session.
func (s *Server) Remove(sid string) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.sessions.Remove(sid)
}

func (s *Server) newSession(r *http.Request, conn transport.Conn, reqTransport string) (*sessionImpl.Session, error) {
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
	newSession, err := sessionImpl.New(conn, sid, reqTransport, params, s.log)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	newSession.SetLimits(s.maxHTTPBufferSize, s.upgradeTimeout)
	newSession.SetRequest(r)
	s.installHooks(newSession, r, reqTransport != "websocket")

	// The ended session leaves the shutdown snapshot and the registry. A session
	// refused as a duplicate also runs this, so only this session's entries go.
	newSession.SetCloseCallback(func() { s.unregister(newSession, true) })

	// Register before the handshake: the client may send its next request
	// with this sid as soon as it reads the OPEN packet.
	s.lifecycle.Lock()
	if isClosed(s.closed) {
		s.lifecycle.Unlock()
		_ = newSession.Shutdown()
		return nil, io.EOF
	}
	if _, exists := s.sessions.Get(sid); exists || s.live[sid] != nil {
		s.lifecycle.Unlock()
		_ = newSession.Shutdown()
		return nil, fmt.Errorf("generate session id: duplicate identifier")
	}
	s.sessions.Add(newSession)
	s.live[sid] = newSession
	s.lifecycle.Unlock()

	s.connInitor(r, newSession)

	go func(newSession *sessionImpl.Session) {
		if err := newSession.Init(); err != nil {
			// A close during the handshake has already unregistered it, and
			// its ID may since belong to a newer session.
			s.unregister(newSession, false)
			if errors.Is(err, sessionImpl.ErrClosedDuringInit) {
				// Opened and closed by someone else while its handshake was
				// being written: not a refused request. The session's own
				// close record has the reason.
				s.log.Debug("engineio: session closed during handshake", "sid", newSession.ID(), "transport", reqTransport)
				return
			}
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

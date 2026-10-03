package engineio

import (
	"context"
	"fmt"
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
	_ = c.Close()
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
	srvTransport, ok := s.transports.Get(reqTransport)
	if !ok {
		http.Error(w, fmt.Sprintf("invalid transport: %s", reqTransport), http.StatusBadRequest)
		return
	}

	header, err := s.requestChecker(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("request checker err: %s", err.Error()), http.StatusBadGateway)
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
			http.Error(w, fmt.Sprintf("invalid sid value: %s", sid), http.StatusBadRequest)
			return
		}

		transportConn, err := srvTransport.Accept(w, r)
		if err != nil {
			http.Error(w, fmt.Sprintf("transport accept err: %s", err.Error()), http.StatusBadGateway)
			return
		}

		reqSession, err = s.newSession(r.Context(), transportConn, reqTransport)
		if err != nil {
			http.Error(w, fmt.Sprintf("create new session err: %s", err.Error()), http.StatusBadRequest)
			return
		}

		s.connInitor(r, reqSession)
	}

	// try upgrade current connection
	if current := reqSession.Transport(); current != reqTransport {
		if !s.canUpgrade(current, reqTransport) {
			http.Error(w, fmt.Sprintf("invalid transport upgrade: %s to %s", current, reqTransport), http.StatusBadRequest)
			return
		}

		transportConn, err := srvTransport.Accept(w, r)
		if err != nil {
			// don't call http.Error() for HandshakeErrors because
			// they get handled by the websocket library internally.
			if _, ok := err.(websocket.HandshakeError); !ok {
				http.Error(w, err.Error(), http.StatusBadGateway)
			}
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
			s.log.Error("init new session", "err", err)

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

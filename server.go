package socketio

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gomodule/redigo/redis"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/logger"
	"github.com/sshaplygin/go-socket.io/parser"
)

// Server is a go-socket.io server.
type Server struct {
	engine *engineio.Server

	handlers *namespaceHandlers

	redisAdapter *RedisAdapterOptions
	createMu     sync.Mutex            // serialises handler creation, not dispatch; see createNamespace
	closed       atomic.Bool           // set by Close; read under createMu by createNamespace
	adapterErr   atomic.Pointer[error] // the first Redis construction error, read by Serve
	served       chan struct{}         // tests set it; Serve closes it after its entry check

	limits connLimits
	log    *slog.Logger
}

// NewServer returns a server.
func NewServer(opts *engineio.Options) *Server {
	return &Server{
		handlers: newNamespaceHandlers(),
		engine:   engineio.NewServer(opts),
		limits:   newConnLimits(opts),
		log:      loggerFrom(opts),
	}
}

// loggerFrom returns opts.Logger passed through logger.Wrap, or logger.Log
// when opts or the field is nil. It mirrors engineio.Options.getLogger for the
// socket.io layer.
func loggerFrom(opts *engineio.Options) *slog.Logger {
	if opts != nil && opts.Logger != nil {
		return logger.Wrap(opts.Logger)
	}
	return logger.Log
}

// Adapter sets the Redis broadcast adapter for the namespaces registered after it. A
// namespace builds its Redis broadcast when its first handler is registered; its two
// Redis connections, AUTH and SELECT included, must be ready within 10 seconds. If that
// fails, the namespace keeps a no-op broadcast and the error, which names the namespace
// and wraps the Redis error; registering more handlers does not retry, and Serve returns
// the first such error. The Server room methods of that namespace do nothing (RoomLen
// returns -1, Rooms nil, the others false; ForEach does not call f). A connection to it
// fails before OnConnect: for the root namespace the error goes to root OnError with a
// nil Conn and nothing is written; for another namespace it goes to that namespace's
// OnError, whose Conn has no rooms, and the connection closes as on a dispatch error.
func (s *Server) Adapter(opts *RedisAdapterOptions) (bool, error) {
	opts = getOptions(opts)
	var redisOpts []redis.DialOption
	if len(opts.Password) > 0 {
		redisOpts = append(redisOpts, redis.DialPassword(opts.Password))
	}
	if opts.DB > 0 {
		redisOpts = append(redisOpts, redis.DialDatabase(opts.DB))
	}

	conn, err := redis.Dial(opts.Network, opts.getAddr(), redisOpts...)
	if err != nil {
		return false, err
	}

	s.redisAdapter = opts

	return true, conn.Close()
}

// Close closes the engine.io server, so Serve returns nil, and stops the Redis
// connections of every namespace. It does not close the sessions already open, but their
// broadcasts no longer reach other instances. Close waits for a handler registration
// that is dialling Redis, up to the 10 seconds that dial may take. With Adapter set, a
// namespace registered after Close has no broadcast, as if its construction failed, but
// Serve does not return that error.
func (s *Server) Close() error {
	s.closed.Store(true)
	err := s.engine.Close()

	s.createMu.Lock() // waits for a registration that is building a broadcast
	defer s.createMu.Unlock()
	s.handlers.Range(func(h *namespaceHandler) {
		if bc, ok := h.broadcast.(*redisBroadcast); ok {
			bc.close()
		}
	})

	return err
}

// ServeHTTP dispatches the request to the handler whose pattern most closely matches the request URL.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.engine.ServeHTTP(w, r)
}

// OnConnect set a handler function f to handle open event for namespace.
func (s *Server) OnConnect(namespace string, f func(Conn) error) {
	h := s.getNamespace(namespace)
	if h == nil {
		h = s.createNamespace(namespace)
	}

	h.OnConnect(f)
}

// OnDisconnect set a handler function f to handle disconnect event for namespace.
func (s *Server) OnDisconnect(namespace string, f func(Conn, string)) {
	h := s.getNamespace(namespace)
	if h == nil {
		h = s.createNamespace(namespace)
	}

	h.OnDisconnect(f)
}

// OnError set a handler function f to handle error for namespace.
func (s *Server) OnError(namespace string, f func(Conn, error)) {
	h := s.getNamespace(namespace)
	if h == nil {
		h = s.createNamespace(namespace)
	}

	h.OnError(f)
}

// OnEvent sets the handler f for event in namespace.
//
// f must be a func whose first parameter is Conn; the remaining parameters are
// decoded from the event arguments. If f returns values, or the client asked for an
// acknowledgement, the return values are sent back to the client as the ACK payload.
// f is called on the read goroutine of the connection, so a blocking f delays only
// that client. OnEvent panics if f is not a func or its first parameter is not Conn.
func (s *Server) OnEvent(namespace, event string, f interface{}) {
	h := s.getNamespace(namespace)
	if h == nil {
		h = s.createNamespace(namespace)
	}

	h.OnEvent(event, f)
}

// Serve accepts and serves connections until Close is called, then returns nil. If a
// namespace's Redis broadcast could not be created before Serve was called (see Adapter)
// and Close has not been called, Serve returns that error at once without accepting a
// connection. The engine.io server then still completes handshakes, which nobody serves,
// until Close is called.
func (s *Server) Serve() error {
	if s.closed.Load() {
		return nil
	}
	if err := s.adapterErr.Load(); err != nil {
		return *err
	}
	if s.served != nil {
		close(s.served)
	}
	for {
		conn, err := s.engine.Accept()
		if errors.Is(err, io.EOF) {
			// Accept reports io.EOF once Close has been called.
			return nil
		}
		if err != nil {
			return err
		}

		go s.serveConn(conn)
	}
}

// broadcastOf returns the broadcast of namespace, or nil when the namespace has no
// handler or its Redis broadcast could not be created.
func (s *Server) broadcastOf(namespace string) Broadcast {
	if h := s.getNamespace(namespace); h != nil && h.err == nil {
		return h.broadcast
	}
	return nil
}

// JoinRoom joins given connection to the room.
func (s *Server) JoinRoom(namespace string, room string, connection Conn) bool {
	bc := s.broadcastOf(namespace)
	if bc != nil {
		bc.Join(room, connection)
	}
	return bc != nil
}

// LeaveRoom leaves given connection from the room.
func (s *Server) LeaveRoom(namespace string, room string, connection Conn) bool {
	bc := s.broadcastOf(namespace)
	if bc != nil {
		bc.Leave(room, connection)
	}
	return bc != nil
}

// LeaveAllRooms leaves the given connection from all rooms.
func (s *Server) LeaveAllRooms(namespace string, connection Conn) bool {
	bc := s.broadcastOf(namespace)
	if bc != nil {
		bc.LeaveAll(connection)
	}
	return bc != nil
}

// ClearRoom clears the room.
func (s *Server) ClearRoom(namespace string, room string) bool {
	bc := s.broadcastOf(namespace)
	if bc != nil {
		bc.Clear(room)
	}
	return bc != nil
}

// BroadcastToRoom broadcasts given event & args to all the connections in the room.
func (s *Server) BroadcastToRoom(namespace string, room, event string, args ...interface{}) bool {
	bc := s.broadcastOf(namespace)
	if bc != nil {
		bc.Send(room, event, args...)
	}
	return bc != nil
}

// BroadcastToNamespace broadcasts given event & args to all the connections in the same namespace.
func (s *Server) BroadcastToNamespace(namespace string, event string, args ...interface{}) bool {
	bc := s.broadcastOf(namespace)
	if bc != nil {
		bc.SendAll(event, args...)
	}
	return bc != nil
}

// RoomLen gives number of connections in the room.
func (s *Server) RoomLen(namespace string, room string) int {
	if bc := s.broadcastOf(namespace); bc != nil {
		return bc.Len(room)
	}
	return -1
}

// Rooms gives list of all the rooms.
func (s *Server) Rooms(namespace string) []string {
	if bc := s.broadcastOf(namespace); bc != nil {
		return bc.Rooms(nil)
	}
	return nil
}

// Count number of connections.
func (s *Server) Count() int {
	return s.engine.Count()
}

// Remove session from sessions pool. Fixed the sessions map leak(connections, mem).
func (s *Server) Remove(sid string) {
	s.engine.Remove(sid)
}

// ForEach sends data by DataFunc, if room does not exit sends anything.
func (s *Server) ForEach(namespace string, room string, f EachFunc) bool {
	bc := s.broadcastOf(namespace)
	if bc != nil {
		bc.ForEach(room, f)
	}
	return bc != nil
}

func (s *Server) serveConn(conn engineio.Conn) {
	c := newConn(conn, s.handlers, s.limits, s.log.With("sid", conn.ID()))
	if !c.connected(c.connect()) {
		return
	}

	go c.serveError()
	go c.serveWrite()
	go func() { c.serveRead(connectPacketHandler, disconnectPacketHandler); s.engine.Remove(c.Conn.ID()) }()
}

func (c *conn) serveError() {
	for {
		select {
		case <-c.closing:
			c.reportOverflow(c.overflow)
			return
		case err := <-c.errorChan:
			var errMsg *errorMessage
			if !errors.As(err, &errMsg) {
				continue
			}

			if handler := c.namespace(errMsg.namespace); handler != nil {
				if handler.onError != nil && errMsg.conn != nil {
					handler.onError(errMsg.conn, errMsg.err)
				}
			}
			close(errMsg.done)
		}
	}
}

// serveWrite writes until the queue is discarded, or empty after the seal.
func (c *conn) serveWrite() {
	seal := c.seal
	for {
		select {
		case <-c.discard:
			return
		case <-seal:
			seal = nil
		case pkg := <-c.writeChan:
			if err := c.encoder.Encode(pkg.Header, pkg.Data); err != nil {
				c.onError(pkg.Header.Namespace, err) // not once a close started
				c.stop()                             // also ends a drain
			}
		}
		if seal == nil && len(c.writeChan) == 0 {
			c.stop()
		}
	}
}

// serveRead dispatches packets until a close takes the namespaces.
func (c *conn) serveRead(connect, disconnect func(*conn, parser.Header) error) {
	defer c.finish()

	var event string

	for {
		var header parser.Header

		if err := c.decoder.DecodeHeader(&header, &event); err != nil {
			c.log.Error("decode packet header", "err", err)
			c.onError(rootNamespace, err)
			return
		}

		if header.Namespace == aliasRootNamespace {
			header.Namespace = rootNamespace
		}

		var err error
		closed := isDone(c.closing) // then the handlers dispatch nothing and end no drain
		switch header.Type {
		case parser.Ack:
			err = ackPacketHandler(c, header)
		case parser.Connect:
			err = connect(c, header)
		case parser.Disconnect:
			err = disconnect(c, header)
		case parser.Event:
			err = eventPacketHandler(c, event, header)
		}

		if err != nil && !closed {
			c.log.Error("serve read", "err", err)

			return
		}
	}
}

// createNamespace returns the handler of nsp, creating it if needed. Under createMu it
// rechecks, builds, records the first construction error and only then stores the
// handler, so one namespace never builds two broadcasts and dispatch, which reads
// s.handlers, never waits for a Redis dial.
func (s *Server) createNamespace(nsp string) *namespaceHandler {
	nsp = fmtNS(nsp)
	s.createMu.Lock()
	defer s.createMu.Unlock()
	if handler, ok := s.handlers.Get(nsp); ok {
		return handler
	}

	var handler *namespaceHandler
	if s.redisAdapter != nil && s.closed.Load() { // no Redis broadcast, and not recorded for Serve
		handler = newNamespaceHandler(nsp, nil)
		handler.broadcast, handler.err = nopBroadcast{}, errServerClosed
	} else if handler = newNamespaceHandler(nsp, s.redisAdapter); handler.err != nil && s.adapterErr.Load() == nil {
		s.adapterErr.Store(&handler.err)
	}
	s.handlers.Set(nsp, handler)

	return handler
}

func (s *Server) getNamespace(nsp string) *namespaceHandler {
	if nsp == aliasRootNamespace {
		nsp = rootNamespace
	}

	ret, ok := s.handlers.Get(nsp)
	if !ok {
		return nil
	}

	return ret
}

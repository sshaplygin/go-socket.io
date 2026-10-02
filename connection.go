package socketio

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"time"

	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/session"
	"github.com/googollee/go-socket.io/parser"
)

// defaultWriteBufferSize is the outbound queue size; see ErrWriteBufferFull.
const defaultWriteBufferSize = 64

const defaultDrainTimeout = time.Minute // see Conn.Close

// Conn is a connection in go-socket.io
type Conn interface {
	// Close closes the connection and returns nil; only the first close, by
	// Close or by the library, has an effect. Close runs OnDisconnect for
	// every connected namespace, leaves its rooms and returns without waiting.
	// The packets queued until then, Emits from OnDisconnect included, are
	// written in the background; an Emit that finds the queue full, or comes
	// later, is dropped without a report. Then, or one minute after Close
	// started, the rest is discarded and the engine.io connection is closed;
	// Server.Count counts the session until then. Incoming packets are no
	// longer dispatched. The library closes a connection on a read, decode,
	// dispatch or encode error (an encode error is reported to OnError
	// first), a peer close, a ping timeout, an overflow (see
	// ErrWriteBufferFull) or a failed connect, discarding the queue at once.
	Close() error
	Namespace

	// ID returns session id
	ID() string
	URL() url.URL
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	RemoteHeader() http.Header
}

type conn struct {
	engineio.Conn

	id         uint64
	handlers   *namespaceHandlers
	namespaces *namespaces

	encoder *parser.Encoder
	decoder *parser.Decoder

	writeChan chan parser.Payload
	errorChan chan error

	log *slog.Logger

	// mu orders the close state against queueing packets and connecting
	// namespaces. The channels close at those steps of the close.
	mu                           sync.Mutex
	closing, seal, discard, done chan struct{}
	draining, connecting         bool             // the first close is Close; serveConn runs connect
	overflow                     *namespaceConn   // the first close is its overflow
	pending                      []*namespaceConn // OnDisconnect calls a library close owes
	drainTimer                   *time.Timer
	drainTimeout                 time.Duration // tests shorten it
}

func newConn(engineConn engineio.Conn, handlers *namespaceHandlers, log *slog.Logger) *conn {
	c := &conn{
		log:        log,
		Conn:       engineConn,
		decoder:    parser.NewDecoder(engineConn),
		errorChan:  make(chan error),
		writeChan:  make(chan parser.Payload, defaultWriteBufferSize),
		handlers:   handlers,
		namespaces: newNamespaces(),
		closing:    make(chan struct{}),
		seal:       make(chan struct{}),
		discard:    make(chan struct{}),
		done:       make(chan struct{}),
	}
	c.encoder, c.drainTimeout = parser.NewEncoder(queueWriter{c}), defaultDrainTimeout
	return c
}

func (c *conn) Close() error {
	c.mu.Lock()
	if isDone(c.closing) {
		c.mu.Unlock()
		return nil
	}
	ncs := c.startClose()
	c.draining = true
	c.drainTimer = time.AfterFunc(c.drainTimeout, c.stop)
	c.mu.Unlock()

	c.disconnect(ncs)
	c.mu.Lock()
	close(c.seal)
	c.mu.Unlock()
	return nil
}

// startClose takes the connected namespaces; c.mu is held.
func (c *conn) startClose() (ncs []*namespaceConn) {
	close(c.closing)
	c.namespaces.Range(func(_ string, nc *namespaceConn) { ncs = append(ncs, nc) })
	for _, nc := range ncs {
		c.namespaces.Delete(fmtNS(nc.namespace))
	}
	return ncs
}

// stop starts a library close, or ends a drain.
func (c *conn) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopLocked()
}

func (c *conn) stopLocked() {
	if !isDone(c.closing) {
		c.pending = c.startClose()
	}
	if !isDone(c.discard) {
		close(c.discard)
		if c.drainTimer != nil {
			c.drainTimer.Stop()
		}
		go func() { _ = c.Conn.Close() }() // the emitter of an overflow must not block
	}
}

func (c *conn) finish() {
	c.mu.Lock()
	ncs := c.pending
	c.mu.Unlock()
	c.disconnect(ncs)
	close(c.done)
}

func (c *conn) disconnect(ncs []*namespaceConn) {
	for _, nc := range ncs {
		if nh := c.namespace(fmtNS(nc.namespace)); nh != nil && nh.onDisconnect != nil {
			nh.onDisconnect(nc, clientDisconnectMsg)
		}
		nc.LeaveAll()
	}
}

func (c *conn) register(nsp string, nc *namespaceConn) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if isDone(c.closing) {
		return false
	}
	c.namespaces.Set(nsp, nc)
	return true
}

func (c *conn) claim(nsp string) (*namespaceConn, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nc, ok := c.namespaces.Get(nsp)
	c.namespaces.Delete(nsp)
	return nc, ok
}

func (c *conn) connect() error {
	c.connecting = true // before root joins a room, so no other goroutine sees it
	rootHandler, ok := c.handlers.Get(rootNamespace)
	if !ok {
		return errUnavailableRootHandler
	}

	root := newNamespaceConn(c, aliasRootNamespace, rootHandler.broadcast)
	c.namespaces.Set(rootNamespace, root)

	root.Join(root.Conn.ID())

	c.namespaces.Range(func(ns string, nc *namespaceConn) {
		nc.SetContext(c.Conn.Context())
	})

	header := parser.Header{
		Type: parser.Connect,
	}

	if err := c.encoder.Encode(header); err != nil {
		return err
	}

	if _, err := rootHandler.dispatch(root, header); err != nil {
		return err
	}

	c.mu.Lock() // the goroutines start after this; an overflow before it fails the connect
	defer c.mu.Unlock()
	if c.overflow != nil {
		return ErrWriteBufferFull
	}
	c.connecting = false
	return nil
}

func (c *conn) nextID() uint64 {
	c.id++

	return c.id
}

func (c *conn) write(header parser.Header, args ...reflect.Value) {
	data := make([]interface{}, len(args))

	for i := range data {
		data[i] = args[i].Interface()
	}

	pkg := parser.Payload{
		Header: header,
		Data:   data,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case isDone(c.closing) && (!c.draining || isDone(c.seal) || isDone(c.discard)):
	case len(c.writeChan) < cap(c.writeChan):
		c.writeChan <- pkg
	case !isDone(c.closing):
		c.overflow, _ = c.namespaces.Get(header.Namespace)
		if c.pending = c.startClose(); !c.connecting { // else serveConn reports first
			c.stopLocked()
		}
	}
}

// reportOverflow reports an overflow with conn, nil on a failed connect as in v1.4.
func (c *conn) reportOverflow(conn Conn) {
	if nc := c.overflow; nc != nil {
		if nh := c.namespace(fmtNS(nc.namespace)); nh != nil && nh.onError != nil {
			nh.onError(conn, ErrWriteBufferFull)
		}
	}
}

// queueWriter starts no packet (its TEXT frame) once the queue is discarded.
type queueWriter struct{ c *conn }

func (w queueWriter) NextWriter(ft session.FrameType) (io.WriteCloser, error) {
	if ft == session.TEXT && isDone(w.c.discard) {
		return nil, io.EOF
	}
	return w.c.Conn.NextWriter(ft)
}

func isDone(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// onError reports err and waits for OnError, unless a close started.
func (c *conn) onError(namespace string, err error) {
	if isDone(c.closing) {
		return
	}
	msg := newErrorMessage(namespace, err)
	msg.conn, _ = c.namespaces.Get(namespace)
	select {
	case c.errorChan <- msg:
		<-msg.done
	case <-c.closing:
	}
}

func (c *conn) namespace(nsp string) *namespaceHandler {
	handler, _ := c.handlers.Get(nsp)
	return handler
}

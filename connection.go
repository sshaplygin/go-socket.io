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

// The outbound queue size (see ErrWriteBufferFull) and drain deadline (see Conn.Close).
const defaultWriteBufferSize, defaultDrainTimeout = 64, time.Minute

// Conn is a connection in go-socket.io
type Conn interface {
	// Close closes the connection and returns nil; only the first close, by Close or by the library,
	// has an effect. Close runs OnDisconnect for every connected namespace, leaves its rooms and
	// returns without waiting. The packets queued until then, Emits from OnDisconnect included, are
	// written in the background; an Emit that finds the queue full, or comes later, is dropped without
	// a report. Then, or one minute after Close started, the rest is discarded and the engine.io
	// connection is closed; Server.Count counts the session until then. Incoming packets are no longer
	// dispatched, and a read or write failure ends the drain at once. The library closes a connection
	// on a read, decode, dispatch or encode error (an encode error used to leave it open), a peer
	// close, a ping timeout, an overflow (see ErrWriteBufferFull) or a failed connect, discarding the
	// queue at once. Other than an overflow, the error is reported to OnError before the close's
	// effects run, so Emits from OnError still queue; a failed connect is reported to root OnError
	// with a nil Conn. Failures during a close are not reported.
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

	// mu orders the close state, whose steps close the channels, against queueing and registering.
	mu                           sync.Mutex
	closing, seal, discard, done chan struct{}
	draining, connecting         bool             // the first close is Close; until connected
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
	c.encoder, c.drainTimeout, c.connecting = parser.NewEncoder(queueWriter{c}), defaultDrainTimeout, true
	return c
}

func (c *conn) Close() error {
	c.mu.Lock()
	if isDone(c.closing) {
		c.mu.Unlock()
		return nil
	}
	ncs := c.startClose()
	c.draining, c.drainTimer = true, time.AfterFunc(c.drainTimeout, c.stop)
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

// finish ends the close: it discards, then runs the OnDisconnect calls a library close owes.
func (c *conn) finish() {
	c.mu.Lock()
	c.stopLocked()
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
	if !isDone(c.closing) {
		c.namespaces.Set(nsp, nc)
	}
	return !isDone(c.closing)
}

func (c *conn) claim(nsp string) (*namespaceConn, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nc, ok := c.namespaces.Get(nsp)
	c.namespaces.Delete(nsp)
	return nc, ok
}

func (c *conn) connect() error {
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

	_, err := rootHandler.dispatch(root, header)
	return err
}

// connected ends the connect before the goroutines start. A failure, err or an overflow, starts
// the close at once, so later Emits are dropped; err, then the overflow, is reported, then closed.
func (c *conn) connected(err error) bool {
	c.mu.Lock()
	failed := err != nil || isDone(c.closing) && !c.draining
	if c.connecting = false; failed && !isDone(c.closing) {
		c.pending = c.startClose()
	}
	c.mu.Unlock()
	if root := c.namespace(rootNamespace); err != nil && root != nil && root.onError != nil {
		root.onError(nil, err)
	}
	if failed {
		c.reportOverflow(nil)
		c.finish()
	}
	return !failed
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

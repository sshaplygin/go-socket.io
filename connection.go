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

// defaultWriteBufferSize is the number of outbound packets a connection
// queues while its writer is busy; one more closes the connection.
const defaultWriteBufferSize = 64

// defaultDrainTimeout bounds the drain of Close; see Conn.Close.
const defaultDrainTimeout = time.Minute

// Conn is a connection in go-socket.io
type Conn interface {
	io.Closer
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
	quitChan  chan struct{}
	done      chan struct{} // closed once the close has run every OnDisconnect it owes

	log *slog.Logger

	closeOnce sync.Once

	// mu orders write and flush: nothing is queued after the flush token.
	mu           sync.Mutex
	closing      bool
	queued       bool          // a packet was queued, so flush has work
	discard      bool          // the queue overflowed or no writer reads it
	token        bool          // the flush token is in writeChan
	written      chan struct{} // closed when the writer reaches the token
	draining     chan struct{} // closed by flush; onError then drops reports
	drainTimeout time.Duration // see Conn.Close; tests shorten it
}

func newConn(engineConn engineio.Conn, handlers *namespaceHandlers, log *slog.Logger) *conn {
	c := &conn{
		log:          log,
		Conn:         engineConn,
		decoder:      parser.NewDecoder(engineConn),
		errorChan:    make(chan error),
		writeChan:    make(chan parser.Payload, defaultWriteBufferSize+1),
		quitChan:     make(chan struct{}),
		done:         make(chan struct{}),
		handlers:     handlers,
		namespaces:   newNamespaces(),
		written:      make(chan struct{}),
		draining:     make(chan struct{}),
		drainTimeout: defaultDrainTimeout,
	}
	c.encoder = parser.NewEncoder(queueWriter{c})
	return c
}

func (c *conn) Close() error {
	var err error

	c.closeOnce.Do(func() {
		// for each namespace, leave all rooms, and call the disconnect handler.
		c.namespaces.Range(func(ns string, nc *namespaceConn) {
			if nh, _ := c.handlers.Get(ns); nh != nil && nh.onDisconnect != nil {
				nh.onDisconnect(nc, clientDisconnectMsg)
			}
			nc.LeaveAll()
		})
		c.flush()
		err = c.Conn.Close()

		close(c.quitChan)
		close(c.done)
	})

	return err
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

	handler, ok := c.handlers.Get(header.Namespace)
	if ok {
		_, err := handler.dispatch(root, header)
		if err != nil {
			c.mu.Lock()
			c.discard = true // serveConn closes c without starting its writer
			c.mu.Unlock()
		}
		return err
	}

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
	case c.closing:
	case len(c.writeChan) < defaultWriteBufferSize: // one slot is left for the token
		c.writeChan <- pkg
		c.queued = true
	case !c.discard: // report the first overflow only
		c.discard = true // an overflow close may discard the queue
		go c.closeOnOverflow(header.Namespace)
	}
}

// flush waits, at most drainTimeout, until the writer has written the packets
// queued before Close: it queues a token behind them in the slot write leaves
// free, and the writer takes writeChan in order.
func (c *conn) flush() {
	c.mu.Lock()
	c.closing = true
	close(c.draining) // the writer must not wait for a goroutine in Close
	if c.discard || !c.queued {
		c.mu.Unlock()
		return
	}
	c.writeChan <- parser.Payload{}
	c.token = true
	c.mu.Unlock()

	timer := time.NewTimer(c.drainTimeout)
	defer timer.Stop()
	select {
	case <-c.written:
	case <-timer.C:
	}
}

// queueWriter is the encoder's FrameWriter. A packet starts with a TEXT frame;
// the one starting with writeChan empty after the token was queued is the token.
type queueWriter struct{ c *conn }

func (w queueWriter) NextWriter(ft session.FrameType) (io.WriteCloser, error) {
	c := w.c
	c.mu.Lock()
	token := ft == session.TEXT && c.token && len(c.writeChan) == 0
	c.token = c.token && !token // close written once
	c.mu.Unlock()
	if token {
		close(c.written)
		return discardWriter{io.Discard}, nil
	}
	return c.Conn.NextWriter(ft)
}

type discardWriter struct{ io.Writer }

func (discardWriter) Close() error { return nil }

// closeOnOverflow reports ErrWriteBufferFull to OnError of namespace unless
// Close starts draining first, then closes the engine.io connection; the read
// goroutine fails and runs OnDisconnect, unordered with OnError, after its
// handler returns. It runs off the emitter, which may take the report itself.
func (c *conn) closeOnOverflow(namespace string) {
	c.onError(namespace, ErrWriteBufferFull)

	select {
	case <-c.quitChan:
		return
	default:
	}

	if err := c.Conn.Close(); err != nil {
		c.log.Error("close engine.io connection", "err", err)
	}
}

func (c *conn) onError(namespace string, err error) {
	select {
	case c.errorChan <- newErrorMessage(namespace, err):
	case <-c.quitChan:
	case <-c.draining:
	}
}

func (c *conn) namespace(nsp string) *namespaceHandler {
	handler, _ := c.handlers.Get(nsp)
	return handler
}

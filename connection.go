package socketio

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sync"

	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/parser"
)

// defaultWriteBufferSize is the number of outbound packets a connection
// queues while its writer is busy; one more closes the connection.
const defaultWriteBufferSize = 64

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

	log *slog.Logger

	closeOnce    sync.Once
	overflowOnce sync.Once
}

func newConn(engineConn engineio.Conn, handlers *namespaceHandlers, log *slog.Logger) *conn {
	return &conn{
		log:        log,
		Conn:       engineConn,
		encoder:    parser.NewEncoder(engineConn),
		decoder:    parser.NewDecoder(engineConn),
		errorChan:  make(chan error),
		writeChan:  make(chan parser.Payload, defaultWriteBufferSize),
		quitChan:   make(chan struct{}),
		handlers:   handlers,
		namespaces: newNamespaces(),
	}
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
		err = c.Conn.Close()

		close(c.quitChan)
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

	select {
	case c.writeChan <- pkg:
	case <-c.quitChan:
	default:
		c.overflowOnce.Do(func() { go c.closeOnOverflow(header.Namespace) })
	}
}

// closeOnOverflow reports errWriteBufferFull to the OnError handler of
// namespace and then closes only the engine.io connection. The read goroutine
// (serveRead or clientRead) then fails to read and its deferred Close runs the
// OnDisconnect handlers and leaves the rooms, after any handler running on it
// has returned, so the handlers of a connection keep running on one
// goroutine. closeOnOverflow runs on its own goroutine because the emitter
// may be unable to wait for the report: an Emit from OnError runs on the
// goroutine that receives it, and an Emit from OnDisconnect runs inside
// Close. The report only hands the error to serveError, so OnError and
// OnDisconnect run in no fixed order, and the report is dropped if the
// connection quits first.
func (c *conn) closeOnOverflow(namespace string) {
	c.onError(namespace, errWriteBufferFull)

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
		return
	}
}

func (c *conn) namespace(nsp string) *namespaceHandler {
	handler, _ := c.handlers.Get(nsp)
	return handler
}

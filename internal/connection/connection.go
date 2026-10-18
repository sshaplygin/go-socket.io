package connection

import (
	"cmp"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	namespaceImpl "github.com/sshaplygin/go-socket.io/internal/namespace"
	"github.com/sshaplygin/go-socket.io/parser"
)

// The outbound queue size (see ErrWriteBufferFull) and drain deadline (see namespaceImpl.Conn.Close).
const DefaultWriteBufferSize, DefaultDrainTimeout = 64, time.Minute

type Connection struct {
	engineio.Conn

	id               atomic.Uint64
	request          *http.Request
	resolveNamespace func(string, url.Values) *namespaceImpl.Handler
	handlers         *namespaceImpl.Registry
	namespaces       *namespaces

	encoder *parser.Encoder
	decoder *parser.Decoder
	reader  *frameReader // tells the decoder's transport failures apart
	writer  *queueWriter // tells the encoder's transport failures apart

	writeReady     chan struct{}
	drainQueue     []drainPacket
	pendingWrites  int
	drainQueued    int
	controlQueued  int
	closeControls  int
	controlPending int // claimed namespace disconnects not yet enqueued; guarded by mu
	writeChan      chan outboundPayload
	errorChan      chan error

	log          *slog.Logger
	rootRejected error
	boundary     bool // log the namespace connect and disconnect records (Server only)

	// mu orders the close state, whose steps close the channels, against queueing and registering.
	mu                           sync.Mutex
	closing, seal, discard, done chan struct{}
	draining, connecting         bool             // the first close is Close; until connected
	appSealed                    bool             // Close returned from its own callbacks
	overflow                     *namespaceConn   // the first close is its overflow, in this namespace
	pending                      []*namespaceConn // OnDisconnect calls a library close owes
	drainTimer                   *time.Timer
	drainTimeout                 time.Duration  // tests shorten it
	disconnectReason             string         // guarded by mu; forced close unless read observes an engine close
	disconnects                  sync.WaitGroup // registered under mu before claiming a namespace
	disconnecting                int            // callbacks claimed but not completed; guarded by mu
}

func New(engineConn engineio.Conn, handlers *namespaceImpl.Registry, writeBufferSize int, drainTimeout time.Duration, log *slog.Logger) *Connection {
	c := &Connection{
		log:              log,
		disconnectReason: "forced close",
		Conn:             engineConn,
		errorChan:        make(chan error),
		writeChan:        make(chan outboundPayload, writeBufferSize),
		writeReady:       make(chan struct{}, 1),
		handlers:         handlers,
		namespaces:       newNamespaces(),
		closing:          make(chan struct{}),
		seal:             make(chan struct{}),
		discard:          make(chan struct{}),
		done:             make(chan struct{}),
	}
	if source, ok := engineConn.(interface{ Request() *http.Request }); ok {
		c.request = source.Request()
	}
	c.reader, c.writer = &frameReader{FrameReader: engineConn}, &queueWriter{c: c}
	c.decoder, c.encoder = parser.NewDecoder(c.reader), parser.NewEncoder(c.writer)
	c.drainTimeout, c.connecting = drainTimeout, true
	return c
}

// connectRecord logs the namespace connect record of nsp, with err unless it is nil.
func (c *Connection) connectRecord(nsp string, err error) {
	if args := []any{nspAttr(nsp)}; err == nil {
		c.log.Debug("socketio: namespace connect", args...)
	} else {
		c.log.Debug("socketio: namespace connect", append(args, "err", err)...)
	}
}

func (c *Connection) connect() error {
	rootHandler, ok := c.handlers.Get(rootNamespace)
	if !ok {
		return errUnavailableRootHandler
	}
	if rootHandler.Err() != nil { // its Redis broadcast could not be created
		return rootHandler.Err()
	}

	root := newNamespaceConn(c, aliasRootNamespace, rootHandler.Adapter())
	root.beginConnect()
	root.SetContext(c.Conn.Context())
	c.namespaces.Set(rootNamespace, root)
	if err := rootHandler.Connect(root); err != nil {
		if isDone(c.closing) {
			return err
		}
		c.claim(rootNamespace)
		root.reject()
		c.rootRejected = err
		if onError := rootHandler.ErrorHandler(); onError != nil {
			onError(nil, err)
		} else {
			c.unhandled(rootNamespace, err)
		}
		c.write(parser.Header{Type: parser.Error}, reflect.ValueOf(err.Error()))
		return nil
	}
	if isDone(c.closing) {
		return nil
	}
	root.acceptPending()
	return nil
}

// connected ends the connect; a failure reports err first, so Emits from OnError queue, then closes.
func (c *Connection) connected(err error) bool {
	if err != nil {
		root := c.namespace(rootNamespace)
		var onError func(namespaceImpl.Conn, error)
		if root != nil {
			onError = root.ErrorHandler()
		}
		if onError != nil {
			onError(nil, err)
		} else if !expected(err, c.writer.failed) {
			c.unhandled(rootNamespace, err)
		}
	}
	c.mu.Lock()
	failed := err != nil || isDone(c.closing) && !c.draining
	if c.connecting = false; failed && !isDone(c.closing) {
		c.pending = c.startClose()
	}
	c.mu.Unlock()
	if failed && c.overflow != nil { // final: no overflow once a close started
		err = errors.Join(ErrWriteBufferFull, err)
	}
	if c.boundary {
		c.connectRecord(rootNamespace, cmp.Or(err, c.rootRejected))
	}
	if failed {
		c.reportOverflow(nil)
		c.finish()
	}
	return !failed
}

func (c *Connection) nextID() uint64 {
	return c.id.Add(1)
}

// reportOverflow reports an overflow with conn, nil on a failed connect as in v1.4.
func (c *Connection) reportOverflow(conn namespaceImpl.Conn) {
	if c.overflow == nil {
		return
	}
	if nh := c.namespace(fmtNS(c.overflow.namespace)); nh != nil {
		if onError := nh.ErrorHandler(); onError != nil {
			onError(conn, ErrWriteBufferFull)
			return
		}
	}
	c.unhandled(c.overflow.namespace, ErrWriteBufferFull)
}

// unhandled logs err, which no OnError receives and which is not expected closure.
func (c *Connection) unhandled(nsp string, err error) {
	c.log.Warn("socketio: unhandled error", nspAttr(nsp), "err", err)
}

// nspAttr is the nsp attribute of namespace nsp; the root namespace is "/".
func nspAttr(nsp string) slog.Attr {
	return slog.String("nsp", cmp.Or(nsp, aliasRootNamespace))
}

// errConn returns the namespaceImpl.Conn to report an error of nsp with (new if nsp was disconnected), or nil.
func (c *Connection) errConn(nsp string) (nc *namespaceConn) {
	if nh := c.namespace(nsp); nh != nil && nh.ErrorHandler() != nil {
		if nc, _ = c.namespaces.Get(nsp); nc == nil {
			nc = newNamespaceConn(c, cmp.Or(nsp, aliasRootNamespace), nh.Adapter())
		}
	}
	return nc
}

// onError reports err of the packet being read (see report); a failure of the frame
// reader that the decoder returned is expected closure.
func (c *Connection) onError(namespace string, err error) {
	c.report(namespace, err, c.reader.failed)
}

// report delivers err to OnError of namespace and waits for it, unless a close started.
// Without OnError, err is logged as unhandled unless it is the frame failure failed.
func (c *Connection) report(namespace string, err, failed error) {
	if isDone(c.closing) {
		return
	}
	msg := newErrorMessage(namespace, err)
	if msg.conn = c.errConn(namespace); msg.conn == nil {
		if !expected(err, failed) {
			c.unhandled(namespace, err)
		}
		return
	}
	select {
	case c.errorChan <- msg:
		<-msg.done
	case <-c.closing:
	}
}

func (c *Connection) namespace(nsp string) *namespaceImpl.Handler {
	handler, _ := c.handlers.Get(nsp)
	return handler
}

func (c *Connection) EngineConn() engineio.Conn {
	if c.boundary {
		return c.Conn
	}
	return nil
}

func (c *Connection) Request() *http.Request {
	if c.boundary {
		return c.request
	}
	return nil
}

// Start connects the root socket and starts its error and writer loops.
// The public server registers this connection before invoking Start.
func (c *Connection) Start(resolve func(string, url.Values) *namespaceImpl.Handler) bool {
	c.boundary = true
	c.resolveNamespace = resolve
	if !c.connected(c.connect()) {
		return false
	}
	go c.serveError()
	go c.serveWrite()
	return true
}

// Read dispatches until the transport ends and completes its local close effects.
func (c *Connection) Read() { c.serveRead(connectPacketHandler, disconnectPacketHandler) }

// Wait waits for local close and every disconnect claimed before it.
func (c *Connection) Wait() { <-c.done; c.disconnects.Wait() }

package socketio

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

	"github.com/sshaplygin/go-socket.io/parser"
)

// The outbound queue size (see ErrWriteBufferFull) and drain deadline (see Conn.Close).
const defaultWriteBufferSize, defaultDrainTimeout = 64, time.Minute

// Configuration, codec ownership and channel references are installed before
// workers start. The header reader and dispatcher hand off decoder ownership;
// the writer owns encoder/writer. Registries synchronize their own contents.
// rootRejected is initialized before dispatch; id is atomic.
type connection struct {
	engineio.Conn

	id                 atomic.Uint64
	request            *http.Request
	resolveNamespace   func(string, url.Values) *namespaceHandler
	handlers           *namespaceRegistry
	namespaces         *namespaces
	retiring           map[string]*parser.Header // guarded by mu
	reconnectReady     []string                  // guarded by mu
	readReady          chan struct{}
	engineDone         <-chan struct{}
	engineObserverDone chan struct{}

	encoder *parser.Encoder
	decoder *parser.Decoder
	reader  *frameReader // tells the decoder's transport failures apart
	writer  *queueWriter // tells the encoder's transport failures apart

	writeReady      chan struct{}
	drainQueue      []drainPacket
	pendingWrites   int
	drainQueued     int
	controlQueued   int
	closeControls   int
	controlPending  int // claimed namespace disconnects not yet enqueued; guarded by mu
	writeBufferSize int
	errorChan       chan error

	log          *slog.Logger
	rootRejected error
	errorOnce    sync.Once
	state        connectionState // guarded by mu

	// mu guards state, queue contents/counters, namespace retirement and admission,
	// connecting, draining, appSealed, overflow, pending, drainTimer and disconnectReason.
	// Channels publish transitions; done has one closer on the read dispatcher.
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

func newConnection(engineConn engineio.Conn, handlers *namespaceRegistry, writeBufferSize int, drainTimeout time.Duration, log *slog.Logger) *connection {
	c := &connection{
		log:              log,
		disconnectReason: "forced close",
		Conn:             engineConn,
		errorChan:        make(chan error),
		writeBufferSize:  writeBufferSize,
		writeReady:       make(chan struct{}, 1),
		handlers:         handlers,
		namespaces:       newNamespaces(),
		retiring:         make(map[string]*parser.Header),
		readReady:        make(chan struct{}, 1),
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
	if signal, ok := engineConn.(interface{ Done() <-chan struct{} }); ok {
		c.engineDone = signal.Done()
		if c.engineDone != nil {
			c.engineObserverDone = make(chan struct{})
			go c.observeEngineClose()
		}
	}

	return c
}

// connectRecord logs the namespace connect record of nsp, with err unless it is nil.
func (c *connection) connectRecord(nsp string, err error) {
	if args := []any{nspAttr(nsp)}; err == nil {
		c.log.Debug("socketio: namespace connect", args...)
	} else {
		c.log.Debug("socketio: namespace connect", append(args, "err", err)...)
	}
}

func (c *connection) connect() error {
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
	c.mu.Lock()
	if isDone(c.engineDone) {
		root.endTyped()
	}
	c.namespaces.Set(rootNamespace, root)
	c.mu.Unlock()
	err := rootHandler.Connect(root)
	root.connectReturned(err)
	if err != nil {
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
func (c *connection) connected(err error) bool {
	if err != nil {
		root := c.namespace(rootNamespace)
		var onError func(Conn, error)
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
	c.connectRecord(rootNamespace, cmp.Or(err, c.rootRejected))
	if failed {
		c.reportOverflow(nil)
		c.finish()
	}
	return !failed
}

func (c *connection) nextID() uint64 {
	return c.id.Add(1)
}

// reportOverflow reports an overflow with conn, nil on a failed connect.
func (c *connection) reportOverflow(conn Conn) {
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
func (c *connection) unhandled(nsp string, err error) {
	c.log.Warn("socketio: unhandled error", nspAttr(nsp), "err", err)
}

// nspAttr is the nsp attribute of namespace nsp; the root namespace is "/".
func nspAttr(nsp string) slog.Attr {
	return slog.String("nsp", cmp.Or(nsp, aliasRootNamespace))
}

// errConn preserves error context for an unjoined namespace without granting it
// the room membership or sending capabilities of an accepted socket.
func (c *connection) errConn(nsp string) (nc *namespaceConn) {
	if nh := c.namespace(nsp); nh != nil && nh.ErrorHandler() != nil {
		if nc, _ = c.namespaces.Get(nsp); nc == nil {
			nc = newNamespaceConn(c, cmp.Or(nsp, aliasRootNamespace), nh.Adapter())
			nc.rejected, nc.roomsClosed = true, true
			nc.endTyped()
		}
	}
	return nc
}

// onError reports err of the packet being read (see report); a failure of the frame
// reader that the decoder returned is expected closure.
func (c *connection) onError(namespace string, err error) {
	c.report(namespace, err, c.reader.failure())
}

// report delivers err to OnError of namespace and waits for it, unless a close started.
// Without OnError, err is logged as unhandled unless it is the frame failure failed.
func (c *connection) report(namespace string, err, failed error) {
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
	c.startError()
	select {
	case c.errorChan <- msg:
		<-msg.done
	case <-c.closing:
	}
}

func (c *connection) namespace(nsp string) *namespaceHandler {
	handler, _ := c.handlers.Get(nsp)
	return handler
}

func (c *connection) EngineConn() engineio.Conn {
	return c.Conn
}

func (c *connection) Request() *http.Request {
	return c.request
}

// Start connects the root socket and starts its writer. The error worker starts
// only when a report needs it.
// The public server registers this connection before invoking Start.
func (c *connection) Start(resolve func(string, url.Values) *namespaceHandler) bool {
	c.resolveNamespace = resolve
	if !c.connected(c.connect()) {
		return false
	}
	go c.serveWrite()
	return true
}

// Read dispatches until the transport ends and completes its local close effects.
func (c *connection) Read() { c.serveRead(connectPacketHandler, disconnectPacketHandler) }

// Wait waits for local close and every disconnect claimed before it.
func (c *connection) Wait() {
	<-c.done
	c.disconnects.Wait()
	if c.engineObserverDone != nil {
		<-c.engineObserverDone
	}
}

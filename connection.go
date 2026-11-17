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
	eventImpl "github.com/sshaplygin/go-socket.io/internal/event"

	"github.com/sshaplygin/go-socket.io/parser"
)

// The outbound queue size (see ErrWriteBufferFull) and drain deadline (see Conn.Close).
const defaultWriteBufferSize, defaultDrainTimeout = 1024, time.Minute

// connection configuration, codecs and channel references are installed before
// workers start. The read path coordinates decoder/reader handoffs between its
// header worker and dispatcher; rootRejected is set before those workers start.
// The writer owns encoder/writer. Registries synchronize their own contents. Channel
// operations synchronize wakeups and completion; id uses atomic operations.
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
	drainQueue      []drainPacket // guarded by mu
	pendingWrites   int           // guarded by mu
	drainQueued     int           // guarded by mu
	controlQueued   int           // guarded by mu
	closeControls   int           // guarded by mu
	controlPending  int           // claimed namespace disconnects not yet enqueued; guarded by mu
	writeBufferSize int           // the outbound queue capacity
	errorChan       chan error
	errorOnce       sync.Once // starts serveError on the first report that needs it

	log          *slog.Logger
	rootRejected error
	state        connectionState // guarded by mu

	// mu guards state and close-state transitions and queue admission, including draining,
	// connecting, appSealed, overflow, pending, drainTimer and disconnectReason.
	// It serializes closing/seal/discard closure. finish closes done, either
	// after failed initial connection or on the read dispatcher after success.
	// drainTimeout is set before workers start.
	mu                           sync.Mutex
	closing, seal, discard, done chan struct{}
	draining, connecting         bool             // the first close is Close; until connected
	appSealed                    bool             // Close returned from its own callbacks
	overflow                     *namespaceConn   // the first close is its overflow, in this namespace
	overflowByPeer               bool             // refuse queued the packet that overflowed
	pending                      []*namespaceConn // OnDisconnect calls a library close owes
	drainTimer                   *time.Timer
	drainTimeout                 time.Duration  // tests shorten it
	disconnectReason             string         // guarded by mu; forced close unless read observes an engine close
	disconnects                  sync.WaitGroup // registered under mu before claiming a namespace
	disconnecting                int            // callbacks claimed but not completed; guarded by mu
	released                     chan struct{}  // closed when the server releases the connection
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
		released:         make(chan struct{}),
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
	err := rootHandler.Connect(root, root.enter)
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
			c.rejected(rootNamespace, err)
		}
		c.write(parser.Header{Type: parser.Error}, reflect.ValueOf(eventImpl.WireMessage(err)))
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
		switch {
		case onError != nil:
			onError(nil, err)
		case expected(err, c.writer.failed):
			// the session ended: OnDisconnect reports it
		case root == nil || root.Err() != nil: // see connect: no OnConnect ran
			c.unhandled(rootNamespace, err)
		default:
			c.rejected(rootNamespace, err)
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
	if c.overflowByPeer {
		// The packet answered a CONNECT to a namespace that the server does not have or
		// that refused it (see refuse): the client caused the overflow with the CONNECTs
		// it sent and by not reading.
		c.unhandledPeer(c.overflow.namespace, ErrWriteBufferFull)
		return
	}
	c.unhandled(c.overflow.namespace, ErrWriteBufferFull)
}

// unhandled logs err, which no OnError receives and which is not expected closure.
// A recovered panic is logged with its stack.
func (c *connection) unhandled(nsp string, err error) {
	var panicked *PanicError
	if errors.As(err, &panicked) {
		c.log.Warn("socketio: handler panic", nspAttr(nsp), "err", err, "stack", string(panicked.Stack))
		return
	}
	c.log.Warn("socketio: unhandled error", nspAttr(nsp), "err", err)
}

// unhandledPeer logs err, which no OnError receives, at DEBUG: the peer caused it with
// what it sent, so it may repeat it at will.
func (c *connection) unhandledPeer(nsp string, err error) {
	c.log.Debug("socketio: unhandled error", nspAttr(nsp), "err", err)
}

// rejected logs err, which connect middleware or OnConnect of nsp returned and which no
// OnError receives. A rejection is the application's answer to a CONNECT that any
// client may send again at will, and the ERROR packet carries it to the client, so it is
// logged at DEBUG (see unhandledPeer); a recovered panic is a fault of the application
// and is logged at WARN (see unhandled).
func (c *connection) rejected(nsp string, err error) {
	if eventImpl.IsPanic(err) {
		c.unhandled(nsp, err)
	} else {
		c.unhandledPeer(nsp, err)
	}
}

// notify delivers err of a callback on nc to the OnError of nh on this goroutine,
// or logs it as unhandled.
func (c *connection) notify(nh *namespaceHandler, nc *namespaceConn, err error) {
	if nh != nil {
		if onError := nh.ErrorHandler(); onError != nil {
			onError(nc, err)
			return
		}
	}
	c.unhandled(nc.namespace, err)
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

// onError reports err of the packet being read (see report). A failure of the engine
// connection's NextReader or of a frame reader, which the decoder returned, ends the
// session: OnDisconnect reports it and OnError does not.
func (c *connection) onError(namespace string, err error) {
	c.onErrorOr(namespace, err, c.unhandled)
}

// onPeerError is onError for a failure the peer caused with what it sent: a packet
// that does not decode, a payload of the wrong type or an ERROR packet. Without OnError
// it is logged at DEBUG (see unhandledPeer).
func (c *connection) onPeerError(namespace string, err error) {
	c.onErrorOr(namespace, err, c.unhandledPeer)
}

// onErrorOr is onError with unhandled in place of c.unhandled.
func (c *connection) onErrorOr(namespace string, err error, unhandled func(string, error)) {
	if !expected(err, c.reader.failure()) {
		c.report(namespace, err, unhandled)
	}
}

// report delivers err to OnError of namespace and waits for it, unless a close started.
// Without OnError, err is passed to unhandled. Callers report no transport failure: the
// end of the session reaches OnDisconnect only. After a close started, see
// reportAfterClose.
func (c *connection) report(namespace string, err error, unhandled func(string, error)) {
	if isDone(c.closing) {
		c.reportAfterClose(namespace, err)
		return
	}
	msg := newErrorMessage(namespace, err)
	if msg.conn = c.errConn(namespace); msg.conn == nil {
		unhandled(namespace, err)
		return
	}
	c.startError()
	select {
	case c.errorChan <- msg:
		<-msg.done
	case <-c.closing:
		c.reportAfterClose(namespace, err)
	}
}

// reportAfterClose handles err raised after the connection's close started. A
// recovered panic is reported on this goroutine, because the error worker may have
// stopped; any other failure is logged at DEBUG only.
func (c *connection) reportAfterClose(namespace string, err error) {
	if !eventImpl.IsPanic(err) {
		c.log.Debug("socketio: error after close", nspAttr(namespace), "err", err)
		return
	}
	if nc := c.errConn(namespace); nc != nil {
		c.notify(c.namespace(namespace), nc, err)
		return
	}
	c.unhandled(namespace, err)
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

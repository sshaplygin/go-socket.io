package connection

import (
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sshaplygin/go-socket.io/client/internal/namespace"
	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/parser"
)

// connLimits are the queue size and drain deadline of the connections the client manager creates.
type connLimits struct {
	writeBufferSize int
	drainTimeout    time.Duration
}

type conn struct {
	engineclient.Conn

	id         atomic.Uint64
	handlers   *namespace.Registry
	namespaces *namespaces

	encoder *parser.Encoder
	decoder *parser.Decoder
	reader  *frameReader // tells the decoder's transport failures apart
	writer  *queueWriter // tells the encoder's transport failures apart

	writeReady                              chan struct{}
	writeChan                               chan queuedPayload
	controls                                []controlPayload
	enqueued, started                       uint64
	owner                                   *clientState
	readTasks                               []func()
	readReady                               chan struct{}
	readStopped, writeStopped, errorStopped chan struct{}
	errorChan                               chan *errorMessage

	log *slog.Logger

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

func newConn(engineConn engineclient.Conn, handlers *namespace.Registry, limits connLimits, log *slog.Logger) *conn {
	c := &conn{
		log:              log,
		disconnectReason: "forced close",
		Conn:             engineConn,
		errorChan:        make(chan *errorMessage),
		writeChan:        make(chan queuedPayload, limits.writeBufferSize),
		writeReady:       make(chan struct{}, 1),
		handlers:         handlers,
		namespaces:       newNamespaces(),
		closing:          make(chan struct{}),
		seal:             make(chan struct{}),
		discard:          make(chan struct{}),
		done:             make(chan struct{}),
		readReady:        make(chan struct{}, 1),
		readStopped:      make(chan struct{}),
		writeStopped:     make(chan struct{}),
		errorStopped:     make(chan struct{}),
	}
	c.reader, c.writer = &frameReader{FrameReader: engineConn}, &queueWriter{c: c}
	c.decoder, c.encoder = parser.NewDecoder(c.reader), parser.NewEncoder(c.writer)
	c.drainTimeout, c.connecting = limits.drainTimeout, true
	return c
}

func (c *conn) nextID() uint64 {
	return c.id.Add(1)
}

func isDone(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (c *conn) namespace(nsp string) *namespace.Handler {
	handler, _ := c.handlers.Get(nsp)
	return handler
}

// EngineConn returns nil on a client connection.
func (c *conn) EngineConn() engineclient.Conn { return nil }

// Request returns nil on a client connection.
func (c *conn) Request() *http.Request { return nil }

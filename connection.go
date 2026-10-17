package socketio

import (
	"cmp"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/parser"
)

// The outbound queue size (see ErrWriteBufferFull) and drain deadline (see Conn.Close).
const defaultWriteBufferSize, defaultDrainTimeout = 64, time.Minute

// connLimits are the queue size and drain deadline of the connections a Server or Client creates.
type connLimits struct {
	writeBufferSize int
	drainTimeout    time.Duration
}

// newConnLimits reads opts.WriteBufferSize and opts.PingTimeout; nil options, 0 and
// negative values give the defaults.
func newConnLimits(opts *engineio.Options) connLimits {
	l := connLimits{defaultWriteBufferSize, defaultDrainTimeout}
	if opts != nil && opts.WriteBufferSize > 0 {
		l.writeBufferSize = opts.WriteBufferSize
	}
	if opts != nil && opts.PingTimeout > 0 {
		l.drainTimeout = opts.PingTimeout
	}
	return l
}

// Conn is a connection in go-socket.io
type Conn interface {
	// Close closes the connection and returns nil; only the first close, by Close or by the library,
	// has an effect. For every connected namespace it emits disconnecting while rooms are joined,
	// writes a DISCONNECT packet, leaves its rooms, and calls OnDisconnect with
	// "server namespace disconnect". It returns without waiting. A client-side
	// connection keeps the engine close behavior and "forced close" reason.
	// The packets queued until then, Emits from OnDisconnect included, are written in the background;
	// an Emit that finds the queue full, or comes later, is dropped without a report. Then, or
	// engineio.Options.PingTimeout (one minute if not positive) after Close started, the rest is
	// discarded and the engine.io connection is closed; Server.Count counts the session until then.
	// Incoming packets are no longer dispatched, and a read or write failure ends the drain at once.
	// The library closes a connection on a read, decode or dispatch error, a peer close, a ping
	// timeout or an overflow (see ErrWriteBufferFull), discarding the queue at once.
	// Other than an overflow, the error is reported to OnError before the close's effects run,
	// so Emits from OnError still queue. A middleware or OnConnect rejection sends ERROR and keeps
	// the engine connection open; it reports to that namespace's OnError, with a nil Conn for root.
	// Failures during a close are not reported. An encode failure reports OnError, sends an ERROR
	// packet with "encode error", and keeps the connection open.
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
	reader  *frameReader // tells the decoder's transport failures apart
	writer  *queueWriter // tells the encoder's transport failures apart

	initialConnect *parser.Payload
	writeReady     chan struct{}
	drainQueue     []drainPacket
	pendingWrites  int
	drainQueued    int
	writeChan      chan parser.Payload
	errorChan      chan error

	log          *slog.Logger
	rootRejected error
	boundary     bool // log the namespace connect and disconnect records (Server only)

	// mu orders the close state, whose steps close the channels, against queueing and registering.
	mu                           sync.Mutex
	closing, seal, discard, done chan struct{}
	draining, connecting         bool             // the first close is Close; until connected
	overflow                     *namespaceConn   // the first close is its overflow, in this namespace
	pending                      []*namespaceConn // OnDisconnect calls a library close owes
	drainTimer                   *time.Timer
	drainTimeout                 time.Duration  // tests shorten it
	disconnectReason             string         // guarded by mu; forced close unless read observes an engine close
	disconnects                  sync.WaitGroup // registered under mu before claiming a namespace
}

func newConn(engineConn engineio.Conn, handlers *namespaceHandlers, limits connLimits, log *slog.Logger) *conn {
	c := &conn{
		log:              log,
		disconnectReason: "forced close",
		Conn:             engineConn,
		errorChan:        make(chan error),
		writeChan:        make(chan parser.Payload, limits.writeBufferSize),
		writeReady:       make(chan struct{}, 1),
		handlers:         handlers,
		namespaces:       newNamespaces(),
		closing:          make(chan struct{}),
		seal:             make(chan struct{}),
		discard:          make(chan struct{}),
		done:             make(chan struct{}),
	}
	c.reader, c.writer = &frameReader{FrameReader: engineConn}, &queueWriter{c: c}
	c.decoder, c.encoder = parser.NewDecoder(c.reader), parser.NewEncoder(c.writer)
	c.drainTimeout, c.connecting = limits.drainTimeout, true
	return c
}

func (c *conn) Close() error {
	c.mu.Lock()
	if isDone(c.closing) {
		c.mu.Unlock()
		return nil
	}
	ncs := c.startClose()
	if c.boundary {
		c.disconnectReason = "server namespace disconnect"
	}
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
	c.disconnects.Add(len(ncs))
	return ncs
}

// stop starts a library close, or ends a drain.
func (c *conn) stop() { c.mu.Lock(); defer c.mu.Unlock(); c.stopLocked() }

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
	c.mu.Lock()
	reason := c.disconnectReason
	c.mu.Unlock()
	for _, nc := range ncs {
		if c.boundary && c.draining {
			nc.acceptPending()
			c.write(parser.Header{Type: parser.Disconnect, Namespace: fmtNS(nc.namespace)})
		}
		c.disconnectNamespace(nc, reason)
		if c.boundary {
			c.log.Debug("socketio: disconnect", nspAttr(nc.namespace), "reason", "connection close")
		}
	}
}

// disconnectNamespace exposes rooms to disconnecting, then removes them before
// OnDisconnect, matching the reference's two lifecycle notifications.
func (c *conn) disconnectNamespace(nc *namespaceConn, reason string) {
	defer c.disconnects.Done()
	nh := c.namespace(fmtNS(nc.namespace))
	if nh != nil {
		if _, err := nh.dispatchEvent(nc, "disconnecting", reflect.ValueOf(reason)); err != nil {
			if nh.onError != nil {
				nh.onError(nc, err)
			} else {
				c.unhandled(nc.namespace, err)
			}
		}
	}
	if nh != nil {
		nh.removeSocket(nc)
	}
	nc.LeaveAll()
	if nh != nil && nh.onDisconnect != nil {
		nh.onDisconnect(nc, reason)
	}
}

// finishRead obtains the final engine cause on the read goroutine, after Decode
// returned and before finish invokes any OnDisconnect handlers.
func (c *conn) finishRead() {
	reason := "forced close"
	var closed *engineio.CloseError
	failed := c.reader.failed
	if failed != nil && !errors.As(failed, &closed) {
		// An acquired frame reader keeps its own transport error. Closing the
		// engine preserves the cause that reader recorded; the next reader then
		// exposes it through CloseError without changing the frame's error.
		_ = c.Conn.Close()
		_, reader, err := c.Conn.NextReader()
		if reader != nil {
			_ = reader.Close()
		}
		failed = err
	}
	if errors.As(failed, &closed) {
		switch closed.Reason {
		case engineio.ReasonTransportClose, engineio.ReasonPingTimeout, engineio.ReasonTransportError:
			reason = string(closed.Reason)
		}
	}
	c.mu.Lock()
	if !c.draining {
		c.disconnectReason = reason
	}
	c.mu.Unlock()
	c.finish()
}

// connectRecord logs the namespace connect record of nsp, with err unless it is nil.
func (c *conn) connectRecord(nsp string, err error) {
	if args := []any{nspAttr(nsp)}; err == nil {
		c.log.Debug("socketio: namespace connect", args...)
	} else {
		c.log.Debug("socketio: namespace connect", append(args, "err", err)...)
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

func (c *conn) claim(nsp string, expected ...*namespaceConn) (*namespaceConn, bool) {
	return c.claimNamespace(nsp, false, expected...)
}

func (c *conn) claimDisconnect(nsp string, expected ...*namespaceConn) (*namespaceConn, bool) {
	return c.claimNamespace(nsp, true, expected...)
}

func (c *conn) claimNamespace(nsp string, disconnect bool, expected ...*namespaceConn) (*namespaceConn, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nc, ok := c.namespaces.Get(nsp)
	if len(expected) > 0 && nc != expected[0] {
		return nil, false
	}
	c.namespaces.Delete(nsp)
	if ok && disconnect {
		c.disconnects.Add(1)
	}
	return nc, ok
}

func (c *conn) connect() error {
	rootHandler, ok := c.handlers.Get(rootNamespace)
	if !ok {
		return errUnavailableRootHandler
	}
	if rootHandler.err != nil { // its Redis broadcast could not be created
		return rootHandler.err
	}

	root := newNamespaceConn(c, aliasRootNamespace, rootHandler.broadcast)
	root.beginConnect()
	root.SetContext(c.Conn.Context())
	c.namespaces.Set(rootNamespace, root)
	if err := rootHandler.connect(root); err != nil {
		if isDone(c.closing) {
			return err
		}
		c.claim(rootNamespace)
		root.reject()
		c.rootRejected = err
		if rootHandler.onError != nil {
			rootHandler.onError(nil, err)
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
func (c *conn) connected(err error) bool {
	if root := c.namespace(rootNamespace); err != nil && root != nil && root.onError != nil {
		root.onError(nil, err)
	} else if err != nil && !expected(err, c.writer.failed) {
		c.unhandled(rootNamespace, err)
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

func (c *conn) nextID() uint64 {
	c.id++

	return c.id
}

func (c *conn) write(header parser.Header, args ...reflect.Value) { c.queue(header, false, args...) }

func (c *conn) writeReserved(header parser.Header, args ...reflect.Value) {
	c.queue(header, true, args...)
}

func (c *conn) queue(header parser.Header, reserved bool, args ...reflect.Value) {
	data := make([]interface{}, len(args))

	for i := range data {
		if args[i].IsValid() {
			data[i] = args[i].Interface()
		}
	}

	pkg := parser.Payload{
		Header: header,
		Data:   data,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if reserved {
		c.pendingWrites--
	}
	switch {
	case isDone(c.closing) && (!c.draining || isDone(c.seal) || isDone(c.discard)):
	case !c.draining && c.boundary && c.connecting && header.Type == parser.Connect && header.Namespace == rootNamespace:
		c.initialConnect = &pkg
		c.notifyWrite()
	case c.draining:
		control := header.Type == parser.Connect || header.Type == parser.Disconnect
		if control || reserved || len(c.writeChan)+c.drainQueued+c.pendingWrites < cap(c.writeChan) {
			c.drainQueue = append(c.drainQueue, drainPacket{pkg, control})
			if !control {
				c.drainQueued++
			}
			c.notifyWrite()
		}
	case len(c.writeChan) < cap(c.writeChan) && (reserved || len(c.writeChan)+c.pendingWrites < cap(c.writeChan)):
		c.writeChan <- pkg
		c.notifyWrite()
	case !isDone(c.closing):
		c.overflowWrite(header)
	}

}

// overflowWrite starts a library close for a full outbound queue; mu is held.
func (c *conn) overflowWrite(header parser.Header) {
	if isDone(c.closing) {
		return
	}
	if c.overflow = c.errConn(header.Namespace); c.overflow == nil {
		c.overflow = &namespaceConn{conn: c, namespace: header.Namespace}
	}
	if c.pending = c.startClose(); !c.connecting {
		c.stopLocked()
	}

}

// reportOverflow reports an overflow with conn, nil on a failed connect as in v1.4.
func (c *conn) reportOverflow(conn Conn) {
	if c.overflow == nil {
		return
	}
	if nh := c.namespace(fmtNS(c.overflow.namespace)); nh != nil && nh.onError != nil {
		nh.onError(conn, ErrWriteBufferFull)
	} else {
		c.unhandled(c.overflow.namespace, ErrWriteBufferFull)
	}
}

// unhandled logs err, which no OnError receives and which is not expected closure.
func (c *conn) unhandled(nsp string, err error) {
	c.log.Warn("socketio: unhandled error", nspAttr(nsp), "err", err)
}

// nspAttr is the nsp attribute of namespace nsp; the root namespace is "/".
func nspAttr(nsp string) slog.Attr {
	return slog.String("nsp", cmp.Or(nsp, aliasRootNamespace))
}

// errConn returns the Conn to report an error of nsp with (new if nsp was disconnected), or nil.
func (c *conn) errConn(nsp string) (nc *namespaceConn) {
	if nh := c.namespace(nsp); nh != nil && nh.onError != nil {
		if nc, _ = c.namespaces.Get(nsp); nc == nil {
			nc = newNamespaceConn(c, cmp.Or(nsp, aliasRootNamespace), nh.broadcast)
		}
	}
	return nc
}

// queueWriter starts no packet (its TEXT frame) once the queue is discarded. failed is the
// first failure of NextWriter or of the frame writer it returned since the last NextWriter.
type queueWriter struct {
	c      *conn
	failed error
}

func (w *queueWriter) NextWriter(ft session.FrameType) (io.WriteCloser, error) {
	if ft == session.TEXT && isDone(w.c.discard) {
		w.failed = io.EOF
		return nil, io.EOF
	}
	fw, err := w.c.Conn.NextWriter(ft)
	if w.failed = err; err != nil {
		return nil, err
	}
	return frameIO{w: fw, c: fw, failed: &w.failed}, nil
}

// frameReader is queueWriter's counterpart for the decoder.
type frameReader struct {
	parser.FrameReader
	failed error
}

func (r *frameReader) NextReader() (session.FrameType, io.ReadCloser, error) {
	ft, fr, err := r.FrameReader.NextReader()
	if r.failed = err; err != nil {
		return ft, fr, err
	}
	return ft, frameIO{r: fr, c: fr, failed: &r.failed}, nil
}

// frameIO is a frame reader or writer that records its first failure in *failed; io.EOF
// from Read ends the frame. It returns the errors unchanged.
type frameIO struct {
	r      io.Reader
	w      io.Writer
	c      io.Closer
	failed *error
}

func (f frameIO) mark(err error) error {
	*f.failed = cmp.Or(*f.failed, err)
	return err
}

func (f frameIO) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if err == io.EOF {
		return n, err
	}
	return n, f.mark(err)
}

func (f frameIO) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	return n, f.mark(err)
}

func (f frameIO) Close() error { return f.mark(f.c.Close()) }

// expected tells whether err is the frame failure failed, which the decoder or encoder
// returned: an error that follows a frame failure the parser ignored is not.
func expected(err, failed error) bool { return failed != nil && errors.Is(err, failed) }

func isDone(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// onError reports err of the packet being read (see report); a failure of the frame
// reader that the decoder returned is expected closure.
func (c *conn) onError(namespace string, err error) {
	c.report(namespace, err, c.reader.failed)
}

// report delivers err to OnError of namespace and waits for it, unless a close started.
// Without OnError, err is logged as unhandled unless it is the frame failure failed.
func (c *conn) report(namespace string, err, failed error) {
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

func (c *conn) namespace(nsp string) *namespaceHandler {
	handler, _ := c.handlers.Get(nsp)
	return handler
}

type drainPacket struct {
	payload parser.Payload
	control bool
}

// notifyWrite is called under mu; the signal is only a hint to recheck the queues.
func (c *conn) notifyWrite() {
	select {
	case c.writeReady <- struct{}{}:
	default:
	}
}

func (c *conn) nextQueued() (parser.Payload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.initialConnect != nil {
		packet := *c.initialConnect
		c.initialConnect = nil
		return packet, true
	}
	if len(c.writeChan) > 0 {
		return <-c.writeChan, true
	}
	if len(c.drainQueue) > 0 {
		packet := c.drainQueue[0]
		c.drainQueue[0] = drainPacket{}
		c.drainQueue = c.drainQueue[1:]
		if !packet.control {
			c.drainQueued--
		}
		return packet.payload, true
	}
	return parser.Payload{}, false
}

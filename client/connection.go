package client

import (
	"cmp"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/parser"
)

// The outbound queue size (see ErrWriteBufferFull) and drain deadline (see Conn.Close).
const defaultWriteBufferSize, defaultDrainTimeout = 64, time.Minute

// connLimits are the queue size and drain deadline of the connections a Server or Client creates.
type connLimits struct {
	writeBufferSize int
	drainTimeout    time.Duration
}

// newConnLimits reads opts.WriteBufferSize and opts.CloseTimeout; nil options, 0 and
// negative values give the defaults.
func newConnLimits(opts *Options) connLimits {
	l := connLimits{defaultWriteBufferSize, defaultDrainTimeout}
	if opts != nil && opts.WriteBufferSize > 0 {
		l.writeBufferSize = opts.WriteBufferSize
	}
	if opts != nil && opts.CloseTimeout > 0 {
		l.drainTimeout = opts.CloseTimeout
	}
	return l
}

type conn struct {
	engineclient.Conn

	id         atomic.Uint64
	handlers   *namespaceHandlers
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

func newConn(engineConn engineclient.Conn, handlers *namespaceHandlers, limits connLimits, log *slog.Logger) *conn {
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
	c.appSealed = true
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
	c.disconnecting += len(ncs)
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
		c.disconnectNamespace(nc, reason)

	}
}

// disconnectNamespace exposes rooms to disconnecting, then removes them before
// OnDisconnect, matching the reference's two lifecycle notifications.
func (c *conn) disconnectNamespace(nc *namespaceConn, reason string) {
	c.mu.Lock()
	draining := c.draining
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.disconnecting--
		c.mu.Unlock()
		c.disconnects.Done()
	}()
	nh := c.namespace(fmtNS(nc.namespace))
	if c.owner != nil && !draining {
		c.owner.detach(nc)
	}
	if nh != nil {
		if _, err := nh.dispatchEvent(nc, "disconnecting", reflect.ValueOf(reason)); err != nil {
			if f := nh.errorHandler(); f != nil {
				f(nc, err)
			} else {
				c.unhandled(nc.namespace, err)
			}
		}
	}
	nc.LeaveAll()
	if nh != nil {
		if f := nh.disconnectHandler(); f != nil {
			f(nc, reason)
		}
	}
	if c.owner != nil && draining {
		c.owner.detach(nc)
	}
}

// finishRead obtains the final engine cause on the read goroutine, after Decode
// returned and before finish invokes any OnDisconnect handlers.
func (c *conn) finishRead() {
	reason := "forced close"
	var closed *engineclient.CloseError
	failed := c.reader.failed
	if failed != nil && !errors.As(failed, &closed) {
		// Dispatch has ended and its error was already reported. Start the
		// close before the engine shuts down, so the writer cannot report that
		// shutdown as a second failure while the reader obtains its cause.
		c.stop()
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
		case engineclient.ReasonTransportClose, engineclient.ReasonPingTimeout, engineclient.ReasonTransportError:
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

func (c *conn) register(nsp string, nc *namespaceConn) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !isDone(c.closing) {
		c.namespaces.Set(nsp, nc)
	}
	return !isDone(c.closing)
}

func (c *conn) claim(nsp string, expected ...*namespaceConn) (*namespaceConn, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nc, ok := c.namespaces.Get(nsp)
	if len(expected) > 0 && nc != expected[0] {
		return nil, false
	}
	c.namespaces.Delete(nsp)
	return nc, ok
}

// connected ends the connect; a failure reports err first, so Emits from OnError queue, then closes.
func (c *conn) connected(err error) bool {
	var onError func(Conn, error)
	if root := c.namespace(rootNamespace); root != nil {
		onError = root.errorHandler()
	}
	if err != nil && onError != nil {
		onError(nil, err)
	} else if err != nil && !expected(err, c.writer.failed) {
		c.unhandled(rootNamespace, err)
	}
	c.mu.Lock()
	failed := err != nil || isDone(c.closing) && !c.draining
	if c.connecting = false; failed && !isDone(c.closing) {
		c.pending = c.startClose()
	}
	c.mu.Unlock()
	if failed {
		c.reportOverflow(nil)
		c.finish()
	}
	return !failed
}

func (c *conn) nextID() uint64 {
	return c.id.Add(1)
}

func (c *conn) write(header parser.Header, args ...reflect.Value) {
	c.writeFlags(header, false, args...)
}

func (c *conn) writeFlags(header parser.Header, noCompress bool, args ...reflect.Value) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queueFlagsLocked(header, noCompress, args...)
}

// queueFlagsLocked admits a packet with the queue budget and close state locked.
func (c *conn) queueFlagsLocked(header parser.Header, noCompress bool, args ...reflect.Value) {
	data := make([]interface{}, len(args))
	for i := range data {
		if args[i].IsValid() {
			data[i] = args[i].Interface()
		}
	}
	if isDone(c.closing) && (!c.draining || c.appSealed || isDone(c.discard)) {
		return
	}
	if len(c.writeChan) < cap(c.writeChan) {
		c.enqueued++
		c.writeChan <- queuedPayload{payload: outboundPayload{Payload: parser.Payload{Header: header, Data: data}, noCompress: noCompress}, sequence: c.enqueued}
		c.notifyWrite()
	} else if !isDone(c.closing) {
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
	if nh := c.namespace(fmtNS(c.overflow.namespace)); nh != nil {
		if f := nh.errorHandler(); f != nil {
			f(conn, ErrWriteBufferFull)
			return
		}
	}
	c.unhandled(c.overflow.namespace, ErrWriteBufferFull)
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
	if nh := c.namespace(nsp); nh != nil && nh.errorHandler() != nil {
		if nc, _ = c.namespaces.Get(nsp); nc == nil {
			nc = newNamespaceConn(c, cmp.Or(nsp, aliasRootNamespace), nh.broadcast)
		}
	}
	return nc
}

// queueWriter starts no packet (its TEXT frame) once the queue is discarded. failed is the
// first failure of NextWriter or of the frame writer it returned since the last NextWriter.
type queueWriter struct {
	c          *conn
	failed     error
	noCompress bool
}

func (w *queueWriter) NextWriter(ft parser.FrameType) (io.WriteCloser, error) {
	if ft == parser.Text && isDone(w.c.discard) {
		w.failed = io.EOF
		return nil, io.EOF
	}
	var fw io.WriteCloser
	var err error
	if compressed, ok := w.c.Conn.(engineclient.CompressWriter); ok {
		fw, err = compressed.NextWriterCompress(eioparser.FrameType(ft), !w.noCompress)
	} else {
		fw, err = w.c.Conn.NextWriter(eioparser.FrameType(ft))
	}
	if w.failed = err; err != nil {
		return nil, err
	}
	return frameIO{w: fw, c: fw, failed: &w.failed}, nil
}

// frameReader is queueWriter's counterpart for the decoder.
type frameReader struct {
	FrameReader interface {
		NextReader() (eioparser.FrameType, io.ReadCloser, error)
	}
	failed error
}

func (r *frameReader) NextReader() (parser.FrameType, io.ReadCloser, error) {
	ft, fr, err := r.FrameReader.NextReader()
	if r.failed = err; err != nil {
		return parser.FrameType(ft), fr, err
	}
	return parser.FrameType(ft), frameIO{r: fr, c: fr, failed: &r.failed}, nil
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

// notifyWrite is called under mu; the signal is only a hint to recheck the queues.
func (c *conn) notifyWrite() {
	select {
	case c.writeReady <- struct{}{}:
	default:
	}
}

func (c *conn) nextQueued() (outboundPayload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.controls) > 0 && c.controls[0].after <= c.started {
		control := c.controls[0]
		c.controls = c.controls[1:]
		return outboundPayload{Payload: control.payload}, true
	}
	if len(c.writeChan) > 0 {
		queued := <-c.writeChan
		c.started = queued.sequence
		return queued.payload, true
	}
	return outboundPayload{}, false
}

type queuedPayload struct {
	payload  outboundPayload
	sequence uint64
}

type controlPayload struct {
	payload parser.Payload
	after   uint64
}

// Lifecycle packets retain their position among application writes without
// consuming an application slot. At most one CONNECT/DISCONNECT is owed per socket.
func (c *conn) writeControl(header parser.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if isDone(c.discard) || c.appSealed {
		return
	}
	c.controls = append(c.controls, controlPayload{payload: parser.Payload{Header: header}, after: c.enqueued})
	c.notifyWrite()
}

func (c *conn) queueReadTask(task func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if isDone(c.closing) {
		return
	}
	c.readTasks = append(c.readTasks, task)
	select {
	case c.readReady <- struct{}{}:
	default:
	}
}

func (c *conn) runReadTasks() {
	c.mu.Lock()
	tasks := c.readTasks
	c.readTasks = nil
	c.mu.Unlock()
	for _, task := range tasks {
		task()
	}
}

// EngineConn returns nil on a client connection.
func (c *conn) EngineConn() engineclient.Conn { return nil }

// Request returns nil on a client connection.
func (c *conn) Request() *http.Request { return nil }

// outboundPayload keeps transport flags out of the shared parser codec.
type outboundPayload struct {
	parser.Payload
	noCompress bool
}

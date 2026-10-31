package polling

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// batchOffer proposes another packet for an existing response. A different
// compression preference declines it while keeping NextWriter pending for the
// next GET; no frame has been acquired or encoded at that point.
type batchOffer struct {
	writer   *flushWriter
	accepted chan bool
}

type readArg struct {
	r             io.Reader
	supportBinary bool
}

// payload owns polling feed/flush lifetimes, deadlines and pause state. Wire
// encoding and decoding belong to engineio/parser.
type payload struct {
	close     chan struct{}
	closeOnce sync.Once
	err       atomic.Value

	pauser *pauser

	readerChan   chan readArg
	feeding      int32
	readError    chan error
	readDeadline atomic.Value
	feedTimer    operationTimer // owned by the FeedIn that holds feeding
	decoder      *eioparser.PayloadDecoder

	writerChan      chan io.Writer
	batchWriterChan chan batchOffer
	compress        atomic.Bool
	answerPoll      chan struct{}
	flushing        int32
	writeError      chan error
	writeDeadline   atomic.Value
	flushTimer      operationTimer // owned by the flushOut that holds flushing
	encoder         *eioparser.PayloadEncoder
	batch           bool // server GETs coalesce complete packets; client POSTs keep their lease
	batchClock      *batchClock
}

// newPayload returns a new payload.
func newPayload(supportBinary bool) *payload {
	ret := &payload{
		close:           make(chan struct{}),
		pauser:          newPauser(),
		readerChan:      make(chan readArg),
		readError:       make(chan error),
		writerChan:      make(chan io.Writer),
		batchWriterChan: make(chan batchOffer),
		answerPoll:      make(chan struct{}),
		writeError:      make(chan error),
	}
	ret.readDeadline.Store(time.Time{})
	ret.decoder = eioparser.NewPayloadDecoder(ret)
	ret.writeDeadline.Store(time.Time{})
	ret.encoder = eioparser.NewPayloadEncoder(ret, eioparser.WithBinarySupport(supportBinary))
	return ret
}

// flushWriter is the io.Writer that FlushOut hands to the session writer. detach waits
// for a Write in progress and fails every later one with errDetached, so once FlushOut
// has returned nothing is written to, or still running against, the caller's writer.
type flushWriter struct {
	mu       sync.Mutex
	w        io.Writer
	gone     bool
	bytes    int
	compress bool
}

func (f *flushWriter) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone {
		return 0, errDetached
	}
	n, err := f.w.Write(b)
	f.bytes += n
	return n, err
}

func (f *flushWriter) size() int { f.mu.Lock(); defer f.mu.Unlock(); return f.bytes }

func (f *flushWriter) detach() {
	f.mu.Lock()
	f.gone = true
	f.mu.Unlock()
}

// FeedIn feeds in a new reader for NextReader.
// Multi-FeedIn needs be called sync.
//
// If Close called when FeedIn, it returns io.EOF.
// If have Pause-ed when FeedIn, it returns ErrPaused.
// If NextReader has timeout, it returns ErrTimeout.
// If read error while FeedIn, it returns read error.
func (p *payload) FeedIn(r io.Reader, supportBinary bool) error {
	if err := p.acquireFeed(); err != nil {
		return err
	}
	return p.feed(r, supportBinary)
}

// acquireFeed admits one request body at a time, before any of it is read;
// feed releases it. Every error is immediate: closed, overlapping or paused.
func (p *payload) acquireFeed() error {
	select {
	case <-p.close:
		return p.load()
	default:
	}

	if !atomic.CompareAndSwapInt32(&p.feeding, 0, 1) {
		return newOpError("read", errOverlap)
	}

	if ok := p.pauser.Working(); !ok {
		atomic.StoreInt32(&p.feeding, 0)
		return newOpError("payload", errPaused)
	}
	return nil
}

// feed hands an acquired body to the decoder and waits for it to finish.
func (p *payload) feed(r io.Reader, supportBinary bool) error {
	defer atomic.StoreInt32(&p.feeding, 0)
	defer p.pauser.Done()

	err := p.offerFeed(readArg{r: r, supportBinary: supportBinary})
	if err == nil {
		err = p.awaitFeed()
	}
	// Not deferred, to keep this frame small; after a panic the next owner's
	// until stops the timer.
	p.feedTimer.stop()
	return err
}

// offerFeed hands one request body to the decoder and returns nil once it is
// taken; every failure it returns is non-nil. offerFeed and awaitFeed keep
// FeedIn's blocking selects in their own small frames: under Go 1.23+ timers,
// blocking on the deadline channel adds the timer to the runtime heap on the
// caller's stack, and doing that under FeedIn's frame made a goroutine that
// had just started grow its stack.
func (p *payload) offerFeed(arg readArg) error {
	for {
		after, ok := p.feedTimer.until(p.readDeadline.Load().(time.Time))
		if !ok {
			return p.Store("read", errTimeout)
		}
		select {
		case <-p.close:
			return p.load()
		case <-after:
			// it may changed during wait, need check again
			continue
		case p.readerChan <- arg:
			return nil
		}
	}
}

// awaitFeed waits for the decoder to finish the request body.
func (p *payload) awaitFeed() error {
	for {
		after, ok := p.feedTimer.until(p.readDeadline.Load().(time.Time))
		if !ok {
			return p.Store("read", errTimeout)
		}
		select {
		case <-after:
			// it may changed during wait, need check again
			continue
		case err := <-p.readError:
			return p.Store("read", err)
		case <-p.close:
			return p.load()
		}
	}
}

// FlushOut write data from NextWriter.
// FlushOut needs be called sync.
//
// If Close called when Flushout,  it return io.EOF.
// If Pause called when Flushout, it flushs out a NOOP message and return
// nil.
// If NextWriter has timeout, it returns ErrTimeout.
// If write error while FlushOut, it returns write error.
func (p *payload) FlushOut(w io.Writer) error {
	return p.flushOut(context.Background(), w)
}

func (p *payload) cancelPoll(ctx context.Context) error {
	err := p.Store("write", ctx.Err())
	_ = p.Close()
	return err
}

func (p *payload) flushOut(ctx context.Context, w io.Writer) error {
	select {
	case <-ctx.Done():
		return p.cancelPoll(ctx)
	case <-p.close:
		return p.load()
	default:
	}

	if !atomic.CompareAndSwapInt32(&p.flushing, 0, 1) {
		return newOpError("write", errOverlap)
	}
	defer atomic.StoreInt32(&p.flushing, 0)
	timeout := &p.flushTimer
	defer timeout.stop()

	if ok := p.pauser.Working(); !ok {
		_, err := w.Write(p.encoder.NOOP())
		return err
	}
	defer p.pauser.Done()

	// FlushOut may return, on a timeout or Close, while the session writer is still
	// writing; detach makes that return wait for the Write in progress.
	fw := &flushWriter{w: w}
	defer fw.detach()

	packets := 0
	var batchEnd time.Time
	for {
		after, ok := timeout.until(p.writeDeadline.Load().(time.Time))
		if !ok {
			return p.Store("write", errTimeout)
		}
		select {
		case <-ctx.Done():
			return p.cancelPoll(ctx)
		case <-p.close:
			return p.load()

		case <-after:
			continue

		case <-p.pauser.PausingTrigger():
			_, err := w.Write(p.encoder.NOOP())
			return err
		case <-p.answerPoll:
			_, err := w.Write(p.encoder.NOOP())
			return err

		case p.writerChan <- fw:
		}
		break
	}

	for {
		after, ok := timeout.until(p.writeDeadline.Load().(time.Time))
		if !ok {
			return p.Store("write", errTimeout)
		}
		select {
		case <-ctx.Done():
			return p.cancelPoll(ctx)
		case <-p.close:
			return p.load()
		case <-after:
			// it may changed during wait, need check again
		case err := <-p.writeError:
			if err != nil || !p.batch {
				return p.Store("write", err)
			}
			packets++
			if batchEnd.IsZero() {
				batchEnd = p.batchNow().Add(5 * time.Millisecond)
			}
			if packets >= 64 || fw.size() >= 256*1024 {
				return nil
			}
			idle := min(time.Millisecond, p.batchUntil(batchEnd))
			if idle <= 0 {
				return nil
			}
			offer := batchOffer{writer: fw, accepted: make(chan bool, 1)}
			timer := p.newBatchTimer(idle)
			select {
			case <-ctx.Done():
				timer.Stop()
				return p.cancelPoll(ctx)
			case <-p.close:
				timer.Stop()
				return p.load()
			case <-p.pauser.PausingTrigger():
				timer.Stop()
				return nil
			case <-timer.C:
				return nil
			case p.batchWriterChan <- offer:
				timer.Stop()
				select {
				case <-ctx.Done():
					return p.cancelPoll(ctx)
				case <-p.close:
					return p.load()
				case accepted := <-offer.accepted:
					if !accepted {
						return nil
					}
				}
			}
		}
	}
}

// AnswerPoll answers a pending FlushOut with NOOP if no session writer has taken
// it. It returns false immediately when no such poll waits, or while paused or closed.
func (p *payload) AnswerPoll() bool {
	select {
	case <-p.close:
		return false
	default:
	}
	if !p.pauser.Working() {
		return false
	}
	defer p.pauser.Done()
	select {
	case p.answerPoll <- struct{}{}:
		return true
	default:
		return false
	}
}

// NextReader returns a reader for next frame.
// NextReader and SetReadDeadline needs be called sync.
//
// If Close called when NextReader,  it return io.EOF.
// Pause doesn't effect to NextReader. NextReader should wait till resumed
// and next FeedIn.
func (p *payload) NextReader() (eioparser.FrameType, eioparser.PacketType, io.ReadCloser, error) {
	ft, pt, r, err := p.decoder.NextReader()
	return ft, pt, r, err
}

// SetReadDeadline sets next reader deadline.
// NextReader and SetReadDeadline needs be called sync.
// NextReader will wait a FeedIn call, then it returns ReadCloser which
// decodes packet from FeedIn's Reader.
//
// If Close called when SetReadDeadline,  it return io.EOF.
// If beyond the time set by SetReadDeadline, it returns ErrTimeout.
// Pause doesn't effect to SetReadDeadline.
func (p *payload) SetReadDeadline(t time.Time) error {
	p.readDeadline.Store(t)
	return nil
}

// NextWriter returns a writer for next frame.
// NextWriter and SetWriterDeadline needs be called sync.
// NextWriter will wait a FlushOut call, then it returns WriteCloser which
// encode package to FlushOut's Writer.
//
// If Close called when NextWriter,  it returns io.EOF.
// If beyond the time set by SetWriteDeadline, it returns ErrTimeout.
// If Pause called when NextWriter, it returns ErrPaused.
func (p *payload) NextWriter(ft eioparser.FrameType, pt eioparser.PacketType) (io.WriteCloser, error) {
	return p.encoder.NextWriter(ft, pt)
}

// SetWriteDeadline sets next writer deadline.
// NextWriter and SetWriteDeadline needs be called sync.
//
// If Close called when SetWriteDeadline,  it return io.EOF.
// Pause doesn't effect to SetWriteDeadline.
func (p *payload) SetWriteDeadline(t time.Time) error {
	p.writeDeadline.Store(t)
	return nil
}

// Pause pauses the payload. It will wait all reader and writer closed which
// created from NextReader or NextWriter.
// It can call in multi-goroutine.
func (p *payload) Pause() {
	p.pauser.Pause()
}

// Resume resumes the payload.
// It can call in multi-goroutine.
func (p *payload) Resume() {
	p.pauser.Resume()
}

// Close closes the payload.
// It can call in multi-goroutine.
func (p *payload) Close() error {
	p.closeOnce.Do(func() {
		close(p.close)
	})
	return nil
}

// Store stores a error in payload, and block all other request.
func (p *payload) Store(op string, err error) error {
	old := p.err.Load()
	if old == nil {
		if err == io.EOF || err == nil {
			return err
		}
		op := newOpError(op, err)
		p.err.Store(op)
		return op
	}
	return old.(error)
}

// operationTimer is the deadline timer of one blocking call. FeedIn and
// flushOut use the payload's feedTimer and flushTimer: the feeding and flushing
// flags admit one call at a time, and the call stops its timer before it clears
// the flag. Each Acquire/Release of the payload reader or writer has a timer of
// its own, because it can wait on the same deadline as a running feed or flush.
// until re-arms the timer each time the call re-reads its deadline, and every
// return after the first until stops it.
//
// Stopping matters under pre-Go 1.23 timer semantics (main module go line
// below 1.23, an older toolchain, or GODEBUG=asynctimerchan=1): an unstopped
// timer stays live until it fires, about 45 s for polling deadlines. In that
// mode a timer that fired before Stop can leave a value in C, and stop drains
// it. A firing still in flight when Stop returns can land after the next
// Reset, so a call may see a spurious tick; every deadline case only re-reads
// the deadline, so such a tick costs one extra loop and cannot shorten it.
// Under Go 1.23+ semantics Stop and Reset discard pending values and the
// drain finds nothing.
type operationTimer struct{ timer *time.Timer }

func (t *operationTimer) stop() {
	if t.timer != nil && !t.timer.Stop() {
		select {
		case <-t.timer.C:
		default:
		}
	}
}

func (t *operationTimer) until(deadline time.Time) (<-chan time.Time, bool) {
	t.stop()
	if deadline.IsZero() {
		return nil, true
	}
	wait := time.Until(deadline)
	if wait <= 0 {
		return nil, false
	}
	if t.timer == nil {
		t.timer = time.NewTimer(wait)
	} else {
		t.timer.Reset(wait)
	}
	return t.timer.C, true
}

func (p *payload) AcquirePayloadReader() (io.Reader, bool, error) {
	var timeout operationTimer
	defer timeout.stop()
	select {
	case <-p.close:
		return nil, false, p.load()
	default:
	}

	if ok := p.pauser.Working(); !ok {
		return nil, false, newOpError("payload", errPaused)
	}
	p.pauser.Done()

	for {
		after, ok := timeout.until(p.readDeadline.Load().(time.Time))
		if !ok {
			return nil, false, p.Store("read", errTimeout)
		}
		select {
		case <-p.close:
			return nil, false, p.load()
		case <-p.pauser.PausedTrigger():
			return nil, false, newOpError("payload", errPaused)
		case <-after:
			continue
		case arg := <-p.readerChan:
			return arg.r, arg.supportBinary, nil
		}
	}
}

func (p *payload) ReleasePayloadReader(err error) error {
	var timeout operationTimer
	defer timeout.stop()
	select {
	case <-p.close:
		return p.load()
	default:
	}
	for {
		after, ok := timeout.until(p.readDeadline.Load().(time.Time))
		if !ok {
			return p.Store("read", errTimeout)
		}
		select {
		case <-p.close:
			return p.load()
		case <-after:
			continue
		case p.readError <- err:
		}
		return nil
	}
}

func (p *payload) AcquirePayloadWriter() (io.Writer, error) {
	var timeout operationTimer
	defer timeout.stop()
	select {
	case <-p.close:
		return nil, p.load()
	default:
	}

	if ok := p.pauser.Working(); !ok {
		return nil, newOpError("payload", errPaused)
	}
	p.pauser.Done()

	for {
		after, ok := timeout.until(p.writeDeadline.Load().(time.Time))
		if !ok {
			return nil, p.Store("write", errTimeout)
		}
		select {
		case <-p.close:
			return nil, p.load()
		case <-p.pauser.PausedTrigger():
			return nil, newOpError("payload", errPaused)
		case <-after:
			continue
		case w := <-p.writerChan:
			if fw, ok := w.(*flushWriter); ok {
				fw.compress = p.compress.Load()
			}
			return w, nil
		case offer := <-p.batchWriterChan:
			accepted := offer.writer.compress == p.compress.Load()
			offer.accepted <- accepted
			if accepted {
				return offer.writer, nil
			}
		}
	}
}

func (p *payload) ReleasePayloadWriter(err error) error {
	var timeout operationTimer
	defer timeout.stop()
	if errors.Is(err, errDetached) {
		// FlushOut has returned; nobody waits for this result. The error it
		// stored, such as an expired deadline, is why the writer was detached.
		if stored := p.err.Load(); stored != nil {
			return stored.(error)
		}
		return err
	}
	select {
	case <-p.close:
		return p.load()
	default:
	}
	for {
		after, ok := timeout.until(p.writeDeadline.Load().(time.Time))
		if !ok {
			return p.Store("write", errTimeout)
		}
		ret := p.Store("write", err)
		select {
		case <-p.close:
			return p.load()
		case <-after:
			continue
		case p.writeError <- err:
			return ret
		}
	}
}

func (p *payload) load() error {
	ret := p.err.Load()
	if ret == nil {
		return io.EOF
	}
	return ret.(error)
}

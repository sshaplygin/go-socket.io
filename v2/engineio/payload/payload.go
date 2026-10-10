package payload

import (
	"errors"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// DefaultMaxPayload is the body limit, in wire bytes, that New applies to a read
// when it is given none: 1 MiB.
const DefaultMaxPayload = 1 << 20

// Payload carries Engine.IO v4 polling payloads between HTTP requests and the
// packet reader and writer of one session.
type Payload struct {
	close     chan struct{}
	closeOnce sync.Once
	err       atomic.Value

	pauser *pauser

	readLimit    int
	readerChan   chan []Packet
	feeding      int32
	readError    chan error
	readDeadline atomic.Value
	decoder      decoder

	writeLimit    atomic.Int64
	writerChan    chan *flush
	flushing      int32
	writeDeadline atomic.Value
	queue         queue
	waiting       atomic.Int32 // NextWriter calls waiting for a FlushOut
}

// New returns a new payload. readLimit bounds one body fed in and writeLimit one
// body flushed out, both in wire bytes. A readLimit of zero or less means
// DefaultMaxPayload; a writeLimit of zero or less means no limit.
func New(readLimit, writeLimit int) *Payload {
	if readLimit <= 0 {
		readLimit = DefaultMaxPayload
	}
	ret := &Payload{
		close:      make(chan struct{}),
		pauser:     newPauser(),
		readLimit:  readLimit,
		readerChan: make(chan []Packet),
		readError:  make(chan error),
		writerChan: make(chan *flush),
	}
	ret.readDeadline.Store(time.Time{})
	ret.decoder.feeder = ret
	ret.writeDeadline.Store(time.Time{})
	ret.SetWriteLimit(writeLimit)
	return ret
}

// SetWriteLimit sets the limit of one body flushed out, in wire bytes; zero or
// less means no limit. A client calls it with the maxPayload the server advertised.
func (p *Payload) SetWriteLimit(n int) {
	if n <= 0 {
		n = math.MaxInt
	}
	p.writeLimit.Store(int64(n))
}

// FeedIn reads one polling body from r, decodes it and offers its packets to
// NextReader. It returns once NextReader has consumed every packet.
// Multi-FeedIn needs be called sync.
//
// The body is read, up to the read limit, before any packet is offered, and a
// body that is malformed or oversized delivers nothing.
//
// If Close called when FeedIn, it returns io.EOF.
// If have Pause-ed when FeedIn, it returns ErrPaused.
// If NextReader has timeout, it returns ErrTimeout.
// If the body exceeds the read limit, it returns an error wrapping ErrTooLarge;
// the payload stays usable, as no packet of the body was delivered.
// If the body is malformed or cannot be read, it returns an error wrapping
// ErrInvalidPayload or the read error, and the payload fails and closes: the
// batch is lost, so the session cannot continue.
func (p *Payload) FeedIn(r io.Reader) error {
	select {
	case <-p.close:
		return p.load()
	default:
	}

	if !atomic.CompareAndSwapInt32(&p.feeding, 0, 1) {
		return newOpError("read", errOverlap)
	}
	defer atomic.StoreInt32(&p.feeding, 0)

	if ok := p.pauser.Working(); !ok {
		return newOpError("payload", errPaused)
	}
	defer p.pauser.Done()

	packets, err := DecodeReader(r, p.readLimit)
	if err != nil {
		if errors.Is(err, ErrTooLarge) {
			return newOpError("read", err)
		}
		err = p.Store("read", err)
		_ = p.Close() // wakes NextReader, which returns the stored error
		return err
	}

	for {
		after, ok := p.readTimeout()
		if !ok {
			return p.Store("read", errTimeout)
		}

		select {
		case <-p.close:
			return p.load()

		case <-after:
			// it may changed during wait, need check again
			continue

		case p.readerChan <- packets:
		}
		break
	}

	for {
		after, ok := p.readTimeout()
		if !ok {
			return p.Store("read", errTimeout)
		}

		select {
		case <-after:
			// it may changed during wait, need check again
			continue

		case err := <-p.readError:
			return p.Store("read", err)
		}
	}
}

// NextReader returns a reader for next frame.
// NextReader and SetReadDeadline needs be called sync.
//
// If Close called when NextReader,  it return io.EOF.
// Pause doesn't effect to NextReader. NextReader should wait till resumed
// and next FeedIn.
func (p *Payload) NextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	ft, pt, r, err := p.decoder.NextReader()
	return ft, pt, r, err
}

// SetReadDeadline sets next reader deadline.
// NextReader and SetReadDeadline needs be called sync.
// NextReader will wait a FeedIn call, then it returns a ReadCloser over the
// data of the next packet of FeedIn's body.
//
// If Close called when SetReadDeadline,  it return io.EOF.
// If beyond the time set by SetReadDeadline, it returns ErrTimeout.
// Pause doesn't effect to SetReadDeadline.
func (p *Payload) SetReadDeadline(t time.Time) error {
	p.readDeadline.Store(t)
	return nil
}

// NextWriter returns a writer for next frame.
// NextWriter and SetWriterDeadline needs be called sync.
// NextWriter will wait a FlushOut call, then it returns a WriteCloser which
// collects one packet. Close hands the packet to that FlushOut, whose body can
// carry other packets that were handed over concurrently, and returns once the
// body was written.
//
// If Close called when NextWriter,  it returns io.EOF.
// If beyond the time set by SetWriteDeadline, it returns ErrTimeout.
// If Pause called when NextWriter, it returns ErrPaused.
func (p *Payload) NextWriter(ft frame.Type, pt packet.Type) (io.WriteCloser, error) {
	return p.nextWriter(ft, pt)
}

// SetWriteDeadline sets next writer deadline.
// NextWriter and SetWriteDeadline needs be called sync.
//
// If Close called when SetWriteDeadline,  it return io.EOF.
// Pause doesn't effect to SetWriteDeadline.
func (p *Payload) SetWriteDeadline(t time.Time) error {
	p.writeDeadline.Store(t)
	return nil
}

// ReadDeadline returns the deadline set by SetReadDeadline; the zero time means none.
// A transport applies it to the HTTP request whose body FeedIn reads.
func (p *Payload) ReadDeadline() time.Time {
	return p.readDeadline.Load().(time.Time)
}

// Pause pauses the payload. It will wait all reader and writer closed which
// created from NextReader or NextWriter.
// It can call in multi-goroutine.
func (p *Payload) Pause() {
	p.pauser.Pause()
}

// Resume resumes the payload.
// It can call in multi-goroutine.
func (p *Payload) Resume() {
	p.pauser.Resume()
}

// Close closes the payload.
// It can call in multi-goroutine.
func (p *Payload) Close() error {
	p.closeOnce.Do(func() {
		close(p.close)
	})
	return nil
}

// Store stores a error in payload, and block all other request.
func (p *Payload) Store(op string, err error) error {
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

func (p *Payload) readTimeout() (<-chan time.Time, bool) {
	deadline := p.readDeadline.Load().(time.Time)
	wait := time.Until(deadline)
	if deadline.IsZero() {
		// wait for every
		wait = math.MaxInt64
	}
	if wait <= 0 {
		return nil, false
	}
	return time.After(wait), true
}

func (p *Payload) writeTimeout() (<-chan time.Time, bool) {
	deadline := p.writeDeadline.Load().(time.Time)
	wait := time.Until(deadline)
	if deadline.IsZero() {
		// wait for every
		wait = math.MaxInt64
	}
	if wait <= 0 {
		return nil, false
	}
	return time.After(wait), true
}

func (p *Payload) getReader() ([]Packet, error) {
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
		after, ok := p.readTimeout()
		if !ok {
			return nil, p.Store("read", errTimeout)
		}
		select {
		case <-p.close:
			return nil, p.load()
		case <-p.pauser.PausedTrigger():
			return nil, newOpError("payload", errPaused)
		case <-after:
			continue
		case packets := <-p.readerChan:
			return packets, nil
		}
	}
}

func (p *Payload) putReader(err error) error {
	select {
	case <-p.close:
		return p.load()
	default:
	}
	for {
		after, ok := p.readTimeout()
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

func (p *Payload) load() error {
	ret := p.err.Load()
	if ret == nil {
		return io.EOF
	}
	return ret.(error)
}

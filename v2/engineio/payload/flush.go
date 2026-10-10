package payload

import (
	"bytes"
	"io"
	"sync"
	"sync/atomic"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
)

// noopBody is the body that answers a poll while the payload is paused: one NOOP packet.
var noopBody = []byte{packet.NOOP.StringByte()}

// flush is the state of one FlushOut call. Each NextWriter takes a ticket from the
// FlushOut that is waiting; open counts the tickets not yet returned by Close.
type flush struct {
	open int           // guarded by queue.mu
	gone bool          // guarded by queue.mu; FlushOut has returned
	wake chan struct{} // capacity 1; a writer returned its ticket
}

// queued is a finished packet that waits for a FlushOut to write it.
type queued struct {
	pkt  Packet
	done chan error // capacity 1; receives the result of the write
}

type queue struct {
	mu    sync.Mutex
	items []*queued
}

type packetWriter struct {
	p      *Payload
	f      *flush
	pkt    Packet
	buf    bytes.Buffer
	closed bool
}

func (p *Payload) nextWriter(ft frame.Type, pt packet.Type) (io.WriteCloser, error) {
	f, err := p.getFlush()
	if err != nil {
		return nil, err
	}
	return &packetWriter{p: p, f: f, pkt: Packet{Frame: ft, Type: pt}}, nil
}

func (w *packetWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

// Close hands the packet to the FlushOut that issued the writer and returns once
// that FlushOut wrote it, or failed.
func (w *packetWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	w.pkt.Data = w.buf.Bytes()
	return w.p.submit(w.f, w.pkt)
}

// getFlush waits for a FlushOut that accepts one more packet.
func (p *Payload) getFlush() (*flush, error) {
	select {
	case <-p.close:
		return nil, p.load()
	default:
	}

	if ok := p.pauser.Working(); !ok {
		return nil, newOpError("payload", errPaused)
	}
	p.pauser.Done()

	p.waiting.Add(1)
	defer p.waiting.Add(-1)
	for {
		after, ok := p.writeTimeout()
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
		case f := <-p.writerChan:
			return f, nil
		}
	}
}

// submit queues pkt for the FlushOut that issued the ticket f and waits for its write.
func (p *Payload) submit(f *flush, pkt Packet) error {
	err := validate(pkt)
	var item *queued

	p.queue.mu.Lock()
	f.open--
	switch {
	case f.gone:
		err = errDetached
	case err == nil:
		item = &queued{pkt: pkt, done: make(chan error, 1)}
		p.queue.items = append(p.queue.items, item)
	}
	p.queue.mu.Unlock()

	select {
	case f.wake <- struct{}{}:
	default:
	}
	if item == nil {
		return err
	}

	select {
	case err := <-item.done:
		return err
	case <-p.close:
		select {
		case err := <-item.done:
			return err
		default:
			return p.load()
		}
	}
}

// FlushOut writes the queued packets to w as one polling body and returns when
// the write ended. FlushOut needs be called sync.
//
// It waits for NextWriter to hand over a packet, then takes the other packets
// that writers are handing over at that moment, as many as fit the write limit;
// a packet that does not fit stays queued for the next FlushOut. A packet larger
// than the write limit alone is dropped and its Close returns ErrTooLarge.
//
// If Close called when Flushout, it return io.EOF.
// If Pause called when Flushout, it flushs out a NOOP message and return nil.
// If NextWriter has timeout, it returns ErrTimeout.
// If write error while FlushOut, it returns write error.
func (p *Payload) FlushOut(w io.Writer) error {
	select {
	case <-p.close:
		return p.load()
	default:
	}

	if !atomic.CompareAndSwapInt32(&p.flushing, 0, 1) {
		return newOpError("write", errOverlap)
	}
	defer atomic.StoreInt32(&p.flushing, 0)

	if ok := p.pauser.Working(); !ok {
		_, err := w.Write(noopBody)
		return err
	}
	defer p.pauser.Done()

	f := &flush{wake: make(chan struct{}, 1)}
	defer func() {
		p.queue.mu.Lock()
		f.gone = true
		p.queue.mu.Unlock()
	}()

	for {
		paused, err := p.collect(f)
		if err != nil {
			return err
		}
		if paused {
			_, err := w.Write(noopBody)
			return err
		}

		items, body := p.take()
		if body == nil {
			continue
		}
		_, err = w.Write(body)
		if err != nil {
			err = p.Store("write", err)
		}
		for _, item := range items {
			item.done <- err
		}
		return err
	}
}

// collect waits until packets are queued and every ticket it handed out came back.
// It reports paused when the payload paused before any packet arrived.
func (p *Payload) collect(f *flush) (paused bool, err error) {
	for {
		p.queue.mu.Lock()
		queued, open := len(p.queue.items), f.open
		p.queue.mu.Unlock()

		after, ok := p.writeTimeout()
		if !ok {
			return false, p.Store("write", errTimeout)
		}

		switch {
		case open > 0: // wait for the writers that took a ticket
			select {
			case <-p.close:
				return false, p.load()
			case <-after:
			case <-f.wake:
			}

		case queued == 0: // wait for a first writer
			select {
			case <-p.close:
				return false, p.load()
			case <-after:
			case <-p.pauser.PausingTrigger():
				return true, nil
			case p.writerChan <- f:
				p.addTicket(f)
			}

		default: // take the writers that are already waiting, without waiting for more
			select {
			case p.writerChan <- f:
				p.addTicket(f)
			default:
				return false, nil
			}
		}
	}
}

func (p *Payload) addTicket(f *flush) {
	p.queue.mu.Lock()
	f.open++
	p.queue.mu.Unlock()
}

// take removes the longest prefix of the queue that fits the write limit and
// returns it with its body. A first packet that cannot fit is dropped, its
// writer gets the error and take returns a nil body.
func (p *Payload) take() ([]*queued, []byte) {
	p.queue.mu.Lock()
	defer p.queue.mu.Unlock()

	packets := make([]Packet, len(p.queue.items))
	for i, item := range p.queue.items {
		packets[i] = item.pkt
	}
	body, n, err := EncodeBatch(packets, int(p.writeLimit.Load()))
	if err != nil {
		first := p.queue.items[0]
		p.queue.items = append([]*queued(nil), p.queue.items[1:]...)
		first.done <- newOpError("write", err)
		return nil, nil
	}
	items := p.queue.items[:n:n]
	p.queue.items = append([]*queued(nil), p.queue.items[n:]...)
	return items, body
}

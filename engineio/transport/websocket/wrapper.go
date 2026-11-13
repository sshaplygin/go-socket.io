package websocket

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

type wrapper struct {
	*websocket.Conn

	writeLocker *sync.Mutex
	readLocker  *sync.Mutex
	writing     *atomic.Bool
	readLimit   *atomic.Int64
	readFailed  *atomic.Bool
	compression bool
	threshold   int

	// readers counts the readers that consume the peer's frames: a NextReader
	// call and the message reader it returned until that is closed. Close waits
	// for the peer's reply to its close frame only while one runs. peerGone
	// closes when the peer's close frame arrives or a read fails.
	readers  *atomic.Int32
	peerGone chan struct{}
	peerOnce *sync.Once
	// peerClosed is set when the peer's close frame has status 1000, 1001 or
	// none (1005), before the reply is written and the read returns.
	peerClosed *atomic.Bool
}

func newWrapper(conn *websocket.Conn) wrapper {
	w := wrapper{
		Conn:        conn,
		writeLocker: new(sync.Mutex),
		readLocker:  new(sync.Mutex),
		writing:     new(atomic.Bool),
		readLimit:   new(atomic.Int64),
		readFailed:  new(atomic.Bool),
		readers:     new(atomic.Int32),
		peerGone:    make(chan struct{}),
		peerOnce:    new(sync.Once),
		peerClosed:  new(atomic.Bool),
	}
	// gorilla's default handler, which replies to the peer's close frame, after
	// noting the frame and whether its status marks an orderly close.
	conn.SetCloseHandler(func(code int, _ string) error {
		switch code {
		case websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived:
			w.peerClosed.Store(true)
		}
		w.markPeerGone()
		msg := websocket.FormatCloseMessage(code, "")
		_ = conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
		return nil
	})
	return w
}

func (w wrapper) markPeerGone() { w.peerOnce.Do(func() { close(w.peerGone) }) }

// awaitPeerClose waits until the peer answers a close frame, a read fails,
// stop closes or the deadline passes. A running reader receives the answer.
// With none, awaitPeerClose reads the frames itself under the read lock, so
// that the reader that calls Close from an event handler is not needed: the
// reader count is zero then, and a reader that starts waits for the lock.
func (w wrapper) awaitPeerClose(deadline time.Time, stop <-chan struct{}) {
	if w.readers.Load() == 0 && w.readLocker.TryLock() {
		idle := w.readers.Load() == 0
		if idle {
			w.discardUntilClosed(deadline)
		}
		w.readLocker.Unlock()
		if idle {
			return
		}
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-w.peerGone:
	case <-stop:
	case <-timer.C:
	}
}

// discardUntilClosed reads and drops the peer's frames until its close frame
// or a read failure. At the deadline, or when Abort closes the connection, the
// read ends because the connection closes.
func (w wrapper) discardUntilClosed(deadline time.Time) {
	timer := time.AfterFunc(time.Until(deadline), func() { _ = w.Conn.Close() })
	defer timer.Stop()
	for {
		_, r, err := w.Conn.NextReader()
		if err != nil {
			return
		}
		if _, err = io.Copy(io.Discard, r); err != nil {
			return
		}
	}
}

func (w wrapper) NextReader() (eioparser.FrameType, io.ReadCloser, error) {
	w.readers.Add(1)
	typ, r, err := w.nextReader()
	if err != nil {
		w.readers.Add(-1)
		w.markPeerGone()
	}
	return typ, r, err
}

func (w wrapper) nextReader() (eioparser.FrameType, io.ReadCloser, error) {
	w.readLocker.Lock()
	defer w.readLocker.Unlock()

	// Only one goroutine advances the websocket message reader.
	if w.readFailed.Load() {
		return 0, nil, readLimit(websocket.ErrReadLimit, w.readLimit.Load())
	}
	typ, r, err := w.Conn.NextReader()
	if err != nil {
		return 0, nil, readLimit(err, w.readLimit.Load())
	}

	if limit := w.readLimit.Load(); limit > 0 {
		r = &inflatedReader{Reader: r, conn: w.Conn, remaining: limit, failed: w.readFailed}
	}
	// Gorilla returns its inflater to a pool at EOF. A later drain must not
	// read that released inflater again (which would return ErrClosedPipe).
	r = &messageReader{Reader: r, limit: w.readLimit.Load()}

	switch typ {
	case websocket.TextMessage:
		return eioparser.String, newRcWrapper(w, r), nil
	case websocket.BinaryMessage:
		return eioparser.Binary, newRcWrapper(w, r), nil
	}

	return 0, nil, transport.ErrInvalidFrame
}

type rcWrapper struct {
	io.Reader
	l *sync.Mutex
	// release ends the message reader's count in wrapper.readers, once.
	release func()
}

func newRcWrapper(w wrapper, r io.Reader) rcWrapper {
	return rcWrapper{Reader: r, l: w.readLocker, release: sync.OnceFunc(func() { w.readers.Add(-1) })}
}

func (r rcWrapper) Close() error {
	defer r.release()
	r.l.Lock()
	defer r.l.Unlock()

	// Attempt to drain the Reader.
	_, err := io.Copy(io.Discard, r)

	return err
}

func (w wrapper) NextWriter(ft eioparser.FrameType) (io.WriteCloser, error) {
	return w.nextWriter(ft, true)
}

func (w wrapper) nextWriter(FType eioparser.FrameType, compress bool) (io.WriteCloser, error) {
	var t int

	switch FType {
	case eioparser.String:
		t = websocket.TextMessage
	case eioparser.Binary:
		t = websocket.BinaryMessage
	default:
		return nil, transport.ErrInvalidFrame
	}

	w.writeLocker.Lock()
	// Delay the first wire write only until the encoded packet reaches the
	// threshold; large packets then stream without an unbounded staging buffer.
	writer := &thresholdWriter{conn: w.Conn, typ: t, threshold: w.threshold, compress: compress && w.compression}

	w.writing.Store(true)
	result := &wcWrapper{WriteCloser: writer, writing: w.writing}
	result.l.Store(w.writeLocker)
	return result, nil
}

// wcWrapper is one frame writer. It holds the write lock until its first
// Close, which clears l; concurrent or later calls see nil and keep the lock.
type wcWrapper struct {
	io.WriteCloser
	writing *atomic.Bool
	l       atomic.Pointer[sync.Mutex]
}

func (w *wcWrapper) Close() error {
	// By the next call the lock may belong to the next writer: keep it.
	l := w.l.Swap(nil)
	if l == nil {
		return io.ErrClosedPipe
	}
	// Unlock the wrapper's write lock for future calls to NextWriter.
	defer l.Unlock()
	defer w.writing.Store(false)
	return w.WriteCloser.Close()
}

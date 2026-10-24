package websocket

import (
	"io"
	"sync"
	"sync/atomic"

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
}

func newWrapper(conn *websocket.Conn) wrapper {
	return wrapper{
		Conn:        conn,
		writeLocker: new(sync.Mutex),
		readLocker:  new(sync.Mutex),
		writing:     new(atomic.Bool),
		readLimit:   new(atomic.Int64),
		readFailed:  new(atomic.Bool),
	}
}

func (w wrapper) NextReader() (eioparser.FrameType, io.ReadCloser, error) {
	w.readLocker.Lock()
	defer w.readLocker.Unlock()

	// Only one goroutine advances the websocket message reader.
	if w.readFailed.Load() {
		return 0, nil, websocket.ErrReadLimit
	}
	typ, r, err := w.Conn.NextReader()
	if err != nil {
		return 0, nil, err
	}

	if limit := w.readLimit.Load(); limit > 0 {
		r = &inflatedReader{Reader: r, conn: w.Conn, remaining: limit, failed: w.readFailed}
	}
	// Gorilla returns its inflater to a pool at EOF. A later drain must not
	// read that released inflater again (which would return ErrClosedPipe).
	r = &messageReader{Reader: r}

	switch typ {
	case websocket.TextMessage:
		return eioparser.String, newRcWrapper(w.readLocker, r), nil
	case websocket.BinaryMessage:
		return eioparser.Binary, newRcWrapper(w.readLocker, r), nil
	}

	return 0, nil, transport.ErrInvalidFrame
}

type rcWrapper struct {
	io.Reader
	l *sync.Mutex
}

func newRcWrapper(l *sync.Mutex, r io.Reader) rcWrapper { return rcWrapper{Reader: r, l: l} }

func (r rcWrapper) Close() error {
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
	return &wcWrapper{WriteCloser: writer, writing: w.writing, l: w.writeLocker}, nil
}

// wcWrapper is one frame writer. It holds the write lock until its first Close.
type wcWrapper struct {
	io.WriteCloser
	writing *atomic.Bool
	l       *sync.Mutex
	closed  bool
}

func (w *wcWrapper) Close() error {
	// By the next call the lock may belong to the next writer: keep it.
	if w.closed {
		return io.ErrClosedPipe
	}
	w.closed = true
	// Unlock the wrapper's write lock for future calls to NextWriter.
	defer w.l.Unlock()
	defer w.writing.Store(false)
	return w.WriteCloser.Close()
}

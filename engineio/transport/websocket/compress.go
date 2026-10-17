package websocket

import (
	"io"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type messageReader struct {
	io.Reader
	err error
}

func (r *messageReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	n, err := r.Reader.Read(p)
	r.err = err
	return n, err
}

// thresholdWriter retains only the short prefix needed to choose compression.
// Its caller owns the websocket write lock until Close returns.
type thresholdWriter struct {
	conn      *websocket.Conn
	typ       int
	threshold int
	compress  bool
	prefix    []byte
	writer    io.WriteCloser
	err       error
}

func (w *thresholdWriter) start(compress bool) error {
	w.conn.EnableWriteCompression(compress)
	w.writer, w.err = w.conn.NextWriter(w.typ)
	if w.err == nil && len(w.prefix) != 0 {
		_, w.err = w.writer.Write(w.prefix)
	}
	w.prefix = nil
	return w.err
}

func (w *thresholdWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.writer == nil {
		if w.compress && len(p) < w.threshold-len(w.prefix) {
			w.prefix = append(w.prefix, p...)
			return len(p), nil
		}
		if err := w.start(w.compress); err != nil {
			return 0, err
		}
	}
	n, err := w.writer.Write(p)
	w.err = err
	return n, err
}

func (w *thresholdWriter) Close() error {
	if w.writer == nil && w.err == nil {
		_ = w.start(false)
	}
	if w.writer != nil {
		err := w.writer.Close()
		if w.err == nil {
			w.err = err
		}
	}
	return w.err
}

// Gorilla limits wire bytes. Enforce the same limit after its inflater, reading
// at most one byte beyond it and never draining a rejected compressed message.
type inflatedReader struct {
	io.Reader
	conn      *websocket.Conn
	remaining int64
	failed    *atomic.Bool
}

func (r *inflatedReader) Read(p []byte) (int, error) {
	if r.failed.Load() {
		return 0, websocket.ErrReadLimit
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining+1]
	}
	n, err := r.Reader.Read(p)
	r.remaining -= int64(n)
	if r.remaining < 0 {
		r.failed.Store(true)
		_ = r.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseMessageTooBig, ""), time.Now().Add(time.Second))
		// The session owns the physical close after its synchronous Error hook.
		// failed also prevents NextReader from advancing to another message.
		return 0, websocket.ErrReadLimit
	}
	return n, err
}

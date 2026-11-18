//go:build js && wasm

package browser

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"syscall/js"
	"time"

	"github.com/gorilla/websocket"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// Close codes. The browser lets a page send only 1000 and 3000-4999, so the
// codes of an abort and of a message past the read limit (1009 natively) are
// taken from the private range.
const (
	codeNormal   = 1000
	codeAbort    = 4000
	codeTooLarge = 4009
)

// closeReplyTimeout bounds how long Close waits for the close event after it
// sends its close frame, as the native websocket transport waits for the reply.
const closeReplyTimeout = 250 * time.Millisecond

// handlers are the event handler properties of a WebSocket that conn sets.
var handlers = [...]string{"onopen", "onmessage", "onerror", "onclose"}

// liveFuncs counts the js.Funcs of all connections that are not released.
var liveFuncs atomic.Int64

// uint8Array is the Uint8Array constructor; tests replace it with a value that fails.
var uint8Array = js.Global().Get("Uint8Array")

type message struct {
	binary bool
	data   []byte
}

// conn is a browser WebSocket. The JavaScript callbacks never block: they hold
// mu only to change the fields below it, and signal with non-blocking sends.
type conn struct {
	ws      js.Value
	url     url.URL
	funcs   [len(handlers)]js.Func
	release func() // detaches the handlers and releases funcs, once

	opened  chan struct{} // closed by the open event
	done    chan struct{} // closed by the close event
	notify  chan struct{} // a message, the end or a new read deadline
	aborted chan struct{} // closed by Abort; ends the wait of Close
	limit   atomic.Int64
	limited atomic.Bool // a message passed the read limit
	writers atomic.Int32
	once    sync.Once // the first Close
	abort   sync.Once // the first Abort

	mu            sync.Mutex
	queue         []message
	err           error // set once; returned after the queue is read
	readDeadline  time.Time
	writeDeadline time.Time
}

func newConn(u url.URL) *conn {
	c := &conn{
		url:     u,
		opened:  make(chan struct{}),
		done:    make(chan struct{}),
		notify:  make(chan struct{}, 1),
		aborted: make(chan struct{}),
	}
	c.release = sync.OnceFunc(func() {
		for _, name := range handlers {
			_ = catch(func() { c.ws.Set(name, js.Null()) })
		}
		for _, f := range c.funcs {
			f.Release()
			liveFuncs.Add(-1)
		}
	})
	return c
}

func (c *conn) attach() {
	var openOnce sync.Once
	callbacks := [len(handlers)]func(js.Value){
		func(js.Value) { openOnce.Do(func() { close(c.opened) }) },
		c.onMessage,
		func(js.Value) {}, // the close event that follows reports the failure
		c.onClose,
	}
	for i, name := range handlers {
		callback := callbacks[i]
		c.funcs[i] = js.FuncOf(func(_ js.Value, args []js.Value) any {
			callback(args[0])
			return nil
		})
		liveFuncs.Add(1)
		c.ws.Set(name, c.funcs[i])
	}
}

func (c *conn) wake() {
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

// finish stores the terminal error unless one is stored, and reports whether it did.
func (c *conn) finish(err error) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return false
	}
	c.err = err
	return true
}

func (c *conn) closeErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// onClose reports a close that the connection did not start as the peer's
// close frame, so that the Engine.IO client classifies it as the native
// transport's.
func (c *conn) onClose(event js.Value) {
	c.finish(&websocket.CloseError{Code: event.Get("code").Int(), Text: event.Get("reason").String()})
	close(c.done)
	c.wake()
	c.release()
}

func (c *conn) onMessage(event js.Value) {
	data := event.Get("data")
	limit := c.limit.Load()
	m := message{binary: data.Type() != js.TypeString}
	var size int
	if m.binary {
		size = data.Get("byteLength").Int()
	} else {
		m.data = []byte(data.String())
		size = len(m.data)
	}
	if limit > 0 && int64(size) > limit {
		c.limited.Store(true)
		if c.finish(readLimitError{limit}) {
			_ = c.call("close", codeTooLarge)
		}
		c.wake()
		return
	}
	if m.binary {
		m.data = make([]byte, size)
		if err := catch(func() { js.CopyBytesToGo(m.data, uint8Array.New(data)) }); err != nil {
			// A message that cannot be read ends the connection, as a transport error.
			if c.finish(err) {
				_ = c.call("close", codeAbort)
			}
			c.wake()
			return
		}
	}
	c.mu.Lock()
	if c.err == nil {
		c.queue = append(c.queue, m)
	}
	c.mu.Unlock()
	c.wake()
}

// call calls a method of the WebSocket and returns the exception it throws.
func (c *conn) call(method string, args ...any) error {
	return catch(func() { c.ws.Call(method, args...) })
}

// catch runs f and returns the panic of a JavaScript exception or of a
// syscall/js type error as an error.
func catch(f func()) (err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = fmt.Errorf("browser: %w", r)
		default:
			err = fmt.Errorf("browser: %v", r)
		}
	}()
	f()
	return nil
}

func (c *conn) timeout(op string) error {
	return &net.OpError{Op: op, Net: "websocket", Addr: c.RemoteAddr(), Err: os.ErrDeadlineExceeded}
}

func (c *conn) NextReader() (eioparser.FrameType, eioparser.PacketType, io.ReadCloser, error) {
	return eioparser.NewPacketDecoder(frameReader{c}).NextReader()
}

type frameReader struct{ c *conn }

// NextReader returns the next message. A text message "b<type><base64 data>"
// is a binary packet, as the native websocket transport reads it.
func (r frameReader) NextReader() (eioparser.FrameType, io.ReadCloser, error) {
	m, err := r.c.receive()
	if err != nil {
		return 0, nil, err
	}
	if m.binary || len(m.data) == 0 || m.data[0] != 'b' {
		return frameType(m.binary), io.NopCloser(bytes.NewReader(m.data)), nil
	}
	if len(m.data) < 2 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	packet := make([]byte, 1+base64.StdEncoding.DecodedLen(len(m.data)-2))
	packet[0] = eioparser.ByteToPacketType(m.data[1], eioparser.String).BinaryByte()
	n, err := base64.StdEncoding.Decode(packet[1:], m.data[2:])
	if err != nil {
		return 0, nil, err
	}
	return eioparser.Binary, io.NopCloser(bytes.NewReader(packet[:1+n])), nil
}

func frameType(binary bool) eioparser.FrameType {
	if binary {
		return eioparser.Binary
	}
	return eioparser.String
}

// receive returns the queued messages in arrival order, then the stored error.
func (c *conn) receive() (message, error) {
	for {
		c.mu.Lock()
		if len(c.queue) != 0 {
			m := c.queue[0]
			c.queue[0] = message{}
			c.queue = c.queue[1:]
			c.mu.Unlock()
			return m, nil
		}
		err, deadline := c.err, c.readDeadline
		c.mu.Unlock()
		if err != nil {
			return message{}, err
		}
		var timer *time.Timer
		var expired <-chan time.Time
		if !deadline.IsZero() {
			wait := time.Until(deadline)
			if wait <= 0 {
				return message{}, c.timeout("read")
			}
			timer = time.NewTimer(wait)
			expired = timer.C
		}
		select {
		case <-c.notify:
		case <-expired:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func (c *conn) NextWriter(ft eioparser.FrameType, pt eioparser.PacketType) (io.WriteCloser, error) {
	return eioparser.NewPacketEncoder(frameWriter{c}).NextWriter(ft, pt)
}

type frameWriter struct{ c *conn }

func (w frameWriter) NextWriter(ft eioparser.FrameType) (io.WriteCloser, error) {
	if ft != eioparser.String && ft != eioparser.Binary {
		return nil, transport.ErrInvalidFrame
	}
	if err := w.c.writable(); err != nil {
		return nil, err
	}
	w.c.writers.Add(1)
	return &messageWriter{c: w.c, binary: ft == eioparser.Binary}, nil
}

// writable returns the stored error, or a timeout once the write deadline passed.
func (c *conn) writable() error {
	c.mu.Lock()
	err, deadline := c.err, c.writeDeadline
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return c.timeout("write")
	}
	return nil
}

// messageWriter buffers one message, which Close sends.
type messageWriter struct {
	c      *conn
	binary bool
	buf    bytes.Buffer
	closed bool
}

func (w *messageWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	return w.buf.Write(p)
}

func (w *messageWriter) Close() error {
	if w.closed {
		return io.ErrClosedPipe
	}
	w.closed = true
	defer w.c.writers.Add(-1)
	if err := w.c.writable(); err != nil {
		return err
	}
	var data any = w.buf.String()
	if w.binary {
		err := catch(func() {
			array := uint8Array.New(w.buf.Len())
			js.CopyBytesToJS(array, w.buf.Bytes())
			data = array
		})
		if err != nil {
			return err
		}
	}
	return w.c.call("send", data)
}

// Close sends a close frame with code 1000 and waits up to closeReplyTimeout
// for the close event. Only the first call has an effect.
func (c *conn) Close() error {
	var err error
	c.once.Do(func() {
		select {
		case <-c.aborted:
			return
		default:
		}
		c.finish(net.ErrClosed)
		c.wake()
		err = c.call("close", codeNormal)
		timer := time.NewTimer(closeReplyTimeout)
		select {
		case <-c.done:
		case <-c.aborted:
		case <-timer.C:
		}
		timer.Stop()
		c.release()
	})
	return err
}

// Abort closes the connection with code 4000 without waiting, and ends a wait
// of Close.
func (c *conn) Abort() error {
	c.abort.Do(func() {
		c.finish(net.ErrClosed)
		c.wake()
		_ = c.call("close", codeAbort)
		close(c.aborted)
		c.release()
	})
	return nil
}

// SetReadLimit sets the largest message, in bytes, that the connection accepts:
// the byteLength of a binary message or the UTF-8 length of a text message.
// A larger message closes the connection with code 4009. Nonpositive means no limit.
func (c *conn) SetReadLimit(n int64) { c.limit.Store(n) }

// ReadLimitError returns the read-limit error once a message passed the limit, else nil.
func (c *conn) ReadLimitError() error {
	if c.limited.Load() {
		return readLimitError{c.limit.Load()}
	}
	return nil
}

// Writable reports that the connection is open, no writer is open and the
// browser has sent every message it was given.
func (c *conn) Writable() bool {
	if c.closeErr() != nil || c.writers.Load() != 0 {
		return false
	}
	buffered := 1
	_ = catch(func() { buffered = c.ws.Get("bufferedAmount").Int() })
	return buffered == 0
}

func (c *conn) URL() url.URL { return c.url }

func (c *conn) LocalAddr() net.Addr { return addr{} }

func (c *conn) RemoteAddr() net.Addr { return addr{c.url.Host} }

// RemoteHeader returns an empty header: the browser does not expose the
// headers of the handshake response.
func (c *conn) RemoteHeader() http.Header { return http.Header{} }

func (c *conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	c.mu.Unlock()
	c.wake()
	return nil
}

func (c *conn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.writeDeadline = t
	c.mu.Unlock()
	return nil
}

// addr is a host or host:port; empty when unknown.
type addr struct{ host string }

func (a addr) Network() string { return "tcp" }
func (a addr) String() string  { return a.host }

// readLimitError is websocket.ErrReadLimit for a message past limit bytes. errors.As
// also finds it as *http.MaxBytesError, as for the native transports.
type readLimitError struct{ limit int64 }

func (e readLimitError) Error() string { return websocket.ErrReadLimit.Error() }
func (e readLimitError) Unwrap() error { return websocket.ErrReadLimit }
func (e readLimitError) As(target any) bool {
	t, ok := target.(**http.MaxBytesError)
	if ok {
		*t = &http.MaxBytesError{Limit: e.limit}
	}
	return ok
}

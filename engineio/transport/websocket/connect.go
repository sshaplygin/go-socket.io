package websocket

import (
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// conn implements transport.Conn
type conn struct {
	transport.FrameReader

	ws wrapper
	// base64 is set by b64 in the request URL: binary packets travel as
	// "b<type><base64>" text messages in both directions.
	base64 bool

	url          url.URL
	remoteHeader http.Header

	// closeFrame makes Close announce the close with a normal closure frame
	// first. Accept sets it on server connections.
	closeFrame bool

	closed     chan struct{}
	closedOnce sync.Once
	closeOnce  sync.Once
	aborted    atomic.Bool
	abort      chan struct{} // closed by Abort
	abortOnce  sync.Once
}

// closeFrameTimeout bounds how long Close takes to write its close frame, and
// closeReplyTimeout how long it then waits for the peer's reply. A peer that
// stopped reading, with its receive window full, or that does not answer cannot
// hold Close longer: the connection is then closed.
const (
	closeFrameTimeout = 500 * time.Millisecond
	closeReplyTimeout = 250 * time.Millisecond
)

func newConn(ws *websocket.Conn, url url.URL, header http.Header, settings ...*Transport) *conn {
	w := newWrapper(ws)
	if len(settings) != 0 {
		w.compression = settings[0].EnableCompression
		w.threshold = settings[0].CompressionThreshold
	}
	if w.threshold <= 0 {
		w.threshold = 1024
	}
	closed := make(chan struct{})

	// With b64, inbound text messages starting with 'b' decode as binary packets;
	// binary messages are still accepted.
	var decoder eioparser.FrameReader = w
	b64 := url.Query().Get("b64") != ""
	if b64 {
		decoder = base64FrameReader{w}
	}
	return &conn{
		url:          url,
		remoteHeader: header,
		ws:           w,
		base64:       b64,
		closed:       closed,
		abort:        make(chan struct{}),
		FrameReader:  eioparser.NewPacketDecoder(decoder),
	}
}

func (c *conn) URL() url.URL {
	return c.url
}

func (c *conn) RemoteHeader() http.Header {
	return c.remoteHeader
}

func (c *conn) LocalAddr() net.Addr {
	return c.ws.LocalAddr()
}

func (c *conn) RemoteAddr() net.Addr {
	return c.ws.RemoteAddr()
}

func (c *conn) SetReadDeadline(t time.Time) error {
	return c.ws.SetReadDeadline(t)
}

// SetReadLimit caps both wire and inflated message bytes, closing with code 1009.
func (c *conn) SetReadLimit(n int64) {
	c.ws.readLimit.Store(n)
	c.ws.SetReadLimit(n)
}

func (c *conn) NextWriter(ft eioparser.FrameType, pt eioparser.PacketType) (io.WriteCloser, error) {
	return c.NextWriterCompress(ft, pt, true)
}

// NextWriterCompress keeps the compression choice for the entire encoded packet.
func (c *conn) NextWriterCompress(ft eioparser.FrameType, pt eioparser.PacketType, compress bool) (io.WriteCloser, error) {
	if c.base64 && ft == eioparser.Binary {
		// b64 mode: write the binary packet as text "b<type>" followed by base64 data.
		w, err := c.ws.nextWriter(eioparser.String, compress)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte{'b', pt.StringByte()}); err != nil {
			_ = w.Close()
			return nil, err
		}
		return base64Writer{WriteCloser: base64.NewEncoder(base64.StdEncoding, w), frame: w}, nil
	}

	return eioparser.NewPacketEncoder(compressFrameWriter{c.ws, compress}).NextWriter(ft, pt)
}

type compressFrameWriter struct {
	w        wrapper
	compress bool
}

func (w compressFrameWriter) NextWriter(ft eioparser.FrameType) (io.WriteCloser, error) {
	if w.compress {
		return w.w.NextWriter(ft)
	}
	return w.w.nextWriter(ft, false)
}

func (c *conn) SetWriteDeadline(t time.Time) error {
	// Gorilla stores the write deadline in a field read by its frame writer.
	// Its one-concurrent-writer contract requires excluding an open writer here.
	c.ws.writeLocker.Lock()
	err := c.ws.SetWriteDeadline(t)
	c.ws.writeLocker.Unlock()

	return err
}

func (c *conn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case <-c.closed:
	case <-r.Context().Done():
	}
}

// Close closes the connection. A server connection first writes a close frame
// with the normal closure status (1000), so that the peer can tell an orderly
// stop from a dropped connection, as engine.io's websocket transport does. Only
// the first call closes the underlying websocket and reports its error; later
// calls return nil.
//
// Close then waits up to closeReplyTimeout for the peer's reply to its frame
// before it closes the TCP connection, reading the peer's frames itself when no
// reader runs: a connection closed first would answer the reply with a reset,
// which a client such as ws 1.1.5 reports as an abnormal close (1006) although
// it has read the frame.
//
// Close does not take the write lock: it can run on a frame writer that fails
// while it holds that lock (see Session.closeTransport), and Abort can run while
// Close writes the frame. gorilla/websocket allows WriteControl and Close
// concurrently with a frame writer; WriteControl waits for the frame the writer
// is sending at most until closeFrameTimeout. An open message that has not been
// flushed is dropped, and the writer then fails with websocket.ErrCloseSent.
func (c *conn) Close() error {
	c.markClosed()
	var err error
	c.closeOnce.Do(func() {
		if c.aborted.Load() {
			return
		}
		announced := false
		if c.closeFrame {
			msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
			if c.ws.WriteControl(websocket.CloseMessage, msg, time.Now().Add(closeFrameTimeout)) == nil {
				announced = true
				c.ws.awaitPeerClose(time.Now().Add(closeReplyTimeout), c.abort)
			}
		}
		err = c.ws.Close()
		// An Abort, or the end of the wait for an unresponsive peer, has closed
		// the connection already.
		if (announced || c.aborted.Load()) && errors.Is(err, net.ErrClosed) {
			err = nil
		}
	})
	return err
}

// Abort closes the connection at once, without a close frame, and also ends a
// Close that is waiting to write one.
func (c *conn) Abort() error {
	c.aborted.Store(true)
	c.abortOnce.Do(func() { close(c.abort) })
	c.markClosed()
	_ = c.ws.Close()
	return nil
}

// PeerClosed reports whether the peer sent a close frame with status 1000
// (normal closure), 1001 (going away) or none (1005): an orderly close, which
// the session reports as "transport close". It is set before the reply to the
// frame is written and before the read that received the frame returns its
// error, so a write that the reply makes fail also observes it.
func (c *conn) PeerClosed() bool { return c.ws.peerClosed.Load() }

// ReadLimitError returns the read-limit error once a message passed the limit, else
// nil. A limit on inflated bytes is recorded before the 1009 close frame is written,
// and gorilla's limit on wire bytes when the read returns, so the session can log the
// violation from whichever failed read or write sees it first.
func (c *conn) ReadLimitError() error {
	if c.ws.readFailed.Load() {
		return readLimitError{c.ws.readLimit.Load()}
	}
	return nil
}

func (c *conn) markClosed() {
	c.closedOnce.Do(func() { close(c.closed) })
}

// Writable reports that no message writer is open on this connection.
func (c *conn) Writable() bool {
	select {
	case <-c.closed:
		return false
	default:
	}
	return !c.ws.writing.Load()
}

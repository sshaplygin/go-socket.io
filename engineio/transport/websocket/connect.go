package websocket

import (
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
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

	closed    chan struct{}
	closeOnce sync.Once
}

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

// Close closes the connection. Only the first call closes the underlying
// websocket and reports its error; later calls return nil.
func (c *conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		err = c.ws.Close()
	})
	return err
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

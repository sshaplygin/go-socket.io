package websocket

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gobwas/ws"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
)

// conn implements transport.Conn on a messageConn: every Engine.IO packet is one
// WebSocket data message, encoded and decoded whole by Encode and Decode.
type conn struct {
	mc *messageConn

	url          url.URL
	remoteHeader http.Header
	maxBytes     int
}

// newConn wraps raw after the handshake. br holds bytes the handshake read past
// the response, or nil. readBuf and writeBuf are Transport.ReadBufferSize and
// WriteBufferSize.
func newConn(raw net.Conn, br *bufio.Reader, clientSide bool, u url.URL, header http.Header, maxBytes, readBuf, writeBuf int) (*conn, error) {
	var src io.Reader
	switch {
	case br != nil && br.Buffered() > 0:
		src = br
	case readBuf > 0:
		src = raw
	}
	if readBuf > 0 {
		src = bufio.NewReaderSize(src, readBuf)
	}
	mc, err := newMessageConn(raw, src, clientSide, maxBytes, writeBuf)
	if err != nil {
		return nil, err
	}
	return &conn{mc: mc, url: u, remoteHeader: header, maxBytes: maxBytes}, nil
}

// NextReader reads the next packet. The returned reader holds the packet data
// without its type byte; closing it is optional but harmless.
func (c *conn) NextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	op, body, err := c.mc.readMessage()
	if err != nil {
		return 0, 0, nil, err
	}
	ft := frame.String
	if op == ws.OpBinary {
		ft = frame.Binary
	}
	p, err := Decode(ft, body, c.maxBytes)
	if err != nil {
		return 0, 0, nil, c.mc.fail(err)
	}
	return p.Frame, p.Type, io.NopCloser(bytes.NewReader(p.Data)), nil
}

// NextWriter returns a writer for one packet. The packet is encoded and sent,
// under the connection's write mutex, when the writer is closed; writers may be
// open concurrently.
func (c *conn) NextWriter(ft frame.Type, pt packet.Type) (io.WriteCloser, error) {
	if ft != frame.String && ft != frame.Binary {
		return nil, transport.ErrInvalidFrame
	}
	return &packetWriter{c: c, ft: ft, pt: pt}, nil
}

// packetWriter collects the data of one packet up to the message limit.
type packetWriter struct {
	c      *conn
	ft     frame.Type
	pt     packet.Type
	buf    bytes.Buffer
	err    error
	closed bool
}

func (w *packetWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	if w.err != nil {
		return 0, w.err
	}
	if len(p) > w.c.maxBytes-w.buf.Len() {
		w.err = ErrTooLarge
		return 0, w.err
	}
	return w.buf.Write(p)
}

// Close encodes and sends the packet. A packet that failed to encode or exceeded
// the limit is dropped and its error returned; the connection stays open.
func (w *packetWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.err != nil {
		return w.err
	}
	ft, msg, err := Encode(Packet{Frame: w.ft, Type: w.pt, Data: w.buf.Bytes()}, true, w.c.maxBytes)
	if err != nil {
		return err
	}
	op := ws.OpText
	if ft == frame.Binary {
		op = ws.OpBinary
	}
	return w.c.mc.writeMessage(op, msg)
}

func (c *conn) URL() url.URL {
	return c.url
}

func (c *conn) RemoteHeader() http.Header {
	return c.remoteHeader
}

func (c *conn) LocalAddr() net.Addr {
	return c.mc.raw.LocalAddr()
}

func (c *conn) RemoteAddr() net.Addr {
	return c.mc.raw.RemoteAddr()
}

func (c *conn) SetReadDeadline(t time.Time) error {
	return c.mc.raw.SetReadDeadline(t)
}

func (c *conn) SetWriteDeadline(t time.Time) error {
	return c.mc.raw.SetWriteDeadline(t)
}

// ServeHTTP returns at once: the upgrade hijacked the connection, so the request
// goroutine owns nothing and must not stay blocked for the life of the session.
func (c *conn) ServeHTTP(http.ResponseWriter, *http.Request) {}

// Close closes the connection. Only the first call closes the underlying
// connection and reports its error; later calls report the same result.
func (c *conn) Close() error {
	return c.mc.Close()
}

package transport

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// FrameReader reads a frame. It needs be closed before next reading.
type FrameReader interface {
	NextReader() (eioparser.FrameType, eioparser.PacketType, io.ReadCloser, error)
}

// FrameWriter writes a frame. It needs be closed before next writing.
type FrameWriter interface {
	NextWriter(ft eioparser.FrameType, pt eioparser.PacketType) (io.WriteCloser, error)
}

// Conn is a transport connection.
type Conn interface {
	FrameReader
	FrameWriter
	io.Closer
	URL() url.URL
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	RemoteHeader() http.Header
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

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

// Pauser optionally pauses and resumes a transport around an upgrade.
// Client transports may also implement PauseContext(context.Context) error.
// That method must honor cancellation even before it starts; on rollback the
// client cancels it, waits for it to return, and calls Resume once. Legacy
// client transports retain repeated Resume calls until their Pause returns.
type Pauser interface {
	Pause()
	Resume()
}

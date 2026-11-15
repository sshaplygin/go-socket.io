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
	// NextReader returns the frame type, the packet type and the contents of
	// the next packet. The reader must be closed before NextReader is called
	// again.
	NextReader() (eioparser.FrameType, eioparser.PacketType, io.ReadCloser, error)
}

// FrameWriter writes a frame. It needs be closed before next writing.
type FrameWriter interface {
	// NextWriter starts a packet of type pt in a frame of type ft. The packet
	// is complete when the writer is closed, which must happen before
	// NextWriter is called again.
	NextWriter(ft eioparser.FrameType, pt eioparser.PacketType) (io.WriteCloser, error)
}

// Conn is a transport connection.
type Conn interface {
	FrameReader
	FrameWriter
	io.Closer
	// URL returns the URL of the request that created the connection.
	URL() url.URL
	// LocalAddr returns the local address; see polling.Addr for polling.
	LocalAddr() net.Addr
	// RemoteAddr returns the peer address; see polling.Addr for polling.
	RemoteAddr() net.Addr
	// RemoteHeader returns the headers of the request that created a server
	// connection. On a client connection it returns the headers of the
	// server's websocket handshake response, or of the latest polling
	// response.
	RemoteHeader() http.Header
	// SetReadDeadline sets the time after which reads fail with an error
	// that reports Timeout; the zero time means no deadline.
	SetReadDeadline(t time.Time) error
	// SetWriteDeadline sets the time after which writes fail with an error
	// that reports Timeout; the zero time means no deadline.
	SetWriteDeadline(t time.Time) error
}

// Pauser optionally pauses and resumes a transport around an upgrade.
// Client transports may also implement PauseContext(context.Context) error.
// That method must honor cancellation even before it starts; on rollback the
// client cancels it, waits for it to return, and calls Resume once. Legacy
// client transports retain repeated Resume calls until their Pause returns.
type Pauser interface {
	// Pause stops new reads and writes of the transport and waits for the
	// ones in progress to finish.
	Pause()
	// Resume lets a paused transport continue, after an upgrade that did not
	// complete.
	Resume()
}

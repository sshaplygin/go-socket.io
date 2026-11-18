package engineio

import (
	"io"
	"net"
	"net/http"
	"net/url"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
)

// Conn is connection by client session
type Conn interface {
	ID() string
	// NextReader returns the next message. It also processes the Engine.IO control
	// packets that arrive before it (PING, PONG, CLOSE, NOOP), and that happens only
	// while a NextReader call is running: no goroutine of the connection reads for the
	// application. The application must therefore have one reader that keeps calling
	// NextReader, closes each returned reader promptly and does not run user code
	// between the calls; the Socket.IO server and client are such readers. A reader that
	// stalls lets the heartbeat expire: on the server longer than pingTimeout after a
	// PING, on the client longer than pingInterval + pingTimeout, and the connection
	// then ends with reason "ping timeout".
	NextReader() (frame.Type, io.ReadCloser, error)
	NextWriter(fType frame.Type) (io.WriteCloser, error)
	Close() error
	URL() url.URL
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	RemoteHeader() http.Header
	SetContext(v interface{})
	Context() interface{}
}

package engineio

import (
	"io"
	"net"
	"net/http"
	"net/url"

	"github.com/sshaplygin/go-socket.io/engineio/session"
)

// Conn is connection by client session
type Conn interface {
	ID() string
	// ReadyState returns the connection's current state.
	ReadyState() ReadyState
	// Transport returns the current transport's name.
	Transport() string
	// Upgraded reports whether a transport upgrade has completed.
	Upgraded() bool
	NextReader() (session.FrameType, io.ReadCloser, error)
	NextWriter(fType session.FrameType) (io.WriteCloser, error)
	Close() error
	URL() url.URL
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	RemoteHeader() http.Header
	SetContext(v interface{})
	Context() interface{}
}

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

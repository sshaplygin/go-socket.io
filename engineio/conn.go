package engineio

import (
	"io"
	"net"
	"net/http"
	"net/url"

	sessionImpl "github.com/sshaplygin/go-socket.io/engineio/internal/session"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// Conn is an accepted server-side Engine.IO connection.
type Conn interface {
	ID() string
	// ReadyState returns the connection's current state.
	ReadyState() ReadyState
	// Transport returns the current transport's name.
	Transport() string
	// Upgraded reports whether a transport upgrade has completed.
	Upgraded() bool
	NextReader() (eioparser.FrameType, io.ReadCloser, error)
	NextWriter(fType eioparser.FrameType) (io.WriteCloser, error)
	Close() error
	URL() url.URL
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	RemoteHeader() http.Header
	SetContext(v interface{})
	Context() interface{}
}

// CompressWriter optionally writes packets with a per-packet compression flag.
// NextWriter on these connections is NextWriterCompress with compress true.
type CompressWriter interface {
	NextWriterCompress(ft eioparser.FrameType, compress bool) (io.WriteCloser, error)
}

// ReadyState is the string state of a session: StateOpening, StateOpen,
// StateClosing or StateClosed.
type ReadyState = sessionImpl.ReadyState

const (
	StateOpening = sessionImpl.StateOpening
	StateOpen    = sessionImpl.StateOpen
	StateClosing = sessionImpl.StateClosing
	StateClosed  = sessionImpl.StateClosed
)

// CloseReason is the string reason a session closed: the first cause observed.
// Values are ReasonTransportClose, ReasonPingTimeout, ReasonTransportError,
// ReasonForcedClose and ReasonServerShutdown.
type CloseReason = sessionImpl.CloseReason

const (
	ReasonTransportClose = sessionImpl.ReasonTransportClose
	ReasonPingTimeout    = sessionImpl.ReasonPingTimeout
	ReasonTransportError = sessionImpl.ReasonTransportError
	ReasonForcedClose    = sessionImpl.ReasonForcedClose
	ReasonServerShutdown = sessionImpl.ReasonServerShutdown
)

// CloseError is returned, wrapped, by NextReader and NextWriter once a session
// closes, including the first call that observes the close. Previously obtained
// readers and writers keep returning their own errors.
// Its fields are Reason CloseReason and Err error. Error() string describes the
// cause; Unwrap() error returns Err. Err is the transport error for
// ReasonTransportError, io.EOF for ReasonTransportClose, and nil otherwise.
type CloseError = sessionImpl.CloseError

// Abort immediately closes a server session; custom engines receive ordinary Close.
func Abort(c Conn) error {
	if s, ok := c.(*sessionImpl.Session); ok {
		return s.Abort()
	}
	return c.Close()
}

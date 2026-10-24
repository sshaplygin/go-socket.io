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
	// ID returns the session identifier sent in the handshake.
	ID() string
	// ReadyState returns the connection's current state.
	ReadyState() ReadyState
	// Transport returns the current transport's name.
	Transport() string
	// Upgraded reports whether a transport upgrade has completed.
	Upgraded() bool
	// NextReader returns the next MESSAGE frame; control packets are handled
	// internally. The caller must Close the reader to release the transport.
	NextReader() (eioparser.FrameType, io.ReadCloser, error)
	// NextWriter opens a MESSAGE frame of type fType. The caller must Close the
	// writer to finish the frame and release the transport.
	NextWriter(fType eioparser.FrameType) (io.WriteCloser, error)
	// Close closes the session. A polling session first delivers writes it has
	// already accepted; Abort closes immediately.
	Close() error
	// URL returns the request URL of the current transport.
	URL() url.URL
	// LocalAddr returns the local network address of the current transport.
	LocalAddr() net.Addr
	// RemoteAddr returns the peer's network address on the current transport.
	RemoteAddr() net.Addr
	// RemoteHeader returns the request headers of the current transport.
	RemoteHeader() http.Header
	// SetContext stores an application value on the connection.
	SetContext(v interface{})
	// Context returns the value stored by SetContext, or nil.
	Context() interface{}
}

// CompressWriter optionally writes packets with a per-packet compression flag.
// NextWriter on these connections is NextWriterCompress with compress true.
type CompressWriter interface {
	NextWriterCompress(ft eioparser.FrameType, compress bool) (io.WriteCloser, error)
}

// The public interfaces keep the method sets of the session contracts, and the
// server session implements both.
var (
	_ Conn                       = sessionImpl.Conn(nil)
	_ sessionImpl.Conn           = Conn(nil)
	_ CompressWriter             = sessionImpl.CompressWriter(nil)
	_ sessionImpl.CompressWriter = CompressWriter(nil)
	_ Conn                       = (*sessionImpl.Session)(nil)
	_ CompressWriter             = (*sessionImpl.Session)(nil)
)

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
//
// *CloseError implements error (Error, Unwrap). Its field Reason is the first
// close cause observed; Err is the transport error for ReasonTransportError,
// io.EOF for ReasonTransportClose, and nil otherwise. Error returns
// "engineio: session closed: " + Reason, followed by ": " and Err when Err is
// non-nil. Unwrap returns Err.
type CloseError = sessionImpl.CloseError

// Abort immediately closes a server session; custom engines receive ordinary Close.
func Abort(c Conn) error {
	if s, ok := c.(*sessionImpl.Session); ok {
		return s.Abort()
	}
	return c.Close()
}

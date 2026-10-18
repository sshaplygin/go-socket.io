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

// CompressWriter optionally writes packets with a per-packet compression flag.
// NextWriter on these connections is NextWriterCompress with compress true.
type CompressWriter interface {
	NextWriterCompress(ft session.FrameType, compress bool) (io.WriteCloser, error)
}

// ReadyState is a connection's session state.
type ReadyState = session.ReadyState

const (
	StateOpening = session.StateOpening
	StateOpen    = session.StateOpen
	StateClosing = session.StateClosing
	StateClosed  = session.StateClosed
)

// CloseError and CloseReason describe the first cause of a session's close.
type (
	CloseError  = session.CloseError
	CloseReason = session.CloseReason
)

const (
	ReasonTransportClose = session.ReasonTransportClose
	ReasonPingTimeout    = session.ReasonPingTimeout
	ReasonTransportError = session.ReasonTransportError
	ReasonForcedClose    = session.ReasonForcedClose
	ReasonServerShutdown = session.ReasonServerShutdown
)

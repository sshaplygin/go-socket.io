package engineio

import (
	"io"
	"net"
	"net/http"
	"net/url"

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

// ReadyState is the state of a session.
type ReadyState string

const (
	StateOpening ReadyState = "opening" // The OPEN packet has not been written.
	StateOpen    ReadyState = "open"    // The OPEN packet is written; no close has started.
	StateClosing ReadyState = "closing" // A close has started; the transport is not closed yet.
	StateClosed  ReadyState = "closed"  // The transport connection is closed.
)

// CloseReason is why a session closed: the first cause the session observed.
type CloseReason string

const (
	// ReasonTransportClose means the client sent a CLOSE packet.
	ReasonTransportClose CloseReason = "transport close"
	// ReasonPingTimeout means no packet arrived within PingInterval + PingTimeout.
	ReasonPingTimeout CloseReason = "ping timeout"
	// ReasonTransportError means a transport read or write failed before the deadline.
	ReasonTransportError CloseReason = "transport error"
	// ReasonForcedClose means Conn.Close was called.
	ReasonForcedClose CloseReason = "forced close"
	// ReasonServerShutdown means engineio.Server.Close closed the session.
	ReasonServerShutdown CloseReason = "server shutting down"
)

// CloseError is returned, wrapped, by NextReader and NextWriter once a session
// closes, including the first call that observes the close. Previously obtained
// readers and writers keep returning their own errors.
type CloseError struct {
	Reason CloseReason
	// Err is the transport error for ReasonTransportError, io.EOF for
	// ReasonTransportClose, and nil for the other reasons.
	Err error
}

// Error describes the close reason and, when present, its underlying error.
func (e *CloseError) Error() string {
	message := "engineio: session closed: " + string(e.Reason)
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

// Unwrap returns the underlying transport error.
func (e *CloseError) Unwrap() error { return e.Err }

// Abort immediately closes a server session; custom engines receive ordinary Close.
func Abort(c Conn) error {
	if s, ok := c.(*session); ok {
		return s.abort()
	}
	return c.Close()
}

package engineio

import (
	"io"
	"net"
	"net/http"
	"net/url"

	sessionImpl "github.com/sshaplygin/go-socket.io/engineio/internal/session"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// Conn is an accepted server-side Engine.IO connection. It is implemented by the
// library, and a minor release may add methods to it.
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
	// already accepted. A websocket session sends a close frame with status 1000
	// (normal closure) and waits briefly for the client's reply, at most 0.75
	// seconds in all. Abort closes immediately and sends no frame.
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
	// NextWriterCompress is NextWriter with a compression preference: false
	// sends the message uncompressed, true lets the transport compress it
	// under the server's compression options. The caller must Close the
	// writer to finish the frame.
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
	// StateOpening means the handshake has not started: the OPEN packet is not
	// yet being written.
	StateOpening = sessionImpl.StateOpening
	// StateOpen means the OPEN packet is being or has been written and no close
	// has started.
	StateOpen = sessionImpl.StateOpen
	// StateClosing means a close has started and the transport is not closed yet.
	StateClosing = sessionImpl.StateClosing
	// StateClosed means the transport connection is closed.
	StateClosed = sessionImpl.StateClosed
)

// CloseReason is the string reason a session closed: the first cause observed.
// Values are ReasonTransportClose, ReasonPingTimeout, ReasonTransportError,
// ReasonForcedClose and ReasonServerShutdown.
type CloseReason = sessionImpl.CloseReason

const (
	// ReasonTransportClose means the client sent a CLOSE packet, or closed its
	// websocket with a close frame of status 1000, 1001 or none (1005).
	ReasonTransportClose = sessionImpl.ReasonTransportClose
	// ReasonPingTimeout means the transport reported a deadline or timeout
	// error: the heartbeat window of PingInterval + PingTimeout passed without
	// a packet, or a server-configured HTTP timeout (http.Server ReadTimeout or
	// WriteTimeout), a poll request context deadline, or a TCP timeout expired.
	// CloseError.Err is nil for it.
	ReasonPingTimeout = sessionImpl.ReasonPingTimeout
	// ReasonTransportError means a transport read or write failed with an error
	// that is not a deadline or timeout error.
	ReasonTransportError = sessionImpl.ReasonTransportError
	// ReasonForcedClose means Conn.Close or Abort was called.
	ReasonForcedClose = sessionImpl.ReasonForcedClose
	// ReasonServerShutdown means Server.Close closed the session.
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

// Abort immediately closes a server session; a websocket is dropped without a
// close frame, so its client sees close code 1006. Custom engines receive
// ordinary Close.
func Abort(c Conn) error {
	if s, ok := c.(*sessionImpl.Session); ok {
		return s.Abort()
	}
	return c.Close()
}

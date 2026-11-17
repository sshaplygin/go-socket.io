package client

import (
	"io"
	"net"
	"net/http"
	"net/url"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// Conn is an established client-side Engine.IO connection. It is implemented by the
// library, and a minor release may add methods to it.
type Conn interface {
	// ID returns the session ID that the server sent in its OPEN packet.
	ID() string
	// ReadyState returns the connection's current state: StateOpen until the
	// client starts to close the connection, then StateClosed, also while the
	// transport is still closing. It never returns StateOpening or
	// StateClosing. NextReader and NextWriter can return the CloseError while
	// ReadyState still returns StateOpen.
	ReadyState() ReadyState
	// Transport returns the current transport's name.
	Transport() string
	// Upgraded reports whether a transport upgrade has completed.
	Upgraded() bool
	// NextReader returns the next MESSAGE frame. PONG and CLOSE packets are
	// consumed inside it: a PONG extends the transport's read deadline, so the
	// application keeps calling NextReader for the heartbeat to work, and a
	// CLOSE ends the session with ReasonTransportClose. The caller must Close
	// the reader before the next call. Once the session closes, it returns
	// the CloseError.
	NextReader() (eioparser.FrameType, io.ReadCloser, error)
	// NextWriter opens a MESSAGE frame of type fType. The caller must Close the
	// writer to send the frame; until then, other writes, including the PING
	// of the heartbeat, wait. Once the session closes, it returns the
	// CloseError.
	NextWriter(fType eioparser.FrameType) (io.WriteCloser, error)
	// Close ends the session. An open websocket gets a close frame with status
	// 1000, also after a ping timeout, within a 500 ms write deadline and a wait
	// of up to 250 ms for the server's reply; after a transport error it is
	// dropped without one. An upgrade probe not yet switched to is dropped.
	Close() error
	// URL returns the request URL of the current transport.
	URL() url.URL
	// LocalAddr returns the local network address of the current transport.
	LocalAddr() net.Addr
	// RemoteAddr returns the server's network address on the current transport.
	RemoteAddr() net.Addr
	// RemoteHeader returns the response headers of the current transport: of
	// the websocket handshake, or of the latest polling response.
	RemoteHeader() http.Header
	// SetContext stores an application value on the connection. It is safe
	// for concurrent use with Context.
	SetContext(v interface{})
	// Context returns the value stored by SetContext, or nil.
	Context() interface{}
}

// CompressWriter optionally writes packets with a per-packet compression flag.
// NextWriter on these connections is NextWriterCompress with compress true.
type CompressWriter interface {
	// NextWriterCompress is NextWriter with a compression preference: false
	// sends the message uncompressed, true lets the transport compress it as
	// it is configured to.
	NextWriterCompress(ft eioparser.FrameType, compress bool) (io.WriteCloser, error)
}

// ReadyState is the state of a session.
type ReadyState string

// The ready states of a session. Conn.ReadyState says which of them a client
// Conn reports.
const (
	StateOpening ReadyState = "opening" // The OPEN handshake is incomplete.
	StateOpen    ReadyState = "open"    // The OPEN handshake completed; no close has started.
	StateClosing ReadyState = "closing" // A close has started; the transport is not closed yet.
	StateClosed  ReadyState = "closed"  // The transport connection is closed.
)

// CloseReason is why a session closed: the first cause the session observed.
type CloseReason string

const (
	// ReasonTransportClose means the server ended the session in an orderly way:
	// it sent a CLOSE packet, or closed its websocket with a close frame of normal
	// closure, going away or no status. Other close frames are transport errors.
	ReasonTransportClose CloseReason = "transport close"
	// ReasonPingTimeout means no PONG arrived within PingInterval + PingTimeout;
	// other packets do not renew that window.
	ReasonPingTimeout CloseReason = "ping timeout"
	// ReasonTransportError means a transport read or write failed before the deadline.
	ReasonTransportError CloseReason = "transport error"
	// ReasonForcedClose means Conn.Close was called.
	ReasonForcedClose CloseReason = "forced close"
	// ReasonServerShutdown is the reason the server records when
	// engineio.Server.Close closes a session. The client does not report it;
	// it has the value of the server's reason of the same name.
	ReasonServerShutdown CloseReason = "server shutting down"
)

// CloseError is returned, as a *CloseError, by NextReader and NextWriter once a session
// closes, including the first call that observes the close. Previously obtained
// readers and writers keep returning their own errors.
type CloseError struct {
	// Reason is the first close cause the session observed.
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

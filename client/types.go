package client

import (
	"net"
	"net/http"
	"net/url"
)

// AckFunc acknowledges an incoming event once, possibly from another goroutine
// after the handler returns. It does nothing unless the peer requested an ACK
// and the originating namespace connection is still active.
// A handler with this final parameter owns the ACK; return values are ignored.
type AckFunc func(args ...interface{})

// Conn is one connection generation of a client-side Socket.IO namespace.
// Sends and lifecycle operations stay bound to that generation. Use Client.Socket
// to retain a namespace across reconnections and buffer events during recovery.
//
// Emit, Send, the emitters, Disconnect and Close may be called from any goroutine,
// concurrently with each other and with callbacks; SetContext and Context are not
// synchronized. Conn is implemented by the library, and a minor release may add
// methods to it.
type Conn interface {
	// Close sends DISCONNECT for each connected namespace and reports
	// "io client disconnect". It runs disconnect callbacks before returning;
	// queued packets drain in the background, bounded by WithCloseTimeout (one minute by
	// default). Queue overflow or an Engine.IO failure discards queued packets.
	// A Conn whose namespace has disconnected does nothing; it cannot close a
	// replacement namespace or connection.
	Close() error
	Namespace

	// ID returns the Engine.IO ID in the root namespace, or
	// <namespace>#<Engine.IO ID> in any other namespace.
	ID() string
	// URL returns the request URL of the current Engine.IO transport, which
	// changes with a transport upgrade. The result is a url.URL value, so calling
	// a pointer method such as String needs a variable: u := c.URL(); u.String().
	URL() url.URL
	// LocalAddr returns the local network address of the current transport; over
	// polling it is an empty polling.Addr.
	LocalAddr() net.Addr
	// RemoteAddr returns the server's network address on the current transport.
	RemoteAddr() net.Addr
	// RemoteHeader returns the server's response headers on the current
	// transport: of the websocket handshake, or of the latest polling response.
	RemoteHeader() http.Header
}

// Namespace exposes client operations on one Socket.IO namespace. It is
// implemented by the library, and a minor release may add methods to it.
type Namespace interface {
	// Context returns the application state stored by SetContext. Neither method is
	// synchronized: the application synchronizes calls from more than one goroutine
	// and access to shared mutable state. A disconnect initiated by the application
	// can run callbacks concurrently with handlers.
	Context() interface{}
	// SetContext replaces the value Context returns. It does not synchronize
	// with Context; see Context.
	SetContext(ctx interface{})

	// Namespace returns the name of this namespace, "/" for the root
	// namespace.
	Namespace() string
	// Connected is true from namespace CONNECT until its socket or connection closes.
	Connected() bool
	// Emit sends eventName with arguments v to the server. If the last element of v
	// is a func, it is not sent but registered as the acknowledgement callback: the
	// server's ACK arguments are decoded into its parameters and it is called on the
	// read goroutine of the connection. The callback has no timeout: it is called
	// when the ACK arrives, and never if this generation ends first. ACK arguments
	// that do not decode into its parameters are reported to OnError without calling
	// the callback, and the connection ends with forced close unless the error is a
	// *json.UnmarshalTypeError (see Socket.OnEvent). Encoding is synchronous and may invoke
	// application marshalers. Queue admission does not wait for free capacity;
	// transport writes run in the background. A queue already holding
	// WithWriteBufferSize packets not yet started drops the packet and every
	// later one and closes the connection (see ErrWriteBufferFull). After this
	// namespace disconnects, Emit drops new packets without a report. Retaining a
	// Conn does not buffer sends for a replacement generation; use Client.Socket.
	//
	// These names are never sent: connect, connect_error, connect_timeout,
	// connecting, disconnect, disconnecting, error, reconnect, reconnect_attempt,
	// reconnect_error, reconnect_failed, reconnecting, ping and pong. socket.io-client
	// keeps all of them local except disconnecting, which the server role reserves.
	// Emit of such a name calls this namespace's On, Once and OnEvent
	// listeners of that name synchronously with this Conn and v, a trailing func
	// included, and changes no connection state. A listener error goes to OnError.
	// While one such Emit runs its listeners, another one on this Conn, for
	// example from a listener, does nothing.
	Emit(eventName string, v ...interface{})

	// Volatile starts a single-socket emit that drops when its connection is not writable.
	Volatile() Emitter
	// Compress chooses per-packet compression; the default is true.
	Compress(on bool) Emitter
	// Send is Emit("message", args...), including a trailing acknowledgement callback.
	Send(args ...interface{})
	// Disconnect closes this namespace with reason "io client disconnect";
	// other namespaces stay connected. It does nothing after this generation ends.
	Disconnect()
}

// Emitter applies immutable delivery flags to events sent to the server.
// Each builder returns a new value, so an Emitter can be reused within the Conn's
// generation. It cannot send through a replacement namespace or connection.
// Emitter is implemented by the library, and a minor release may add methods to it.
type Emitter interface {
	// Volatile drops the packet, instead of queueing or buffering it, when the
	// namespace is not connected, the transport is not writable or packets are
	// still waiting to be written.
	Volatile() Emitter
	// Compress chooses per-packet compression; the default is true.
	Compress(on bool) Emitter
	// Emit uses the same encoding, generation and queue-admission rules as Conn.Emit
	// and keeps the same names local, whatever the flags.
	Emit(event string, args ...interface{})
	// Send is Emit("message", args...).
	Send(args ...interface{})
}

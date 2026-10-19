package client

import (
	"net"
	"net/http"
	"net/url"
)

// AckFunc acknowledges an incoming event once, possibly from another goroutine
// after the handler returns. It does nothing unless the peer requested an ACK.
// A handler with this final parameter owns the ACK; return values are ignored.
type AckFunc func(args ...interface{})

// Conn is one connection generation of a client-side Socket.IO namespace.
// Sends and lifecycle operations stay bound to that generation. Use Client.Socket
// to retain a namespace across reconnections and buffer events during recovery.
type Conn interface {
	// Close sends DISCONNECT for each connected namespace and reports
	// "io client disconnect". It runs disconnect callbacks before returning;
	// queued packets drain in the background, bounded by WithCloseTimeout (one minute by
	// default). Queue overflow or an Engine.IO failure discards queued packets.
	// A Conn whose namespace has disconnected does nothing; it cannot close a
	// replacement namespace or connection.
	Close() error
	Namespace

	// ID returns this namespace socket's ID.
	ID() string
	URL() url.URL
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	RemoteHeader() http.Header
}

// Namespace exposes client operations on one Socket.IO namespace.
type Namespace interface {
	// Context returns application state associated with this connection.
	// Synchronize access when callbacks or application goroutines share mutable state;
	// a disconnect initiated by the application can run callbacks concurrently.
	Context() interface{}
	SetContext(ctx interface{})

	Namespace() string
	// Connected is true from namespace CONNECT until its socket or connection closes.
	Connected() bool
	// Emit sends eventName with arguments v to the server. If the last element of v
	// is a func, it is not sent but registered as the acknowledgement callback: the
	// server's ACK arguments are decoded into its parameters and it is called on the
	// read goroutine of the connection. Encoding is synchronous and may invoke
	// application marshalers. Queue admission does not wait for free capacity;
	// transport writes run in the background. A queue already holding
	// WithWriteBufferSize packets not yet started drops the packet and every
	// later one and closes the connection (see ErrWriteBufferFull). After this
	// namespace disconnects, Emit drops new packets without a report. Retaining a
	// Conn does not buffer sends for a replacement generation; use Client.Socket.
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
type Emitter interface {
	Volatile() Emitter
	// Compress chooses per-packet compression; the default is true.
	Compress(on bool) Emitter
	// Emit uses the same encoding, generation and queue-admission rules as Conn.Emit.
	Emit(event string, args ...interface{})
	// Send is Emit("message", args...).
	Send(args ...interface{})
}

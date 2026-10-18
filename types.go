package socketio

import (
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/engineio"
)

// AckFunc acknowledges an incoming event once, possibly from another goroutine
// after the handler returns. It does nothing unless the peer requested an ACK.
// A handler with this final parameter owns the ACK; return values are ignored.
type AckFunc func(args ...interface{})

// Handshake describes the original engine handshake and this namespace's query.
type Handshake struct {
	// Headers are copied from the original handshake request.
	Headers http.Header
	// Time is this namespace socket's creation time (reference time and issued).
	Time time.Time
	// Address is the original request's remote address, including its port.
	Address string
	// XDomain reports a nonempty Origin header on the original request.
	XDomain bool
	// Secure reports TLS on the original request.
	Secure bool
	// URL is the original handshake URL, even after a transport upgrade.
	URL url.URL
	// Query merges the request query and namespace CONNECT query; the latter wins.
	Query url.Values
}

// Conn is a server-side Socket.IO namespace socket.
type Conn interface {
	adapter.Socket
	// Close closes the connection and returns nil; only the first close, by Close or by the library,
	// has an effect. For every connected namespace it emits disconnecting while rooms are joined,
	// writes a DISCONNECT packet, leaves its rooms, and calls OnDisconnect with
	// "server namespace disconnect". It returns without waiting.
	// The packets queued until then, Emits from OnDisconnect included, are written in the background;
	// an Emit that finds the queue full, or comes later, is dropped without a report. Then, or
	// WithCloseTimeout (one minute if not positive) after Close started, the rest is
	// discarded and the engine.io connection is closed; Server.Count counts the session until then.
	// Incoming packets are no longer dispatched, and a read or write failure ends the drain at once.
	// The library closes a connection on a read, decode or dispatch error, a peer close, a ping
	// timeout or an overflow (see ErrWriteBufferFull), discarding the queue at once.
	// Other than an overflow, the error is reported to OnError before the close's effects run,
	// so Emits from OnError still queue. A middleware or OnConnect rejection sends ERROR and keeps
	// the engine connection open; it reports to that namespace's OnError, with a nil Conn for root.
	// Failures during a close are not reported. An encode failure reports OnError, sends an ERROR
	// packet with "encode error", and keeps the connection open.
	Close() error
	Namespace
	// EngineConn returns the underlying server Engine.IO connection.
	EngineConn() engineio.Conn
	// Request returns the original handshake request. Its Body is consumed and its
	// Context ends when that HTTP request returns; retain it for metadata only.
	// It is available before middleware/handlers and after close, and nil on engines
	// without a Request method.
	Request() *http.Request
	// Handshake returns metadata built before this socket's middleware and handlers.
	// It uses the original request even after an upgrade.
	Handshake() Handshake

	// ID returns session id
	ID() string
	URL() url.URL
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	RemoteHeader() http.Header
}

// Namespace describes a communication channel that allows you to split the logic of your application
// over a single shared connection.
type Namespace interface {
	// Context of this connection. You can save one context for one
	// connection, and share it between all handlers. The handlers
	// are called in one goroutine, so no need to lock context if it
	// only accessed in one connection.
	Context() interface{}
	SetContext(ctx interface{})

	Namespace() string
	// Connected is true from namespace CONNECT until its socket or connection closes.
	Connected() bool
	// Emit sends eventName with arguments v to the client. If the last element of v
	// is a func, it is not sent but registered as the acknowledgement callback: the
	// client's ACK arguments are decoded into its parameters and it is called on the
	// read goroutine of the connection. Emit never blocks: it queues the packet, and Emits
	// from OnConnect are written once the writer starts. A queue already holding
	// WithWriteBufferSize packets not yet started drops the packet and every
	// later one and closes the connection (see ErrWriteBufferFull). Once a close started,
	// Emit drops the packet without a report, except that Close still queues packets until
	// the OnDisconnect calls it runs have returned (see Conn.Close).
	Emit(eventName string, v ...interface{})

	// Join joins each room in order, ignoring rooms already joined.
	Join(rooms ...string)
	// To broadcasts to a union of rooms except this socket.
	To(rooms ...string) Emitter
	// Broadcast targets the namespace except this socket.
	Broadcast() Emitter
	// Volatile starts a single-socket emit that drops when its connection is not writable.
	Volatile() Emitter
	// Compress chooses per-packet compression; the default is true.
	Compress(on bool) Emitter
	Leave(room string)
	LeaveAll()
	Rooms() []string
	// Send is Emit("message", args...), including a trailing acknowledgement callback.
	Send(args ...interface{})
	// Disconnect writes DISCONNECT for this namespace, leaves its rooms and calls
	// OnDisconnect with the server namespace reason. Other namespaces stay open.
	Disconnect()
	// Use appends event middleware on this server socket.
	Use(func(event string, args []interface{}) error)
}

// Emitter is an immutable pending target and its flags. Each builder returns a new
// value, so an Emitter can be reused. To or Except converts a single-socket target
// to a broadcast excluding its sender; Volatile, Local and Compress preserve its target.
type Emitter interface {
	To(rooms ...string) Emitter
	Except(ids ...string) Emitter
	Volatile() Emitter
	// Compress chooses per-packet compression; the default is true.
	Compress(on bool) Emitter
	Local() Emitter
	// Emit returns ErrBroadcastAck without sending for a broadcast with a trailing
	// func, or an adapter error. A dynamic parent may have delivered to earlier
	// children before a child returns an error. A single-socket emit never fails;
	// invalid broadcast options return an adapter error.
	Emit(event string, args ...interface{}) error
	// Send is Emit("message", args...).
	Send(args ...interface{}) error
}

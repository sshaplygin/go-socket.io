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
// after the handler returns. It does nothing unless the peer requested an ACK
// and the originating namespace connection is still active.
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

// Conn is one connection generation of a server-side Socket.IO namespace.
// Direct sends and room operations remain bound to that namespace socket, even
// if a later connection to the namespace reuses its ID.
//
// Emit, Send, the emitters, Join, Leave, LeaveAll, Rooms, Disconnect and Close may
// be called from any goroutine, concurrently with each other and with handlers;
// SetContext and Context are not synchronized. Conn is implemented by the library,
// and a minor release may add methods to it.
type Conn interface {
	adapter.Socket
	// Close closes the connection and returns nil; only the first close, by Close or by the library,
	// has an effect. For every connected namespace it emits disconnecting while rooms are joined,
	// writes a DISCONNECT packet, leaves its rooms, and calls OnDisconnect with
	// "server namespace disconnect". It returns without waiting for the transport drain.
	// The packets queued until then, Emits from OnDisconnect included, are written in the background;
	// an Emit that finds the queue full, or comes later, is dropped without a report. Then, or
	// WithCloseTimeout (one minute if not positive) after Close started, the rest is
	// discarded and the engine.io connection is closed; Server.Count counts the session until then.
	// Incoming packets are no longer dispatched, and a read or write failure ends the drain at once.
	// The library itself closes a connection, discarding the queue at once, when its Engine.IO
	// session ends (a peer close, a transport read or write failure, a ping timeout or a message
	// past the size limit), on an overflow (see ErrWriteBufferFull), and when a received packet
	// stops the connection's reader, for example a packet that is not valid, or arguments that
	// fail to decode into the parameters of an untyped event handler (OnEvent, On, Once) or of
	// an Emit acknowledgement callback with an error that is not a *json.UnmarshalTypeError
	// (see Server.OnEvent). The end of the session is not an error: OnDisconnect receives its
	// reason and OnError is not called. A received packet that stops the reader is reported to
	// OnError while the connection is still connected; the close follows, so an Emit from that
	// OnError call can be discarded before it is written. A decode error of such arguments that
	// is a *json.UnmarshalTypeError is reported to OnError without a close.
	// A middleware or OnConnect rejection sends ERROR and keeps the engine connection open; it
	// reports to that namespace's OnError, with a nil Conn for root. Failures during a close are
	// not reported. An encode failure reports OnError, sends an ERROR packet with "encode error",
	// and keeps the connection open.
	Close() error
	Namespace
	// EngineConn returns the underlying server Engine.IO connection for metadata
	// such as ID, Transport, Upgraded, ReadyState and RemoteAddr, and for Close.
	// Socket.IO owns the connection's frame I/O: applications must not call
	// NextReader, NextWriter or NextWriterCompress, which would interfere with the
	// library's reader and writer and corrupt the packet stream.
	EngineConn() engineio.Conn
	// Request returns the original handshake request. Its Body is consumed and its
	// Context ends when that HTTP request returns; retain it for metadata only.
	// It is available before middleware/handlers and after close, and nil on engines
	// without a Request method.
	Request() *http.Request
	// Handshake returns metadata built before this socket's middleware and handlers.
	// It uses the original request even after an upgrade.
	Handshake() Handshake

	// ID returns the Engine.IO ID in the root namespace, or
	// <namespace>#<Engine.IO ID> in any other namespace.
	ID() string
	// URL returns the request URL of the current Engine.IO transport, which
	// changes with a transport upgrade; Handshake keeps the original URL. The
	// result is a url.URL value, so calling a pointer method such as String needs
	// a variable: u := c.URL(); u.String().
	URL() url.URL
	// LocalAddr returns the local network address of the current transport; over
	// polling it is a polling.Addr, which is not the address of a network connection.
	LocalAddr() net.Addr
	// RemoteAddr returns the peer's network address on the current transport.
	RemoteAddr() net.Addr
	// RemoteHeader returns the request headers of the current transport;
	// Handshake keeps the headers of the original request.
	RemoteHeader() http.Header
}

// Namespace exposes server operations on one Socket.IO namespace socket. It is
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

	// Namespace returns the name of this socket's namespace, "/" for the root
	// namespace.
	Namespace() string
	// Connected is true from namespace CONNECT until its socket or connection closes.
	Connected() bool
	// Emit sends eventName with arguments v to the client. If the last element of v
	// is a func, it is not sent but registered as the acknowledgement callback: the
	// client's ACK arguments are decoded into its parameters and it is called on the
	// read goroutine of the connection. The callback has no timeout: it is called
	// when the ACK arrives, and never if its namespace connection ends first. ACK
	// arguments that do not decode into its parameters are reported to OnError
	// without calling the callback, and the connection closes unless the error is a
	// *json.UnmarshalTypeError (see Server.OnEvent). Encoding is synchronous and may invoke
	// application marshalers. Queue admission does not wait for free capacity;
	// transport writes run in the background. Emits from OnConnect are written
	// after the namespace is accepted. A queue already holding
	// WithWriteBufferSize packets not yet started drops the packet and every
	// later one and closes the connection (see ErrWriteBufferFull). Once a close started,
	// Emit drops the packet without a report, except that Close still queues packets until
	// the OnDisconnect calls it runs have returned (see Conn.Close).
	// A disconnected Conn cannot send to a replacement namespace with the same ID.
	Emit(eventName string, v ...interface{})

	// Join joins each room in order, ignoring rooms already joined. After namespace
	// room cleanup, it does nothing, even if a replacement socket reuses the ID.
	Join(rooms ...string)
	// To broadcasts to a union of rooms except this socket.
	To(rooms ...string) Emitter
	// Broadcast targets the namespace except this socket.
	Broadcast() Emitter
	// Volatile starts a single-socket emit that drops when its connection is not writable.
	Volatile() Emitter
	// Compress chooses per-packet compression; the default is true.
	Compress(on bool) Emitter
	// Leave leaves a room; it does nothing after namespace room cleanup.
	Leave(room string)
	// LeaveAll leaves all rooms; it does nothing after namespace room cleanup.
	LeaveAll()
	// Rooms returns this socket's rooms, including during disconnecting callbacks.
	// After room cleanup it returns nil, without querying a replacement socket.
	Rooms() []string
	// Send is Emit("message", args...), including a trailing acknowledgement callback.
	Send(args ...interface{})
	// Disconnect writes DISCONNECT for this namespace, leaves its rooms and calls
	// OnDisconnect with the server namespace reason. Other namespaces stay open.
	Disconnect()
	// Use appends incoming event middleware on this server socket.
	// For namespace connection middleware, see ServerNamespace.Use and Server.Use.
	Use(func(event string, args []interface{}) error)
}

// Emitter is an immutable pending target and its flags. Each builder returns a new
// value, so an Emitter can be reused. To or Except converts a single-socket target
// to a broadcast excluding its sender; Volatile, Local and Compress preserve its target.
// A single-socket target stays bound to its Conn's namespace generation.
// Emitter is implemented by the library, and a minor release may add methods to it.
type Emitter interface {
	// To adds rooms to the target: the broadcast reaches each socket in any of
	// the rooms once. A broadcast without rooms reaches every socket of the
	// namespace that is in at least one room; the server joins each socket to
	// the room of its own ID, so that is every socket that has not left it.
	To(rooms ...string) Emitter
	// Except excludes the sockets with these Conn.ID values. With an adapter
	// that cannot exclude sockets, Emit of any broadcast that excludes sockets
	// returns an error wrapping adapter.ErrExceptUnsupported, and that adapter
	// sends nothing (for a dynamic parent, see Emit); a broadcast that starts
	// from a Conn always excludes its sender, so this applies to it without
	// Except.
	Except(ids ...string) Emitter
	// Volatile drops the packet, instead of queueing it, for each target
	// connection whose transport is not writable or that still has packets
	// waiting to be written.
	Volatile() Emitter
	// Compress chooses per-packet compression; the default is true.
	Compress(on bool) Emitter
	// Local restricts a broadcast to the sockets of this server instance: a
	// clustered adapter, such as the Redis adapter, does not publish it. It
	// does not change a single-socket emit.
	Local() Emitter
	// Emit returns ErrBroadcastAck without sending for a broadcast with a trailing
	// func, or an adapter error. A dynamic parent may have delivered to earlier
	// children before a child returns an error. Invalid broadcast options return
	// an adapter error. A single-socket emit returns nil; this does not acknowledge
	// delivery, and encoding errors and dropped packets follow Conn.Emit's rules.
	Emit(event string, args ...interface{}) error
	// Send is Emit("message", args...).
	Send(args ...interface{}) error
}

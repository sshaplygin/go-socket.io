// Package adapter owns namespace room selection and socket delivery contracts.
package adapter

import (
	"errors"
)

// EachFunc visits one local namespace socket.
type EachFunc func(Socket)

// Options selects the namespace sockets and flags of one broadcast.
// The zero value targets every socket of the namespace.
type Options struct {
	// Rooms selects a union of rooms, once per socket. Empty means every socket.
	Rooms []string
	// Except lists socket IDs that receive nothing.
	Except []string
	// Volatile skips sockets whose connection cannot take the packet now.
	Volatile bool
	// Local restricts delivery to this instance without publishing to a cluster.
	Local bool
	// NoCompress sends this instance's packet uncompressed; compression is enabled by default.
	NoCompress bool
}

// Socket is the minimal namespace socket used by adapters.
// EmitWithOptions applies Volatile and NoCompress to this socket only; room
// selection, exclusion and cluster routing belong to the adapter.
type Socket interface {
	ID() string
	Emit(event string, args ...interface{})
	EmitWithOptions(opts Options, event string, args ...interface{})
}

// Factory constructs one namespace adapter per name. The server coordinates
// callers for the same name while unrelated factories may run concurrently.
// Construction errors are retained by the namespace. Server.Serve reports errors
// recorded before it starts. A failed namespace rejects connections and its
// emitter returns the construction error, including failures after Serve starts.
type Factory func(namespace string) (Adapter, error)

// ErrExceptUnsupported means the adapter cannot exclude sockets. Nothing is sent.
var ErrExceptUnsupported = errors.New("socketio: broadcast except is not supported by this adapter")

// Adapter manages namespace rooms and delivery. Its methods must support
// concurrent calls from handlers and Server methods. Delivery invokes each target
// only after releasing the adapter's lock.
type Adapter interface {
	// SendTo snapshots matching sockets and emits on the caller's goroutine after
	// releasing the adapter lock. Delivery uses target.Emit, or target.EmitWithOptions
	// when Volatile or NoCompress is set. An adapter
	// unable to exclude returns an error wrapping
	// ErrExceptUnsupported before sending anything.
	SendTo(opts Options, event string, args ...interface{}) error
	// IDs returns a copy of the IDs in any of rooms, or all sockets when empty.
	IDs(rooms ...string) []string
	// Join adds the socket to a room.
	Join(room string, connection Socket)
	// Leave removes the socket from a room.
	Leave(room string, connection Socket)
	// LeaveAll removes the socket from every room.
	LeaveAll(connection Socket)
	// Clear removes all sockets from the room.
	Clear(room string)
	// Send emits to a room. Memory registers a trailing callback per socket;
	// Redis rejects it before delivery and logs failures at WARN.
	Send(room, event string, args ...interface{})
	// SendAll emits to all sockets. Memory registers a trailing callback per socket;
	// Redis rejects it before delivery and logs failures at WARN.
	SendAll(event string, args ...interface{})
	// ForEach visits each local member of the room.
	ForEach(room string, f EachFunc)
	// Len counts connections in the room; clustered adapters may query peers.
	Len(room string) int
	// Rooms lists this socket's rooms; nil is equivalent to AllRooms.
	Rooms(connection Socket) []string
	// AllRooms lists all rooms of the namespace, cluster-wide for Redis.
	AllRooms() []string
}

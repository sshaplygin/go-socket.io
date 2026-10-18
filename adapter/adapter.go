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

// Factory constructs a namespace adapter once under the server's creation lock.
// Construction errors are retained by the namespace and reported by Server.Serve.
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
	Join(room string, connection Socket)          // Join causes the connection to join a room
	Leave(room string, connection Socket)         // Leave causes the connection to leave a room
	LeaveAll(connection Socket)                   // LeaveAll causes given connection to leave all rooms
	Clear(room string)                            // Clear causes removal of all connections from the room
	Send(room, event string, args ...interface{}) // Send emits to a room; a trailing callback is registered per socket.
	SendAll(event string, args ...interface{})    // SendAll emits to all sockets; a trailing callback is registered per socket.
	ForEach(room string, f EachFunc)              // ForEach visits each local member of the room.
	Len(room string) int                          // Len gives number of connections in the room
	Rooms(connection Socket) []string             // Gives list of all the rooms if no connection given, else list of all the rooms the connection joined
	AllRooms() []string                           // Gives list of all the rooms the connection joined
}

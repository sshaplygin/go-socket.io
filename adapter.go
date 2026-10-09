package socketio

import (
	"context"
	"encoding/json"

	"github.com/sshaplygin/go-socket.io/parser"
)

// Adapter keeps room membership of one namespace and delivers broadcasts. The
// in-memory implementation lands in 2.2 in this package; external adapters import
// this package and the root never imports them.
//
// Room mutations and SocketRooms are local; Sockets may query the cluster. An empty
// room filter selects all. Broadcast unions rooms, deduplicates recipients, applies
// exclusions and returns partial progress together with any error; Published means
// broker acceptance, never remote delivery or acknowledgement; a Local broadcast never
// publishes. Sockets and FetchSockets may return deduplicated partial data with a
// deadline error. ServerSideEmit success means publication was accepted. Implementations
// copy borrowed input they retain and return owned snapshots. Close releases adapter
// workers and subscriptions, never an application-owned broker client.
type Adapter interface {
	AddAll(sid SocketID, rooms []Room)
	Del(sid SocketID, room Room)
	DelAll(sid SocketID)
	Broadcast(ctx context.Context, pkt parser.Packet, opts BroadcastOptions) (BroadcastResult, error)
	Sockets(ctx context.Context, rooms []Room) ([]SocketID, error)
	SocketRooms(sid SocketID) []Room
	FetchSockets(ctx context.Context, opts BroadcastOptions) ([]RemoteSocket, error)
	ServerSideEmit(ctx context.Context, event string, args ...any) error
	Close() error
}

// AdapterFactory builds the adapter of nsp. The server calls it from Server.Namespace
// with its own context, which is cancelled when Shutdown or Close begins. That context
// bounds only the factory call: an adapter derives no lifetime from it and lives until
// Adapter.Close.
type AdapterFactory func(ctx context.Context, nsp *Namespace) (Adapter, error)

// BroadcastOptions selects the recipients of Adapter.Broadcast and FetchSockets.
type BroadcastOptions struct {
	Rooms, Except []Room
	Flags         BroadcastFlags
}

// BroadcastFlags is the minimal flag set. Proposed and unreviewed: see docs/API.md,
// Open before G2. Local restricts the operation to the
// server's own sockets. The other flags of the Node adapter (volatile, compress,
// timeout) are not part of this contract.
type BroadcastFlags struct{ Local bool }

// RemoteSocket is an owned metadata snapshot of a socket, possibly on another node.
// Proposed and unreviewed: Handshake and Data exposure, including auth and header
// redaction, is open (docs/API.md, Open before G2).
// Handshake and Data are JSON values; producers copy every slice, and mutating a
// snapshot must not change adapter state.
type RemoteSocket struct {
	ID        SocketID
	Rooms     []Room
	Handshake json.RawMessage
	Data      json.RawMessage
}

package socketio

import (
	"context"
	"encoding/json"

	"github.com/sshaplygin/go-socket.io/experiments/v2-api/parser"
)

// Adapter is the roadmap 2.2 contract, not a runtime implementation.
// Room mutations and SocketRooms are local; Sockets may query the cluster.
// Empty room filters select all. Broadcast unions rooms, deduplicates recipients,
// applies room exclusions and returns partial progress with any error. Published
// means broker acceptance, never remote delivery or acknowledgement. Local never
// publishes. FetchSockets and Sockets may return partial data with a deadline error.
// Implementations must copy borrowed input they retain and return owned snapshots.
// Close releases adapter workers, never an application-owned broker client.
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

// AdapterFactory constructs a namespace-owned adapter; the root never imports
// external adapters. Runtime construction and cleanup are outside this proof.
type AdapterFactory func(nsp *Namespace) (Adapter, error)

type BroadcastOptions struct {
	Rooms, Except []Room
	Flags         BroadcastFlags
}

// BroadcastFlags is a minimal proposal, not the complete Node flags surface.
// Other flags require an explicit Go contract before G2; no implicit defaults for
// compression, volatile delivery or cluster acknowledgements are claimed here.
type BroadcastFlags struct{ Local bool }

// RemoteSocket proposes an owned metadata snapshot, not a live remote endpoint.
// Handshake and Data are lazy JSON, not arbitrary Node values or binary graphs.
// Their mapping (including sensitive auth/header data) must be finalized before G2.
// Runtime producers must copy all slices; this declaration performs no copying.
type RemoteSocket struct {
	ID        SocketID
	Rooms     []Room
	Handshake json.RawMessage
	Data      json.RawMessage
}

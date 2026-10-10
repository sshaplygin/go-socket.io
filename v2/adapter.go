package socketio

import (
	"context"
	"encoding/json"

	"github.com/sshaplygin/go-socket.io/v2/parser"
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

// LocalSockets is the seam through which an adapter reaches the sockets of its own
// server. A namespace hands it out through Namespace.LocalSockets; an adapter that
// receives a broadcast from a peer selects local recipients from its own room
// membership and calls Deliver for each. The memory adapter takes it as a constructor
// argument, so its conformance tests run against a test double before the runtime exists.
type LocalSockets interface {
	// Deliver queues pkt on the local socket sid and returns nil only when it was
	// enqueued; BroadcastResult.LocalRecipients counts those successes. A socket that
	// is gone yields an error matching ErrSocketClosed, a full queue ErrWriteBufferFull.
	Deliver(ctx context.Context, sid SocketID, pkt parser.Packet) error
	// Snapshot returns the RemoteSocket of the local socket sid, with the Handshake
	// already passed through RedactHandshake as the RemoteSocket godoc requires; false
	// when the socket is gone.
	Snapshot(sid SocketID) (RemoteSocket, bool)
}

// BroadcastOptions selects the recipients of Adapter.Broadcast and FetchSockets.
type BroadcastOptions struct {
	Rooms, Except []Room
	Flags         BroadcastFlags
}

// BroadcastFlags selects how far a broadcast or query reaches. Local restricts the
// operation to the server's own sockets; a local operation never publishes. The
// other flags of the Node adapter (volatile, compress, timeout) are not part of this
// contract: a later stage may add them as further fields, so construct the struct
// with field names.
type BroadcastFlags struct{ Local bool }

// RemoteSocket is an owned metadata snapshot of a socket, possibly on another node,
// shaped like an entry of Node's fetchSockets: ID, Rooms, Handshake and Data.
//
// Handshake is a JSON object with the Node key names (headers, time, address, xdomain,
// secure, issued, url, query). The guarantee is exactly this: no auth key, and no
// authorization, cookie or proxy-authorization entry in headers (names compared
// without case); the producer drops them, which for a snapshot decoded from a peer is
// the adapter that decoded it, by calling RedactHandshake. Nothing else is redacted: url, query, address and every
// other header may carry credentials and are passed through as Node does. Data is the slot of Node's socket.data: nil
// means none, otherwise valid JSON; binary values are not representable. A producer
// that cannot encode Data returns the entries it can and an error. How an application
// sets Data is defined by stage 2.3S. Producers copy every slice, and mutating a snapshot must not change
// adapter state.
type RemoteSocket struct {
	ID        SocketID
	Rooms     []Room
	Handshake json.RawMessage
	Data      json.RawMessage
}

// RedactHandshake returns a copy of the Handshake object h without the auth key and
// without the authorization, cookie and proxy-authorization entries of headers (names
// compared without case); every other key and header is kept byte for byte. It is the
// one implementation of the RemoteSocket guarantee: the server uses it for local
// snapshots and an adapter calls it on a Handshake decoded from a peer, because a Node
// peer sends the omitted values. nil yields nil; h that is not a JSON object yields an
// error. It lives in the root package, which adapters already import and which the
// graph allows to call it without an import of adapter/...; the skeleton returns
// ErrNotImplemented and stage 2.2 writes the body.
func RedactHandshake(h json.RawMessage) (json.RawMessage, error) { return nil, ErrNotImplemented }

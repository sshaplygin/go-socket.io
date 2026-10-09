// Package externaladapter is a compile-only stand-in for an external adapter
// module. It imports root contracts; root must never import this package.
// Factory and methods must not be used as a functioning adapter.
package externaladapter

import (
	"context"
	"encoding/json"

	sio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/parser"
)

type Adapter struct{}

var _ sio.Adapter = (*Adapter)(nil)
var _ sio.AdapterFactory = New

// New has the AdapterFactory signature, including the ctx of the readiness contract.
func New(ctx context.Context, n *sio.Namespace) (sio.Adapter, error) {
	_ = ctx
	// Prove external modules can reach namespace metadata without a reverse import.
	_ = n.Hooks()
	_ = n.Logger()
	// ...and the local sockets they deliver a received broadcast to.
	_ = n.LocalSockets()
	return nil, sio.ErrNotImplemented
}

// The roadmap membership methods have no error return. These compile-only mock
// methods panic with the sentinel rather than pretend a mutation succeeded.
func (*Adapter) AddAll(sio.SocketID, []sio.Room)     { panic(sio.ErrNotImplemented) }
func (*Adapter) Del(sio.SocketID, sio.Room)          { panic(sio.ErrNotImplemented) }
func (*Adapter) DelAll(sio.SocketID)                 { panic(sio.ErrNotImplemented) }
func (*Adapter) SocketRooms(sio.SocketID) []sio.Room { panic(sio.ErrNotImplemented) }
func (*Adapter) Broadcast(context.Context, parser.Packet, sio.BroadcastOptions) (sio.BroadcastResult, error) {
	return sio.BroadcastResult{}, sio.ErrNotImplemented
}
func (*Adapter) Sockets(context.Context, []sio.Room) ([]sio.SocketID, error) {
	return nil, sio.ErrNotImplemented
}
func (*Adapter) FetchSockets(context.Context, sio.BroadcastOptions) ([]sio.RemoteSocket, error) {
	return nil, sio.ErrNotImplemented
}
func (*Adapter) ServerSideEmit(context.Context, string, ...any) error { return sio.ErrNotImplemented }
func (*Adapter) Close() error                                         { return sio.ErrNotImplemented }

// Local is a test double of the namespace side of the seam, as the conformance suite
// of the memory adapter uses it before the runtime exists.
type Local struct{}

var _ sio.LocalSockets = (*Local)(nil)

func (*Local) Deliver(context.Context, sio.SocketID, parser.Packet) error { return nil }
func (*Local) Snapshot(sio.SocketID) (sio.RemoteSocket, bool)             { return sio.RemoteSocket{}, false }

// FromPeer builds a RemoteSocket from the fields of a snapshot decoded off the wire
// (the decoding is adapter/codec's job and imports no root type). The adapter, not the
// codec, calls the one redaction helper, which lives in the root package.
func FromPeer(id sio.SocketID, rooms []sio.Room, handshake, data json.RawMessage) (sio.RemoteSocket, error) {
	h, err := sio.RedactHandshake(handshake)
	if err != nil {
		return sio.RemoteSocket{}, err
	}
	return sio.RemoteSocket{ID: id, Rooms: rooms, Handshake: h, Data: data}, nil
}

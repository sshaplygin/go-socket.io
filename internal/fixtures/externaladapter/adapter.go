// Package externaladapter is a compile-only stand-in for an external adapter
// module. It imports root contracts; root must never import this package.
// Factory and methods must not be used as a functioning adapter.
package externaladapter

import (
	"context"

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

package socketio

import (
	"context"

	"github.com/sshaplygin/go-socket.io/v2/parser"
)

// Room names a set of sockets of one namespace.
type Room string

// SocketID identifies a socket. It aliases Room because every socket is
// automatically in the room named by its ID, so the ID can select that room, as in
// nsp.To("room").Except(s.ID()). The alias is part of the contract: SocketID and Room
// are the same type and cannot be separated later without a breaking change.
type SocketID = Room

// Socket is one client's connection to one namespace. It implements Endpoint. The
// skeleton holds no session state and no Socket is created yet.
type Socket struct{ id SocketID }

var _ Endpoint = (*Socket)(nil)

// ID returns the socket ID, or "" for a nil Socket.
func (s *Socket) ID() SocketID {
	if s == nil {
		return ""
	}
	return s.id
}

// SendPacket implements Endpoint. The skeleton always returns ErrNotImplemented.
func (*Socket) SendPacket(context.Context, parser.Packet) error { return ErrNotImplemented }

// RequestAck implements Endpoint. The skeleton always returns ErrNotImplemented.
func (*Socket) RequestAck(context.Context, parser.Packet) (parser.Arguments, error) {
	return parser.Arguments{}, ErrNotImplemented
}

// Join adds the socket to rooms. The skeleton always returns ErrNotImplemented.
func (*Socket) Join(...Room) error { return ErrNotImplemented }

// Leave removes the socket from rooms. The skeleton always returns ErrNotImplemented.
func (*Socket) Leave(...Room) error { return ErrNotImplemented }

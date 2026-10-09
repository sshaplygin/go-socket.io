// Package clientstub is a compile-only stand-in for the Go client of roadmap 2.3C.
// It proves that a package outside the root can implement socketio.ClientRegistration,
// so the root never has to import the client. Its methods return
// socketio.ErrNotImplemented and it must not be used as a client.
package clientstub

import (
	"context"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/parser"
)

// Client implements socketio.ClientRegistration.
type Client struct{}

var _ socketio.ClientRegistration = (*Client)(nil)

func (*Client) SendPacket(context.Context, parser.Packet) error { return socketio.ErrNotImplemented }

func (*Client) RequestAck(context.Context, parser.Packet) (parser.Arguments, error) {
	return parser.Arguments{}, socketio.ErrNotImplemented
}

func (*Client) RegisterEvent(string, socketio.ClientRawHandler) error {
	return socketio.ErrNotImplemented
}

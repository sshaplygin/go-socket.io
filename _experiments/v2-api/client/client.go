// Package client proves the root API does not import the client implementation.
// Dial and all runtime methods return socketio.ErrNotImplemented.
package client

import (
	"context"
	"encoding/json"

	socketio "github.com/sshaplygin/go-socket.io/experiments/v2-api"
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/parser"
)

type Options struct{ Auth json.RawMessage }
type Client struct{}

var _ socketio.ClientRegistration = (*Client)(nil)

func Dial(context.Context, string, Options) (*Client, error)    { return nil, socketio.ErrNotImplemented }
func (*Client) SendPacket(context.Context, parser.Packet) error { return socketio.ErrNotImplemented }
func (*Client) RequestAck(context.Context, parser.Packet) (parser.Arguments, error) {
	return parser.Arguments{}, socketio.ErrNotImplemented
}
func (*Client) RegisterEvent(string, socketio.ClientRawHandler) error {
	return socketio.ErrNotImplemented
}
func (*Client) OnRaw(socketio.ClientRawHandler) error { return socketio.ErrNotImplemented }
func (*Client) Close() error                          { return socketio.ErrNotImplemented }

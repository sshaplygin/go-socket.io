package client

import (
	"context"

	"github.com/sshaplygin/go-socket.io/internal/event"
)

// RegisterEvent is an internal bridge for socketio.Event and socketio.AckEvent
// descriptors only. Applications register typed handlers through those descriptors.
func (c *Client) RegisterEvent(name string, handler event.Handler) error {
	return c.manager.RegisterEvent(name, handler)
}

// SendEvent is the internal bridge used by socketio.Event.Emit. Applications
// send typed values through the descriptor rather than calling this method.
func (c *Client) SendEvent(ctx context.Context, name string, value event.Payload) error {
	return c.manager.SendEvent(ctx, name, value)
}

// RequestAck is the internal bridge used by socketio.AckEvent.EmitWithAck.
// Applications request typed responses through the descriptor.
func (c *Client) RequestAck(ctx context.Context, name string, value event.Payload, schema event.Schema) (event.Payload, error) {
	return c.manager.RequestAck(ctx, name, value, schema)
}

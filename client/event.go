package client

import (
	"context"

	"github.com/sshaplygin/go-socket.io/internal/event"
)

// RegisterEvent reserves the selected namespace's typed handler atomically.
func (c *Client) RegisterEvent(name string, handler event.Handler) error {
	return c.manager.RegisterEvent(name, handler)
}

// SendEvent queues one immutable typed value in the selected namespace.
func (c *Client) SendEvent(ctx context.Context, name string, value event.Payload) error {
	return c.manager.SendEvent(ctx, name, value)
}

// RequestAck waits for the selected namespace's acknowledged typed response.
func (c *Client) RequestAck(ctx context.Context, name string, value event.Payload, schema event.Schema) (event.Payload, error) {
	return c.manager.RequestAck(ctx, name, value, schema)
}

package consumer

import (
	"context"
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/parser"
)

type payload struct {
	Value  int64
	Binary parser.Buffer
}

func declarations(s *socketio.Server, c *client.Client, server socketio.Conn, peer client.Conn) {
	ctx := context.Background()
	var e socketio.Event[payload] = socketio.NewEvent[payload]("value")
	_ = e.Name()
	_ = e.Handle(s.Of("/chat"), func(context.Context, socketio.Conn, payload) error { return nil })
	_ = e.HandleClient(c, func(context.Context, socketio.Endpoint, payload) error { return nil })
	_ = e.HandleClient(c.Socket("/chat"), func(context.Context, socketio.Endpoint, payload) error { return nil })
	_ = e.Emit(ctx, server, payload{})
	_ = e.Emit(ctx, peer, payload{})
	_ = e.Emit(ctx, c, payload{})
	_ = e.EmitTo(ctx, s.Of("/chat").To("room"), payload{})
	var a socketio.AckEvent[payload, string] = socketio.NewAckEvent[payload, string]("ack")
	_ = a.Name()
	_ = a.Handle(s.Of("/chat"), func(context.Context, socketio.Conn, payload) (string, error) { return "ok", nil })
	_ = a.HandleClient(c, func(context.Context, socketio.Endpoint, payload) (string, error) { return "ok", nil })
	var result string
	result, _ = a.EmitWithAck(ctx, peer, payload{})
	_ = result
	var _ socketio.Endpoint = server
	var _ socketio.Endpoint = peer
	var _ socketio.ClientRegistration = c
	var _ socketio.ClientRegistration = c.Socket("/chat")
	var _ error = &socketio.RemoteError{Code: "refused", Message: "no"}
	var _ socketio.EventHandler
}

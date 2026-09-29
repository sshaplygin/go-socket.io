// Package positive is compiled, never executed. Runtime does not exist yet.
package positive

import (
	"context"
	"encoding/json"

	sio "github.com/sshaplygin/go-socket.io/experiments/v2-api"
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/client"
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/parser"
)

type Message struct {
	Text   string
	File   sio.Binary
	Nested map[string][]sio.Binary
}
type Receipt struct{ ID string }
type Credentials struct{ Token string }

var MessageEvent = sio.NewEvent[Message]("message")
var Send = sio.NewAckEvent[Message, Receipt]("send")
var Pair = sio.NewEvent[sio.Args2[string, sio.Binary]]("pair")
var PairAck = sio.NewAckEvent[string, sio.Args2[string, sio.Binary]]("pair-ack")

func Server(ctx context.Context, n *sio.Namespace, s *sio.Socket) error {
	if err := MessageEvent.Handle(n, func(context.Context, *sio.Socket, Message) error { return nil }); err != nil {
		return err
	}
	if err := Send.Handle(n, func(context.Context, *sio.Socket, Message) (Receipt, error) { return Receipt{}, nil }); err != nil {
		return err
	}
	if err := n.Use(sio.Auth[Credentials](func(context.Context, *sio.Socket, Credentials) error { return nil })); err != nil {
		return err
	}
	if err := n.OnRaw(func(ctx context.Context, _ *sio.Socket, event sio.RawEvent) error {
		if event.Ack != nil {
			return event.Ack.Respond(ctx, parser.Arguments{Values: []json.RawMessage{json.RawMessage(`"ok"`)}})
		}
		return nil
	}); err != nil {
		return err
	}
	if err := MessageEvent.Emit(ctx, s, Message{Text: "hello", File: sio.Binary{1, 2}}); err != nil {
		return err
	}
	receipt, err := Send.EmitWithAck(ctx, s, Message{})
	if err != nil {
		return err
	}
	assertType[Receipt](receipt)
	result, err := MessageEvent.EmitTo(ctx, n.To("room").Except(s.ID()).Local(), Message{})
	if err != nil {
		return err
	}
	assertType[sio.BroadcastResult](result)
	if err := Pair.Handle(n, func(context.Context, *sio.Socket, sio.Args2[string, sio.Binary]) error { return nil }); err != nil {
		return err
	}
	return Pair.Emit(ctx, s, sio.Args2[string, sio.Binary]{First: "file", Second: sio.Binary{0, 255}})
}

func Client(ctx context.Context, c *client.Client) error {
	if err := MessageEvent.HandleClient(c, func(context.Context, sio.Endpoint, Message) error { return nil }); err != nil {
		return err
	}
	if err := Send.HandleClient(c, func(context.Context, sio.Endpoint, Message) (Receipt, error) { return Receipt{}, nil }); err != nil {
		return err
	}
	if err := MessageEvent.Emit(ctx, c, Message{}); err != nil {
		return err
	}
	receipt, err := Send.EmitWithAck(ctx, c, Message{})
	if err != nil {
		return err
	}
	assertType[Receipt](receipt)
	if err := Pair.HandleClient(c, func(context.Context, sio.Endpoint, sio.Args2[string, sio.Binary]) error { return nil }); err != nil {
		return err
	}
	if err := PairAck.HandleClient(c, func(context.Context, sio.Endpoint, string) (sio.Args2[string, sio.Binary], error) {
		return sio.Args2[string, sio.Binary]{}, nil
	}); err != nil {
		return err
	}
	pair, err := PairAck.EmitWithAck(ctx, c, "file")
	if err != nil {
		return err
	}
	assertType[sio.Args2[string, sio.Binary]](pair)
	return Pair.Emit(ctx, c, sio.Args2[string, sio.Binary]{First: "file", Second: sio.Binary{0, 255}})
}

func Construction(ctx context.Context) error {
	s, err := sio.NewServer(sio.Options{AckTimeout: 0})
	if err != nil {
		return err
	}
	_ = s.Namespace("/")
	c, err := client.Dial(ctx, "http://localhost:3000", client.Options{Auth: json.RawMessage(`{"token":"test"}`)})
	if err != nil {
		return err
	}
	return c.Close()
}

// assertType proves assignability to the explicitly supplied result type. Like
// the rest of this fixture it is compiled, never executed.
func assertType[T any](T) {}

package socketio_test

import (
	"context"
	"errors"
	"testing"
	"time"

	sio "github.com/sshaplygin/go-socket.io/experiments/v2-api"
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/client"
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/parser"
)

func TestRuntimeIsExplicitlyUnavailable(t *testing.T) {
	ctx := context.Background()
	n, s, c := &sio.Namespace{}, &sio.Socket{}, &client.Client{}
	e, a := sio.NewEvent[string]("event"), sio.NewAckEvent[string, int]("ack")
	if e.Name() != "event" || a.Name() != "ack" {
		t.Fatal("descriptor name lost")
	}
	server, serverErr := sio.NewServer(sio.Options{})
	cl, dialErr := client.Dial(ctx, "", client.Options{})
	if server != nil || cl != nil {
		t.Fatal("runtime constructors returned usable-looking objects")
	}
	_, ackErr := a.EmitWithAck(ctx, s, "")
	_, broadcastErr := e.EmitTo(ctx, n.To("room").Except(s.ID()), "")
	_, socketAckErr := s.RequestAck(ctx, parser.Packet{})
	_, clientAckErr := c.RequestAck(ctx, parser.Packet{})
	checks := []error{
		serverErr, dialErr, ackErr, broadcastErr, socketAckErr, clientAckErr,
		e.Emit(ctx, s, ""), e.Handle(n, nil), e.HandleClient(c, nil),
		a.Handle(n, nil), a.HandleClient(c, nil), n.Use(nil), n.OnRaw(nil),
		s.SendPacket(ctx, parser.Packet{}), s.Join("room"), s.Leave("room"),
		c.SendPacket(ctx, parser.Packet{}), c.RegisterEvent("", nil), c.OnRaw(nil), c.Close(),
		sio.Auth[string](nil)(ctx, s, nil),
		(&sio.Server{}).Close(), (&sio.Server{}).Shutdown(ctx),
	}
	for i, err := range checks {
		if !errors.Is(err, sio.ErrNotImplemented) {
			t.Errorf("operation %d: got %v", i, err)
		}
	}
}

func TestBoundedDefaults(t *testing.T) {
	got, err := (sio.Options{}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	want := sio.Options{AckTimeout: 30 * time.Second, OutboundQueueGroups: 64, OutboundQueueBytes: 8 << 20, HandlerQueueEvents: 64, HandlerQueueBytes: 8 << 20, MaxPendingAcks: 128, MaxAttachments: 64, MaxEventBytes: 1 << 20, AttachmentTimeout: 10 * time.Second, MaxConcurrentConnects: 4, ConnectTimeout: 10 * time.Second}
	if got != want {
		t.Fatalf("defaults: got %+v want %+v", got, want)
	}
	one := sio.Options{AckTimeout: time.Second, OutboundQueueGroups: 1, OutboundQueueBytes: 1, HandlerQueueEvents: 1, HandlerQueueBytes: 1, MaxPendingAcks: 1, MaxAttachments: 1, MaxEventBytes: 1, AttachmentTimeout: time.Second, MaxConcurrentConnects: 1, ConnectTimeout: time.Second}
	if normalized, err := one.Normalize(); err != nil || normalized != one {
		t.Fatalf("custom limits changed: %+v %v", normalized, err)
	}
	for _, invalid := range []sio.Options{
		{AckTimeout: -1}, {OutboundQueueGroups: -1}, {OutboundQueueBytes: -1}, {HandlerQueueEvents: -1}, {HandlerQueueBytes: -1}, {MaxPendingAcks: -1}, {MaxAttachments: -1}, {MaxEventBytes: -1}, {AttachmentTimeout: -1}, {MaxConcurrentConnects: -1}, {ConnectTimeout: -1},
	} {
		if _, err := invalid.Normalize(); err == nil {
			t.Errorf("accepted negative limit: %+v", invalid)
		}
	}
}

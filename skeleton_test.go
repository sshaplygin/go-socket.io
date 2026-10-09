package socketio_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	sio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/parser"
)

// TestSkeletonRuntimeIsExplicitlyUnavailable pins that nothing in the skeleton
// pretends to work: every operation that needs the runtime reports ErrNotImplemented,
// and the constructors return no usable object.
func TestSkeletonRuntimeIsExplicitlyUnavailable(t *testing.T) {
	ctx := context.Background()
	n, s := &sio.Namespace{}, &sio.Socket{}
	e, a := sio.NewEvent[string]("event"), sio.NewAckEvent[string, int]("ack")
	if e.Name() != "event" || a.Name() != "ack" {
		t.Fatal("descriptor name lost")
	}

	srv, serverErr := sio.NewServer(sio.Options{})
	if srv != nil {
		t.Fatal("NewServer returned a server")
	}
	nsp, nspErr := (*sio.Server)(nil).Namespace(ctx, "/")
	if nsp != nil {
		t.Fatal("Namespace returned a namespace")
	}
	_, ackErr := a.EmitWithAck(ctx, s, "")
	_, broadcastErr := e.EmitTo(ctx, n.To("room").Except(s.ID()), "")
	_, socketAckErr := s.RequestAck(ctx, parser.Packet{})

	checks := map[string]error{
		"NewServer": serverErr, "Namespace": nspErr, "EmitWithAck": ackErr, "EmitTo": broadcastErr,
		"RequestAck": socketAckErr, "Emit": e.Emit(ctx, s, ""),
		"Event.Handle": e.Handle(n, nil), "Event.HandleClient": e.HandleClient(nil, nil),
		"AckEvent.Handle": a.Handle(n, nil), "AckEvent.HandleClient": a.HandleClient(nil, nil),
		"Use": n.Use(nil), "OnRaw": n.OnRaw(nil), "SendPacket": s.SendPacket(ctx, parser.Packet{}),
		"Join": s.Join("room"), "Leave": s.Leave("room"),
		"Auth":     sio.Auth[string](nil)(ctx, s, nil),
		"Shutdown": (*sio.Server)(nil).Shutdown(ctx), "Close": (*sio.Server)(nil).Close(),
	}
	for name, err := range checks {
		if !errors.Is(err, sio.ErrNotImplemented) {
			t.Errorf("%s: got %v, want ErrNotImplemented", name, err)
		}
	}
}

// TestSkeletonNilReceivers pins the nil-safe metadata accessors, whose result must
// not suggest a configured runtime.
func TestSkeletonNilReceivers(t *testing.T) {
	var n *sio.Namespace
	var s *sio.Socket
	if n.Name() != "" || n.Hooks() != nil || n.Logger() != nil || s.ID() != "" {
		t.Fatal("nil Namespace or Socket returned data")
	}
}

// TestErrorIdentities carries the v1 errors.Is contract (ErrWriteBufferFull) to every
// sentinel of the v2 contract: each has its own identity and survives %w wrapping.
func TestErrorIdentities(t *testing.T) {
	all := []error{
		sio.ErrNotImplemented, sio.ErrNamespaceClosed, sio.ErrAckTimeout, sio.ErrWriteBufferFull,
		sio.ErrSocketClosed, sio.ErrTooManyPendingAcks, sio.ErrMessageTooLarge,
		sio.ErrTooManyAttachments, sio.ErrAckIDExhausted,
	}
	for i, err := range all {
		if err == nil {
			t.Fatalf("sentinel %d is nil", i)
		}
		for j, other := range all {
			if got := errors.Is(err, other); got != (i == j) {
				t.Errorf("errors.Is(%d, %d) = %v", i, j, got)
			}
		}
		if wrapped := fmt.Errorf("context: %w", err); !errors.Is(wrapped, err) {
			t.Errorf("sentinel %d lost by wrapping", i)
		}
	}
}

// TestBoundedDefaults carries the v1 zero-selects-default rule (WriteBufferSize 0 means
// 64) to every budget of Options: zero selects the bounded default, never unlimited, a
// negative value is rejected and a custom value is kept.
func TestBoundedDefaults(t *testing.T) {
	got, err := (sio.Options{}).Normalize()
	if err != nil {
		t.Fatal(err)
	}
	want := sio.Options{
		AckTimeout: 30 * time.Second, OutboundQueueGroups: 64, OutboundQueueBytes: 8 << 20,
		HandlerQueueEvents: 64, HandlerQueueBytes: 8 << 20, MaxPendingAcks: 128, MaxAttachments: 64,
		MaxEventBytes: 1 << 20, AttachmentTimeout: 10 * time.Second, MaxConcurrentConnects: 4,
		ConnectTimeout: 10 * time.Second,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults: got %+v want %+v", got, want)
	}
	one := sio.Options{
		AckTimeout: time.Second, OutboundQueueGroups: 1, OutboundQueueBytes: 1, HandlerQueueEvents: 1,
		HandlerQueueBytes: 1, MaxPendingAcks: 1, MaxAttachments: 1, MaxEventBytes: 1,
		AttachmentTimeout: time.Second, MaxConcurrentConnects: 1, ConnectTimeout: time.Second,
	}
	if got, err := one.Normalize(); err != nil || !reflect.DeepEqual(got, one) {
		t.Fatalf("custom limits changed: %+v %v", got, err)
	}
	for _, invalid := range []sio.Options{
		{AckTimeout: -1}, {OutboundQueueGroups: -1}, {OutboundQueueBytes: -1}, {HandlerQueueEvents: -1},
		{HandlerQueueBytes: -1}, {MaxPendingAcks: -1}, {MaxAttachments: -1}, {MaxEventBytes: -1},
		{AttachmentTimeout: -1}, {MaxConcurrentConnects: -1}, {ConnectTimeout: -1},
	} {
		if _, err := invalid.Normalize(); err == nil {
			t.Errorf("accepted a negative limit: %+v", invalid)
		}
	}
}

// TestNormalizeValidatesEngineOptions checks that the root validates the nested
// Engine.IO observer options and leaves the observer fields unresolved.
func TestNormalizeValidatesEngineOptions(t *testing.T) {
	for _, limit := range []int{-1, 257} {
		if _, err := (sio.Options{Engine: engineio.Options{PayloadPreviewBytes: limit}}).Normalize(); err == nil {
			t.Fatalf("accepted engine preview limit %d", limit)
		}
	}
	got, err := (sio.Options{}).Normalize()
	if err != nil || got.Logger != nil || got.Hooks != nil || got.Adapter != nil {
		t.Fatalf("Normalize resolved an observer or adapter field: %+v %v", got, err)
	}
}

// TestBroadcastOperatorIsImmutable pins that a selection builder never changes the
// operator it was derived from.
func TestBroadcastOperatorIsImmutable(t *testing.T) {
	n := &sio.Namespace{}
	base := n.To("a", "b")
	derived := base.Except("c").Local()
	if !reflect.DeepEqual(base, n.To("a", "b")) {
		t.Fatalf("Except or Local changed the base operator: %+v", base)
	}
	if reflect.DeepEqual(base, derived) {
		t.Fatal("derived operator equals the base")
	}
	rooms := []sio.Room{"x"}
	op := n.To(rooms...)
	rooms[0] = "changed"
	if !reflect.DeepEqual(op, n.To("x")) {
		t.Fatal("To kept the caller's slice")
	}
}

// TestSkeletonServeHTTPAndHookConstructors pins that the http.Handler and the hook
// constructors of the skeleton claim no behaviour: the handler answers 501 and the
// constructors return nil, which is "no observer".
func TestSkeletonServeHTTPAndHookConstructors(t *testing.T) {
	var h http.Handler = &sio.Server{}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/socket.io/?EIO=4&transport=polling", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status %d, want %d", rec.Code, http.StatusNotImplemented)
	}
	if sio.ChainHooks(&sio.Hooks{}, nil) != nil || sio.LoggingHooks(slog.Default()) != nil {
		t.Fatal("the skeleton hook constructors returned an observer")
	}
}

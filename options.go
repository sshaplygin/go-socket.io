package socketio

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
)

// Options configures a Server. In every budget field zero selects the bounded
// default noted on the field, never "unlimited"; a negative value is rejected by
// Normalize. Logger, Hooks and Adapter keep their zero value (nil) as "not set": a
// nil Logger is resolved by the instance logger policy of roadmap 2.4, nil Hooks
// means no observer and a nil Adapter selects the in-memory adapter once 2.2 lands.
type Options struct {
	Logger                *slog.Logger
	Engine                engineio.Options
	Hooks                 *Hooks
	Adapter               AdapterFactory
	AckTimeout            time.Duration // default 30 s; an earlier caller deadline wins.
	OutboundQueueGroups   int           // default 64 complete message groups, across all namespaces of a session.
	OutboundQueueBytes    int           // default 8 MiB including attachments, across a session.
	HandlerQueueEvents    int           // default 64 events per namespace socket.
	HandlerQueueBytes     int           // default 8 MiB per namespace socket including attachments.
	MaxPendingAcks        int           // default 128 per namespace socket.
	MaxAttachments        int           // default 64 per reconstructed event.
	MaxEventBytes         int           // default 1 MiB per reconstructed event including attachments.
	AttachmentTimeout     time.Duration // default 10 s to assemble a binary event.
	MaxConcurrentConnects int           // default 4 namespace CONNECT/auth tasks per Engine.IO session, no waiting queue.
	ConnectTimeout        time.Duration // default 10 s per CONNECT/auth task; its slot is held until middleware returns.
}

// Normalize returns a copy of o with the defaults applied. It validates Engine and
// rejects negative budgets. It copies values only: it does not call user callbacks,
// resolve loggers, change the application's default logger or enforce limits.
func (o Options) Normalize() (Options, error) {
	engine, err := o.Engine.Normalize()
	if err != nil {
		return Options{}, fmt.Errorf("engine options: %w", err)
	}
	o.Engine = engine
	counts := []struct {
		name     string
		value    *int
		fallback int
	}{
		{"OutboundQueueGroups", &o.OutboundQueueGroups, 64},
		{"OutboundQueueBytes", &o.OutboundQueueBytes, 8 << 20},
		{"HandlerQueueEvents", &o.HandlerQueueEvents, 64},
		{"HandlerQueueBytes", &o.HandlerQueueBytes, 8 << 20},
		{"MaxPendingAcks", &o.MaxPendingAcks, 128},
		{"MaxAttachments", &o.MaxAttachments, 64},
		{"MaxEventBytes", &o.MaxEventBytes, 1 << 20},
		{"MaxConcurrentConnects", &o.MaxConcurrentConnects, 4},
	}
	for _, c := range counts {
		if *c.value < 0 {
			return Options{}, fmt.Errorf("socketio: %s must not be negative", c.name)
		}
		if *c.value == 0 {
			*c.value = c.fallback
		}
	}
	durations := []struct {
		name     string
		value    *time.Duration
		fallback time.Duration
	}{
		{"AckTimeout", &o.AckTimeout, 30 * time.Second},
		{"AttachmentTimeout", &o.AttachmentTimeout, 10 * time.Second},
		{"ConnectTimeout", &o.ConnectTimeout, 10 * time.Second},
	}
	for _, d := range durations {
		if *d.value < 0 {
			return Options{}, fmt.Errorf("socketio: %s must not be negative", d.name)
		}
		if *d.value == 0 {
			*d.value = d.fallback
		}
	}
	return o, nil
}

// SocketInfo identifies a namespace socket in hook calls.
type SocketInfo struct{ SID, SocketID, Namespace string }

// EventInfo describes an inbound event.
type EventInfo struct {
	Socket                SocketInfo
	Event                 string
	AckID                 uint64
	NeedAck, HandlerFound bool
}

// EventResult is the terminal notification of an inbound event. Result is one of the
// EventResult constants.
type EventResult struct {
	Result    string
	Err       error
	AckQueued bool
	Duration  time.Duration
}

// EmitInfo describes an outbound emit.
type EmitInfo struct {
	Socket SocketInfo
	Event  string
	AckID  uint64
}

// BroadcastInfo describes a broadcast.
type BroadcastInfo struct {
	Namespace, Event string
	Rooms, Except    []Room
	Local            bool
}

// AdapterMessage describes one adapter publication or receipt. Kind is one of the
// AdapterMessage constants.
type AdapterMessage struct {
	Namespace, Kind string
	Bytes           int
}

// Hooks are the Socket.IO observer callbacks. This declaration fixes the
// signatures only (not yet frozen, see docs/API.md, Open before G2): nothing fires a hook yet (roadmap 2.4S owns the fire points). Nil
// fields mean no observer and callers must test the pointer and the field. Metadata
// is borrowed for the call; an observer that retains it must copy it. Start contexts
// flow left to right, terminal hooks unwind right to left, and the runtime pairs
// every start with exactly one terminal call.
type Hooks struct {
	ConnectStart        func(ctx context.Context, s SocketInfo) context.Context
	ConnectEnd          func(ctx context.Context, s SocketInfo, err error, d time.Duration)
	Disconnect          func(ctx context.Context, s SocketInfo, reason string, d time.Duration)
	EventStart          func(ctx context.Context, e EventInfo) context.Context
	EventEnd            func(ctx context.Context, e EventInfo, r EventResult)
	Emit                func(ctx context.Context, e EmitInfo)
	MessageQueued       func(ctx context.Context, e EmitInfo)
	EmitStart           func(ctx context.Context, e EmitInfo) context.Context
	EmitEnd             func(ctx context.Context, e EmitInfo, err error, rtt time.Duration)
	BroadcastStart      func(ctx context.Context, b BroadcastInfo) context.Context
	BroadcastEnd        func(ctx context.Context, b BroadcastInfo, result BroadcastResult, err error)
	WriteBufferFull     func(ctx context.Context, s SocketInfo)
	AdapterPublishStart func(ctx context.Context, m AdapterMessage) context.Context
	AdapterPublishEnd   func(ctx context.Context, m AdapterMessage, err error, d time.Duration)
	AdapterReceiveStart func(ctx context.Context, m AdapterMessage) context.Context
	AdapterReceiveEnd   func(ctx context.Context, m AdapterMessage, err error, d time.Duration)
}

// Result values of roadmap 2.4 for EventResult.Result, connect results and
// acknowledgement results. The hook fields stay strings so the signatures match the
// roadmap, and Go does not enforce the closed sets. The upgrade results, request
// rejection reasons, disconnect reasons and adapter results are not enumerated by the
// roadmap yet and are not declared here.
const (
	EventResultOK          = "ok"
	EventResultError       = "error"
	EventResultNoHandler   = "no_handler"
	EventResultDecodeError = "decode_error"
	EventResultClosed      = "closed"

	ConnectResultOK               = "ok"
	ConnectResultError            = "error"
	ConnectResultUnknownNamespace = "unknown_namespace"

	AckResultOK       = "ok"
	AckResultTimeout  = "timeout"
	AckResultClosed   = "closed"
	AckResultCanceled = "canceled"
	AckResultError    = "error"

	AdapterMessageBroadcast = "broadcast"
	AdapterMessageRequest   = "request"
	AdapterMessageResponse  = "response"
)

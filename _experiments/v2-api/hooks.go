package socketio

import (
	"context"
	"time"
)

type SocketInfo struct{ SID, SocketID, Namespace string }
type EventInfo struct {
	Socket                SocketInfo
	Event                 string
	AckID                 uint64
	NeedAck, HandlerFound bool
}
type EventResult struct {
	Result    string
	Err       error
	AckQueued bool
	Duration  time.Duration
}
type EmitInfo struct {
	Socket SocketInfo
	Event  string
	AckID  uint64
}
type BroadcastInfo struct {
	Namespace, Event string
	Rooms, Except    []Room
	Local            bool
}
type AdapterMessage struct {
	Namespace, Kind string
	Bytes           int
}

// Hooks preserves the roadmap 2.4 signatures. It has no runtime fire points here.
// Metadata is borrowed for the call only: observers must copy retained slices.
// Start contexts flow left to right; terminal hooks unwind right to left. Runtime
// must pair every start with one terminal call, including rejection and shutdown.
// Nil fields mean no observer; callers must test the pointer and field before use.
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

// These string constants enumerate only values specified in the roadmap 2.4
// lifecycle/metric tables. Fields remain strings to preserve its exact signatures;
// Go does not enforce closed string enums. Mapping errors to metric results is a
// future bridge/runtime obligation, not implemented by these constants.
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

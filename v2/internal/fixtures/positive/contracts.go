package positive

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	sio "github.com/sshaplygin/go-socket.io/v2"
	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/internal/fixtures/externaladapter"
)

// These callbacks are compile fixtures only, never runtime instrumentation.
var SocketHooks = &sio.Hooks{
	ConnectStart:        func(ctx context.Context, _ sio.SocketInfo) context.Context { return ctx },
	ConnectEnd:          func(context.Context, sio.SocketInfo, error, time.Duration) {},
	Disconnect:          func(context.Context, sio.SocketInfo, string, time.Duration) {},
	EventStart:          func(ctx context.Context, _ sio.EventInfo) context.Context { return ctx },
	EventEnd:            func(context.Context, sio.EventInfo, sio.EventResult) {},
	Emit:                func(context.Context, sio.EmitInfo) {},
	MessageQueued:       func(context.Context, sio.EmitInfo) {},
	EmitStart:           func(ctx context.Context, _ sio.EmitInfo) context.Context { return ctx },
	EmitEnd:             func(context.Context, sio.EmitInfo, error, time.Duration) {},
	BroadcastStart:      func(ctx context.Context, _ sio.BroadcastInfo) context.Context { return ctx },
	BroadcastEnd:        func(context.Context, sio.BroadcastInfo, sio.BroadcastResult, error) {},
	WriteBufferFull:     func(context.Context, sio.SocketInfo) {},
	AdapterPublishStart: func(ctx context.Context, _ sio.AdapterMessage) context.Context { return ctx },
	AdapterPublishEnd:   func(context.Context, sio.AdapterMessage, error, time.Duration) {},
	AdapterReceiveStart: func(ctx context.Context, _ sio.AdapterMessage) context.Context { return ctx },
	AdapterReceiveEnd:   func(context.Context, sio.AdapterMessage, error, time.Duration) {},
}
var EngineHooks = &engineio.Hooks{
	HandshakeStart:  func(ctx context.Context, _ *http.Request) context.Context { return ctx },
	HandshakeEnd:    func(context.Context, engineio.SessionInfo, engineio.HandshakeResult) {},
	RequestRejected: func(context.Context, *http.Request, string, error) {},
	SessionOpen:     func(ctx context.Context, _ engineio.SessionInfo) context.Context { return ctx },
	SessionClose:    func(context.Context, engineio.SessionInfo, engineio.CloseReason, error, time.Duration) {},
	UpgradeStart:    func(ctx context.Context, _ engineio.SessionInfo, _, _ string) context.Context { return ctx },
	UpgradeEnd:      func(context.Context, engineio.SessionInfo, string, string, error) {},
	PacketRead:      func(context.Context, engineio.SessionInfo, engineio.PacketInfo) {},
	PacketWrite:     func(context.Context, engineio.SessionInfo, engineio.PacketInfo) {},
	PingSent:        func(context.Context, engineio.SessionInfo) {},
	PongReceived:    func(context.Context, engineio.SessionInfo, time.Duration) {},
}

func ComposeContracts(l *slog.Logger) sio.Options {
	assertType[engineio.PacketInfo](engineio.PacketInfo{Type: packet.MESSAGE, Frame: frame.String, Bytes: 7})
	assertType[sio.RemoteSocket](sio.RemoteSocket{
		ID: "socket", Rooms: []sio.Room{"socket", "room"},
		Handshake: json.RawMessage(`{}`), Data: json.RawMessage(`null`),
	})
	assertType[sio.BroadcastOptions](sio.BroadcastOptions{
		Rooms: []sio.Room{"room"}, Except: []sio.Room{"excluded"}, Flags: sio.BroadcastFlags{Local: true},
	})
	return sio.Options{
		Logger: l, Adapter: externaladapter.New, Hooks: sio.ChainHooks(SocketHooks, sio.LoggingHooks(l)),
		Engine: engineio.Options{Logger: l, Hooks: engineio.ChainHooks(EngineHooks, engineio.LoggingHooks(l)), PayloadPreviewBytes: 128},
	}
}

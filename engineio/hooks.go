package engineio

import (
	"context"
	"net/http"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// SessionInfo identifies an Engine.IO session in hook calls.
type SessionInfo struct{ SID, Transport, RemoteAddr string }

// PacketInfo describes one Engine.IO packet in PacketRead and PacketWrite calls.
// Preview is empty unless Options.PayloadPreviewBytes, a PayloadRedactor and an
// enabled consumer are all configured; it is capped at 256 bytes.
type PacketInfo struct {
	Type    packet.Type
	Frame   frame.Type
	Bytes   int
	Preview []byte
}

// HandshakeResult is the terminal notification of a new-session handshake.
type HandshakeResult struct {
	Result   string
	Err      error
	Duration time.Duration
}

// CloseReason is the reason an Engine.IO session closed.
type CloseReason string

// Close reasons of roadmap 2.4.
const (
	CloseTransportClose     CloseReason = "transport close"
	CloseTransportError     CloseReason = "transport error"
	ClosePingTimeout        CloseReason = "ping timeout"
	CloseForced             CloseReason = "forced close"
	CloseServerShuttingDown CloseReason = "server shutting down"
	CloseParseError         CloseReason = "parse error"
)

// Handshake results of roadmap 2.4. HandshakeResult.Result stays a string so the
// hook signatures match the roadmap; Go does not enforce the closed set.
const (
	HandshakeResultOK           = "ok"
	HandshakeResultBadTransport = "bad_transport"
	HandshakeResultChecker      = "checker"
	HandshakeResultAccept       = "accept"
	HandshakeResultNoHijacker   = "no_hijacker"
	HandshakeResultInit         = "init"
	HandshakeResultTimeout      = "timeout"
	HandshakeResultClosed       = "closed"
)

// Hooks are the Engine.IO observer callbacks. This declaration fixes the
// signatures only (not yet frozen, see docs/API.md, Open before G2): no hook is fired yet (roadmap 2.4E owns the fire points).
// Nil fields mean no observer and callers must test the pointer and the field.
// Metadata is borrowed for the duration of a call; an observer that retains it
// must copy it. Start contexts flow left to right and terminal hooks unwind right
// to left. The context an accepted session carries must outlive HTTP request
// cancellation (roadmap 2.0 lifecycle contract).
type Hooks struct {
	HandshakeStart  func(ctx context.Context, r *http.Request) context.Context
	HandshakeEnd    func(ctx context.Context, s SessionInfo, r HandshakeResult)
	RequestRejected func(ctx context.Context, r *http.Request, reason string, err error)
	SessionOpen     func(ctx context.Context, s SessionInfo) context.Context
	SessionClose    func(ctx context.Context, s SessionInfo, reason CloseReason, err error, d time.Duration)
	UpgradeStart    func(ctx context.Context, s SessionInfo, from, to string) context.Context
	UpgradeEnd      func(ctx context.Context, s SessionInfo, from, to string, err error)
	PacketRead      func(ctx context.Context, s SessionInfo, p PacketInfo)
	PacketWrite     func(ctx context.Context, s SessionInfo, p PacketInfo)
	PingSent        func(ctx context.Context, s SessionInfo)
	PongReceived    func(ctx context.Context, s SessionInfo, rtt time.Duration)
}

// PayloadRedactor is the trusted, protocol-aware boundary that produces
// PacketInfo.Preview. Enabled reports whether a preview consumer is currently
// enabled. Redact receives borrowed, unredacted packet bytes solely to classify
// and redact them and must not retain them. It returns an owned preview of at most
// limit bytes, or nil for any auth-bearing CONNECT, unrecognised, incomplete or
// unsafe packet. Engine.IO does not import the Socket.IO parser, so the owning
// application or Socket.IO layer supplies the implementation. No preview is
// captured yet.
type PayloadRedactor interface {
	Enabled(context.Context) bool
	Redact(ctx context.Context, s SessionInfo, p PacketInfo, payload []byte, limit int) []byte
}

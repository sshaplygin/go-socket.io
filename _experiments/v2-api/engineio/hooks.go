// Package engineio is an observer contract proof, not an Engine.IO implementation.
package engineio

import (
	"context"
	"net/http"
	"time"

	"github.com/sshaplygin/go-socket.io/experiments/v2-api/engineio/frame"
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/engineio/packet"
)

type SessionInfo struct{ SID, Transport, RemoteAddr string }
type PacketInfo struct {
	Type    packet.Type
	Frame   frame.Type
	Bytes   int
	Preview []byte
}
type HandshakeResult struct {
	Result   string
	Err      error
	Duration time.Duration
}
type CloseReason string

// Hooks preserves every roadmap 2.4 Engine.IO hook signature. No hook is fired in
// this prototype. Metadata is borrowed during the call; retention requires copies.
// Accepted session contexts must outlive HTTP request cancellation in production.
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

// Values below come directly from roadmap 2.4's close-reason declaration and
// handshake metric table. String fields deliberately match the roadmap signatures.
const (
	CloseTransportClose     CloseReason = "transport close"
	CloseTransportError     CloseReason = "transport error"
	ClosePingTimeout        CloseReason = "ping timeout"
	CloseForced             CloseReason = "forced close"
	CloseServerShuttingDown CloseReason = "server shutting down"
	CloseParseError         CloseReason = "parse error"

	HandshakeResultOK           = "ok"
	HandshakeResultBadTransport = "bad_transport"
	HandshakeResultChecker      = "checker"
	HandshakeResultAccept       = "accept"
	HandshakeResultNoHijacker   = "no_hijacker"
	HandshakeResultInit         = "init"
	HandshakeResultTimeout      = "timeout"
	HandshakeResultClosed       = "closed"
)

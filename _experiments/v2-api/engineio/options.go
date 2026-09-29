package engineio

import (
	"context"
	"fmt"
	"log/slog"
)

// PayloadRedactor is a proposed trusted protocol-aware preview boundary.
// Enabled must report whether a preview consumer is currently enabled. Redact
// receives borrowed, unredacted packet bytes solely for classification/redaction;
// it must not retain them. PacketInfo.Preview is empty on entry. Return an owned
// redacted preview of at most limit bytes, or nil for any auth-bearing CONNECT,
// unrecognized, incomplete or unsafe packet. Runtime must enforce the output cap
// and never retain full frames. Engine.IO must not import the Socket.IO parser;
// the owning application or Socket.IO layer supplies this implementation.
// This prototype declares the contract only: no preview capture takes place.
type PayloadRedactor interface {
	Enabled(context.Context) bool
	Redact(ctx context.Context, s SessionInfo, p PacketInfo, payload []byte, limit int) []byte
}

// Options includes observer configuration only, not transport/server/client options.
// Preview collection requires a positive limit, a non-nil redactor and an enabled
// consumer. TRACE alone is insufficient. Nil Logger is resolved by the future
// instance logger policy; this helper never changes the application default.
type Options struct {
	Logger              *slog.Logger
	Hooks               *Hooks
	PayloadPreviewBytes int // 0 disables capture; accepted range 0..256.
	PayloadRedactor     PayloadRedactor
}

// Normalize validates a copy. It does not resolve loggers or invoke user callbacks.
func (o Options) Normalize() (Options, error) {
	if o.PayloadPreviewBytes < 0 || o.PayloadPreviewBytes > 256 {
		return Options{}, fmt.Errorf("PayloadPreviewBytes must be between 0 and 256")
	}
	return o, nil
}

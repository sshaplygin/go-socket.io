package socketio

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/sshaplygin/go-socket.io/experiments/v2-api/engineio"
)

// Options proposes names for the stage 2.3 budgets. Zero budget fields select the bounded
// defaults documented on those fields; negative values are rejected by Normalize.
// Observer/adapter fields compose contracts only; runtime wiring remains deferred.
type Options struct {
	Logger                *slog.Logger
	Engine                engineio.Options
	Hooks                 *Hooks
	Adapter               AdapterFactory
	AckTimeout            time.Duration // 30 seconds; caller deadline wins when earlier.
	OutboundQueueGroups   int           // 64 complete message groups, shared across a session.
	OutboundQueueBytes    int           // 8 MiB, including attachments, shared across a session.
	HandlerQueueEvents    int           // 64 events per namespace socket.
	HandlerQueueBytes     int           // 8 MiB per namespace socket, including attachments.
	MaxPendingAcks        int           // 128 per namespace socket.
	MaxAttachments        int           // 64 per reconstructed event.
	MaxEventBytes         int           // 1 MiB per reconstructed event, including attachments.
	AttachmentTimeout     time.Duration // 10 seconds to assemble a binary event.
	MaxConcurrentConnects int           // 4 namespace CONNECT/auth tasks per Engine.IO session; no waiting queue.
	ConnectTimeout        time.Duration // 10 seconds per CONNECT/auth task; slot held until middleware returns.
}

// Normalize applies defaults to a copy and rejects negative settings. It is a
// pure configuration helper; none of these limits are enforced by runtime stubs.
func (o Options) Normalize() (Options, error) {
	engine, err := o.Engine.Normalize()
	if err != nil {
		return Options{}, fmt.Errorf("engine: %w", err)
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
	for _, item := range counts {
		if *item.value < 0 {
			return Options{}, fmt.Errorf("%s must not be negative", item.name)
		}
		if *item.value == 0 {
			*item.value = item.fallback
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
	for _, item := range durations {
		if *item.value < 0 {
			return Options{}, fmt.Errorf("%s must not be negative", item.name)
		}
		if *item.value == 0 {
			*item.value = item.fallback
		}
	}
	return o, nil
}

package client

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// Options configures a Client made by New.
type Options struct {
	// PerMessageDeflate offers websocket compression, with a 1024-byte threshold.
	PerMessageDeflate bool
	// Transports are tried in upgrade order. Nil means polling then websocket.
	Transports []transport.Transport
	// Path is the Engine.IO endpoint; empty means /socket.io.
	Path string
	// Query is added to every engine request, separate from namespace queries.
	Query url.Values
	// Header is sent with every request, including a websocket upgrade.
	Header http.Header
	// Timeout bounds the dial through OPEN. Zero means 20 seconds; negative is unbounded.
	Timeout time.Duration
	// WriteBufferSize bounds queued writes and pre-connect events; nonpositive means 64.
	WriteBufferSize int
	Logger          *slog.Logger
	// DisableReconnection disables automatic recovery after an engine failure.
	DisableReconnection bool
	// ReconnectionAttempts limits failed retries; zero means unlimited.
	ReconnectionAttempts int
	// ReconnectionDelay defaults to one second; ReconnectionDelayMax to five seconds.
	ReconnectionDelay    time.Duration
	ReconnectionDelayMax time.Duration
	// RandomizationFactor defaults to 0.5; a negative value disables jitter.
	RandomizationFactor float64
}

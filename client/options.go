package client

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// Default queue budget and orderly drain bound, resolved by New.
const defaultWriteBufferSize, defaultDrainTimeout = 64, time.Minute

// options holds private client configuration.
type options struct {
	MaxMessageSize int64
	// PerMessageDeflate offers websocket compression, with a 1024-byte threshold.
	PerMessageDeflate bool
	// Transports are tried in upgrade order. Nil means polling then websocket.
	Transports []transport.Transport
	// Path is the Engine.IO endpoint; empty means /socket.io.
	Path string
	// Query is added to every engine request, separate from namespace queries; for
	// a key that the address passed to New also has, its values replace the address's.
	Query url.Values
	// Header is sent with every request, including a websocket upgrade.
	Header http.Header
	// Timeout bounds the dial through OPEN. Zero means 20 seconds; negative is unbounded.
	Timeout time.Duration
	// WriteBufferSize bounds queued writes and pre-connect events; nonpositive means 64.
	WriteBufferSize int
	// CloseTimeout bounds orderly drain; nonpositive means one minute.
	CloseTimeout time.Duration
	Logger       *slog.Logger
	// DisableReconnection disables automatic recovery after an engine failure.
	DisableReconnection bool
	// ReconnectionAttempts limits failed retries; nonpositive means unlimited.
	ReconnectionAttempts int
	// ReconnectionDelay defaults to one second; ReconnectionDelayMax to five seconds.
	ReconnectionDelay    time.Duration
	ReconnectionDelayMax time.Duration
	// RandomizationFactor defaults to 0.5; a value that is negative, greater than 1
	// or NaN disables jitter.
	RandomizationFactor float64
}

// Option configures one instance. Options are applied in order; the last setting wins.
type Option func(*options)

// WithPerMessageDeflate enables websocket compression; the default is false.
func WithPerMessageDeflate(value bool) Option {
	return func(config *options) { config.PerMessageDeflate = value }
}

// WithPath sets the Engine.IO endpoint; empty means /socket.io.
func WithPath(value string) Option {
	return func(config *options) { config.Path = value }
}

// WithQuery adds engine request query values, copied when New is called. For a key
// that the address passed to New also has, the option's values replace the address's.
func WithQuery(value url.Values) Option {
	return func(config *options) { config.Query = value }
}

// WithHeader sets request headers, copied when New is called, including websocket upgrades.
func WithHeader(value http.Header) Option {
	return func(config *options) { config.Header = value }
}

// WithTimeout bounds dialing through OPEN; zero means 20 seconds and negative is unbounded.
func WithTimeout(value time.Duration) Option {
	return func(config *options) { config.Timeout = value }
}

// WithWriteBufferSize bounds queued writes and pre-connect events; nonpositive means 64.
// The bound on queued writes applies to each Engine.IO connection; the bound on
// pre-connect events applies to all namespaces of the Client together.
func WithWriteBufferSize(value int) Option {
	return func(config *options) { config.WriteBufferSize = value }
}

// WithCloseTimeout bounds orderly drain; nonpositive means one minute.
func WithCloseTimeout(value time.Duration) Option {
	return func(config *options) { config.CloseTimeout = value }
}

// WithLogger sets the Socket.IO client and connection log sink; nil selects logger.Log.
func WithLogger(value *slog.Logger) Option {
	return func(config *options) { config.Logger = value }
}

// WithReconnection enables automatic engine recovery; the default is true. Only
// the end of a connection that had opened starts it: a failed Client.Connect or
// Socket.Connect does not.
func WithReconnection(value bool) Option {
	return func(config *options) { config.DisableReconnection = !value }
}

// WithReconnectionAttempts limits failed retries; nonpositive means unlimited.
func WithReconnectionAttempts(value int) Option {
	return func(config *options) { config.ReconnectionAttempts = value }
}

// WithReconnectionDelay sets the initial retry delay; nonpositive means one second.
func WithReconnectionDelay(value time.Duration) Option {
	return func(config *options) { config.ReconnectionDelay = value }
}

// WithReconnectionDelayMax sets the retry delay cap; nonpositive means five seconds.
func WithReconnectionDelayMax(value time.Duration) Option {
	return func(config *options) { config.ReconnectionDelayMax = value }
}

// WithRandomizationFactor sets retry jitter; zero means 0.5, and a value that is
// negative, greater than 1 or NaN disables jitter.
func WithRandomizationFactor(value float64) Option {
	return func(config *options) { config.RandomizationFactor = value }
}

// WithTransports sets transports in dial/upgrade order. The slice is copied for each instance.
// The client uses a copy of each websocket.Transport whose EnableCompression and
// CompressionThreshold are replaced by WithPerMessageDeflate and a threshold of 1024.
func WithTransports(values ...transport.Transport) Option {
	return func(config *options) { config.Transports = append(values[:0:0], values...) }
}

func resolveOptions(opts ...Option) *options {
	config := new(options)
	for _, option := range opts {
		if option != nil {
			option(config)
		}
	}
	return config
}

// WithMaxMessageSize bounds each received websocket message or polling response
// body, including OPEN and reconnects. Nonpositive values mean 1 MiB. Built-in
// transports enforce the limit after decompression; custom transports must
// implement SetReadLimit(int64).
func WithMaxMessageSize(bytes int64) Option {
	return func(config *options) { config.MaxMessageSize = bytes }
}

package socketio

import (
	"log/slog"
	"time"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/engineio"
)

type options struct {
	Adapter         adapter.Factory
	EngineOptions   []engineio.Option
	WriteBufferSize int
	CloseTimeout    time.Duration
	Logger          *slog.Logger
}

// Option configures a server. Options are applied in order; the last setting wins.
type Option func(*options)

// WithEngineOptions configures the underlying Engine.IO server. Repeated calls
// append options in order. Engine logger settings affect only that lower layer.
func WithEngineOptions(values ...engineio.Option) Option {
	return func(config *options) { config.EngineOptions = append(config.EngineOptions, values...) }
}

// WithWriteBufferSize bounds queued packets; nonpositive means 64.
func WithWriteBufferSize(value int) Option {
	return func(config *options) { config.WriteBufferSize = value }
}

// WithCloseTimeout bounds orderly drain; nonpositive means one minute.
func WithCloseTimeout(value time.Duration) Option {
	return func(config *options) { config.CloseTimeout = value }
}

// WithLogger sets the Socket.IO server and underlying engine log sinks; nil
// selects logger.Log. A later engine logger option overrides only that layer.
func WithLogger(value *slog.Logger) Option {
	return func(config *options) {
		config.Logger = value
		config.EngineOptions = append(config.EngineOptions, engineio.WithLogger(value))
	}
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

// WithAdapter selects a factory for future namespaces. Nil selects memory.
// Construction happens on namespace registration; Serve reports its errors.
func WithAdapter(factory adapter.Factory) Option {
	return func(config *options) { config.Adapter = factory }
}

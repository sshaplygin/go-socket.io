// Package logger holds the log level control shared by every package of the
// library and the fallback logger for packages that cannot reach
// engineio.Options.Logger.
//
// # Runtime level
//
// The environment variable SOCKETIO_LOG_LEVEL is read once at program start.
// Accepted values, case-insensitive: error, warn, info, debug, trace. When it
// is set, Level decides which library records are enabled, regardless of the
// level configured on the application's handler, so an operator can turn on
// debug output of a running deployment without a rebuild. When it is unset,
// the application's handler decides. An invalid value logs one WARN and
// behaves as unset. Level can be changed at runtime with Level.Set; the change
// takes effect only while the variable was set at start.
//
// engineio.Options.Logger chooses where records go; it never chooses the
// level. Every logger the library uses is passed through Wrap.
//
// # Levels
//
// The library logs at slog's ERROR, WARN, INFO and DEBUG, plus LevelTrace for
// per-packet and ping/pong lines. Trace records are always guarded by
// Enabled, so disabled trace logging does not allocate.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
)

// EnvLevel is the name of the environment variable that sets Level.
const EnvLevel = "SOCKETIO_LOG_LEVEL"

// LevelTrace is below slog.LevelDebug. Handlers render it as "DEBUG-4" unless
// they use ReplaceAttr.
const LevelTrace = slog.LevelDebug - 4

// Level is the library log level while SOCKETIO_LOG_LEVEL is set.
var Level = new(slog.LevelVar)

// override is true when SOCKETIO_LOG_LEVEL held a valid value at start.
var override atomic.Bool

// Log is the fallback logger for packages without access to
// engineio.Options: the parser, the transports, engineio/packet and the client
// dialer.
var Log = Wrap(slog.Default())

func init() {
	value, ok := os.LookupEnv(EnvLevel)
	if !ok {
		return
	}
	lvl, valid := parseLevel(value)
	if !valid {
		Log.Warn("logger: invalid "+EnvLevel+", ignored", "value", value)
		return
	}
	Level.Set(lvl)
	override.Store(true)
}

func parseLevel(value string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "error":
		return slog.LevelError, true
	case "warn":
		return slog.LevelWarn, true
	case "info":
		return slog.LevelInfo, true
	case "debug":
		return slog.LevelDebug, true
	case "trace":
		return LevelTrace, true
	}
	return 0, false
}

// ReplaceAttr renders LevelTrace as "TRACE". Pass it as
// slog.HandlerOptions.ReplaceAttr to the handler given to the library.
func ReplaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.LevelKey {
		if lvl, ok := a.Value.Any().(slog.Level); ok && lvl == LevelTrace {
			a.Value = slog.StringValue("TRACE")
		}
	}
	return a
}

// Wrap returns a logger over l's handler whose Enabled follows Level while
// SOCKETIO_LOG_LEVEL is set, and l's handler otherwise. A nil l means
// slog.Default(). Wrapping an already wrapped logger returns it unchanged.
func Wrap(l *slog.Logger) *slog.Logger {
	if l == nil {
		l = slog.Default()
	}
	if _, ok := l.Handler().(*levelHandler); ok {
		return l
	}
	return slog.New(&levelHandler{next: l.Handler()})
}

type levelHandler struct {
	next slog.Handler
}

func (h *levelHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	if override.Load() {
		return lvl >= Level.Level()
	}
	return h.next.Enabled(ctx, lvl)
}

// Handle forwards unconditionally: Enabled has already decided, and the
// application's handler level must not hide library records under override.
func (h *levelHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.next.Handle(ctx, r)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{next: h.next.WithAttrs(attrs)}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{next: h.next.WithGroup(name)}
}

// Error logs msg with err at ERROR through Log. A nil err is logged as such.
func Error(msg string, err error) {
	Log.Error(msg, "err", err)
}

// Info logs msg with args at INFO through Log.
func Info(msg string, args ...interface{}) {
	Log.Info(msg, args...)
}

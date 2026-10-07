// Package logger holds the log level control shared by every package of the
// library and the fallback logger for packages that cannot reach
// engineio.Options.Logger.
//
// # Variables
//
// Log is the fallback logger: the parser, the transports, engineio/packet and
// the client dialer log through it, and it writes to whatever slog.Default()
// is when a record is logged. Level is the library log level described below.
// To route library records, pass a logger as engineio.Options.Logger or call
// slog.SetDefault; assigning Log bypasses Wrap.
//
// # Runtime level
//
// The environment variable SOCKETIO_LOG_LEVEL is read once at program start.
// Accepted values, case-insensitive: error, warn, info, debug, trace. When it
// is set, Level decides which library records are enabled, regardless of the
// level configured on the application's handler, so an operator can turn on
// debug output of a running deployment without a rebuild. When it is unset,
// the application's handler decides. An invalid value logs one WARN,
// "logger: invalid level ignored" with the value under value, and behaves as
// unset. Applications can change Level at runtime with Level.Set, with or
// without the variable; Level.Set(LevelUnset) hands the decision back to the
// application's handler.
//
// engineio.Options.Logger chooses where records go; it never chooses the
// level. Every logger the library uses is passed through Wrap.
//
// # Levels
//
// The library logs WARN, DEBUG and LevelTrace records only; it logs no ERROR or
// INFO record. WARN marks a failure no caller receives, such as
// "socketio: unhandled error" or a rejected request (DEBUG for an unknown sid).
// Expected closure (EOF, a closed connection, a peer close, a ping timeout and
// any failure after a close started), errors also returned to a caller or
// delivered to an OnError handler, and the boundary records (session open and
// close, namespace connect, disconnect) are DEBUG. LevelTrace is for
// per-packet and ping/pong lines, which are guarded by Enabled so that disabled
// trace logging does not allocate. Records an application logs through the
// deprecated Error and Info are its own.
//
// # Messages and keys
//
// Every library record has a constant message matching
//
//	^(engineio|socketio|logger): [a-z][a-z0-9 ]*$
//
// prefixed engineio for the engineio packages, socketio for the root package,
// the parser and the Redis broadcast, and logger for this package. Attribute
// keys come only from this list:
//
//   - sid: the engine.io session id
//   - nsp: the namespace; the root namespace is "/"
//   - err: the error
//   - transport: the transport name, following upgrades
//   - remote_addr: the peer address
//   - reason: why a request was rejected, a session closed or a namespace
//     disconnected
//   - duration: how long a session was open
//   - event: the event name
//   - ack_id: the acknowledgement id
//   - type: the packet type
//   - value: an invalid setting, such as SOCKETIO_LOG_LEVEL
//
// No record carries packet payloads.
package logger

import (
	"context"
	"log/slog"
	"math"
	"os"
	"strings"
)

// EnvLevel is the name of the environment variable that sets Level.
const EnvLevel = "SOCKETIO_LOG_LEVEL"

// LevelTrace is below slog.LevelDebug. Handlers render it as "DEBUG-4" unless
// they use ReplaceAttr.
const LevelTrace = slog.LevelDebug - 4

// LevelUnset is the value of Level while no override is active: the
// application's handler decides what is enabled.
const LevelUnset = slog.Level(math.MaxInt32)

// Level is the library log level. It starts at the value of SOCKETIO_LOG_LEVEL,
// or LevelUnset when the variable is unset or invalid. Setting it to any other
// level at runtime turns the override on; setting it to LevelUnset turns it off.
var Level = new(slog.LevelVar)

func overrideOn() bool { return Level.Level() != LevelUnset }

// Log is the fallback logger for packages without access to
// engineio.Options: the parser, the transports, engineio/packet and the client
// dialer. It writes to whatever slog.Default() is when a record is logged, so
// an application's later slog.SetDefault applies to it.
var Log = slog.New(&levelHandler{next: &defaultHandler{}})

func init() {
	Level.Set(LevelUnset)
	value, ok := os.LookupEnv(EnvLevel)
	if !ok {
		return
	}
	lvl, valid := parseLevel(value)
	if !valid {
		Log.Warn("logger: invalid level ignored", "value", value)
		return
	}
	Level.Set(lvl)
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
// Level is not LevelUnset, whether it was set by SOCKETIO_LOG_LEVEL or by
// Level.Set, and l's handler otherwise. A nil l means Log, which follows
// slog.Default() at log time. Wrapping an already wrapped logger returns it
// unchanged.
func Wrap(l *slog.Logger) *slog.Logger {
	if l == nil {
		return Log
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
	if overrideOn() {
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

// defaultHandler delegates to slog.Default()'s handler at call time, applying
// the attributes and groups added through With/WithGroup on the way.
type defaultHandler struct {
	ops []func(slog.Handler) slog.Handler
}

// stderrHandler is used when slog.Default() is Log itself, which would
// otherwise recurse.
var stderrHandler = slog.NewTextHandler(os.Stderr, nil)

func (h *defaultHandler) base() slog.Handler {
	next := slog.Default().Handler()
	if lh, ok := next.(*levelHandler); ok {
		if _, self := lh.next.(*defaultHandler); self {
			return stderrHandler
		}
	}
	return next
}

func (h *defaultHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.base().Enabled(ctx, lvl)
}

func (h *defaultHandler) Handle(ctx context.Context, r slog.Record) error {
	next := h.base()
	for _, op := range h.ops {
		next = op(next)
	}
	return next.Handle(ctx, r)
}

func (h *defaultHandler) with(op func(slog.Handler) slog.Handler) *defaultHandler {
	ops := make([]func(slog.Handler) slog.Handler, len(h.ops), len(h.ops)+1)
	copy(ops, h.ops)
	return &defaultHandler{ops: append(ops, op)}
}

func (h *defaultHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.with(func(n slog.Handler) slog.Handler { return n.WithAttrs(attrs) })
}

func (h *defaultHandler) WithGroup(name string) slog.Handler {
	return h.with(func(n slog.Handler) slog.Handler { return n.WithGroup(name) })
}

// Error logs msg with err at ERROR through Log. A nil err is logged as such.
//
// Deprecated: the library no longer calls Error. Use slog's methods on
// logger.Log or on the logger passed as engineio.Options.Logger instead.
func Error(msg string, err error) {
	Log.Error(msg, "err", err)
}

// Info logs msg with args at INFO through Log.
//
// Deprecated: the library no longer calls Info. Use slog's methods on
// logger.Log or on the logger passed as engineio.Options.Logger instead.
func Info(msg string, args ...interface{}) {
	Log.Info(msg, args...)
}

package socketio

import (
	"context"
	"io"
	"log"
	"log/slog"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/logger"
)

// recordingHandler is a slog.Handler that keeps every record it receives.
type recordingHandler struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recs = append(h.recs, r)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// hasAttr reports whether any record carries the attribute key=val.
func (h *recordingHandler) hasAttr(key, val string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.recs {
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == key && a.Value.String() == val {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// TestServerLoggerOption checks that a server created with
// engineio.Options.Logger reports connection errors through that logger and
// not through the package-level default.
//
// Covers 1L-T12 (S).
func TestServerLoggerOption(t *testing.T) {
	custom := newAttrRecorder()
	fallback := &recordingHandler{}
	// logger.Log follows slog.Default(); swap the default atomically instead of
	// assigning the package variable, which goroutines left by earlier tests
	// may still read. slog.SetDefault also redirects the log package; restore it.
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(fallback))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})

	srv := NewServer(&engineio.Options{Logger: slog.New(custom)})
	srv.OnConnect("/", func(Conn) error { return nil })
	go func() { _ = srv.Serve() }()
	defer func() { _ = srv.Close() }()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// A raw engine.io client sends a CONNECT for a namespace that has no
	// handler; the server logs it as an unhandled error with nsp=/nope.
	dialer := eioclient.Dialer{Transports: []transport.Transport{polling.Default}}
	conn, err := dialer.Dial(ts.URL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	w, err := conn.NextWriter(session.TEXT)
	require.NoError(t, err)
	_, err = w.Write([]byte("0/nope"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	require.Eventually(t, func() bool { return custom.find("msg", "socketio: unhandled error")["nsp"] == "/nope" },
		5*time.Second, 20*time.Millisecond, "custom logger did not receive the namespace error")
	require.Equal(t, "WARN", custom.find("msg", "socketio: unhandled error")["level"])
	require.False(t, fallback.hasAttr("nsp", "/nope"),
		"error was also written to the package-level logger")
}

// attrRecorder records every record with the attributes added through With.
type attrRecorder struct {
	mu    *sync.Mutex
	recs  *[]map[string]string
	attrs []slog.Attr
}

func newAttrRecorder() *attrRecorder {
	return &attrRecorder{mu: &sync.Mutex{}, recs: &[]map[string]string{}}
}

func (h *attrRecorder) Enabled(context.Context, slog.Level) bool { return true }

// Handle keeps the message, the level, the function that logged (by record PC) and the
// attributes.
func (h *attrRecorder) Handle(_ context.Context, r slog.Record) error {
	f, _ := runtime.CallersFrames([]uintptr{r.PC}).Next()
	m := map[string]string{"msg": r.Message, "level": r.Level.String(), "func": f.Function}
	for _, a := range h.attrs {
		m[a.Key] = a.Value.String()
	}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.recs = append(*h.recs, m)
	return nil
}

func (h *attrRecorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *attrRecorder) WithGroup(string) slog.Handler { return h }

func (h *attrRecorder) find(key, val string) map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range *h.recs {
		if m[key] == val {
			return m
		}
	}
	return nil
}

// TestConnLogWrappedWithSid checks that the socket.io layer wraps
// Options.Logger (a debug record is enabled under logger.Level even though
// the handler is at ERROR) and that connection records carry the engine sid.
func TestConnLogWrappedWithSid(t *testing.T) {
	prev := logger.Level.Level()
	logger.Level.Set(slog.LevelDebug)
	t.Cleanup(func() { logger.Level.Set(prev) })

	rec := newAttrRecorder()
	app := slog.New(rec)
	errOnly := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	require.True(t, loggerFrom(&engineio.Options{Logger: errOnly}).Enabled(context.Background(), slog.LevelDebug))
	require.Same(t, logger.Log, loggerFrom(nil))

	srv := NewServer(&engineio.Options{Logger: app})
	srv.OnConnect("/", func(Conn) error { return nil })
	go func() { _ = srv.Serve() }()
	defer func() { _ = srv.Close() }()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	dialer := eioclient.Dialer{Transports: []transport.Transport{polling.Default}}
	conn, err := dialer.Dial(ts.URL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	w, err := conn.NextWriter(session.TEXT)
	require.NoError(t, err)
	_, err = w.Write([]byte("0/nope"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	require.Eventually(t, func() bool { return rec.find("nsp", "/nope") != nil },
		5*time.Second, 20*time.Millisecond)
	require.Equal(t, conn.ID(), rec.find("nsp", "/nope")["sid"])
}

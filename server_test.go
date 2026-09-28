package socketio

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/session"
	"github.com/googollee/go-socket.io/engineio/transport"
	"github.com/googollee/go-socket.io/engineio/transport/polling"
	"github.com/googollee/go-socket.io/logger"
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
func TestServerLoggerOption(t *testing.T) {
	custom := &recordingHandler{}
	fallback := &recordingHandler{}
	prev := logger.Log
	logger.Log = slog.New(fallback)
	t.Cleanup(func() { logger.Log = prev })

	srv := NewServer(&engineio.Options{Logger: slog.New(custom)})
	srv.OnConnect("/", func(Conn) error { return nil })
	go func() { _ = srv.Serve() }()
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// A raw engine.io client sends a CONNECT for a namespace that has no
	// handler; the server logs the failure with namespace=/nope.
	dialer := engineio.Dialer{Transports: []transport.Transport{polling.Default}}
	conn, err := dialer.Dial(ts.URL, nil)
	require.NoError(t, err)
	defer conn.Close()

	w, err := conn.NextWriter(session.TEXT)
	require.NoError(t, err)
	_, err = w.Write([]byte("0/nope"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	require.Eventually(t, func() bool { return custom.hasAttr("namespace", "/nope") },
		5*time.Second, 20*time.Millisecond, "custom logger did not receive the namespace error")
	require.False(t, fallback.hasAttr("namespace", "/nope"),
		"error was also written to the package-level logger")
}

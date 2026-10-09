// Package logtest records log output for the tests of engineio and engineio/client.
// It imports no package of this repository, so in-package tests can use it.
package logtest

import (
	"context"
	"log"
	"log/slog"
	"sync"
	"testing"
)

// Recorder keeps each record as its message, level and attributes, including
// the attributes added through WithAttrs.
type Recorder struct {
	mu    *sync.Mutex
	recs  *[]map[string]string
	attrs []slog.Attr
}

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder { return &Recorder{mu: new(sync.Mutex), recs: new([]map[string]string)} }

// Enabled reports true for every level.
func (h *Recorder) Enabled(context.Context, slog.Level) bool { return true }

// Handle records r.
func (h *Recorder) Handle(_ context.Context, r slog.Record) error {
	m := map[string]string{"msg": r.Message, "level": r.Level.String()}
	add := func(a slog.Attr) bool { m[a.Key] = a.Value.String(); return true }
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(add)
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.recs = append(*h.recs, m)
	return nil
}

// WithAttrs returns a Recorder that adds attrs to every record it handles.
func (h *Recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

// WithGroup returns h: groups are not recorded.
func (h *Recorder) WithGroup(string) slog.Handler { return h }

// SetDefault makes h the default handler until Cleanup, which also restores the log
// package's output and flags that slog.SetDefault changes; not for parallel tests.
func SetDefault(t *testing.T, h slog.Handler) {
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev); log.SetOutput(prevOut); log.SetFlags(prevFlags) })
}

// Find returns the records of msg, or all records if msg is empty.
func (h *Recorder) Find(msg string) (out []map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range *h.recs {
		if msg == "" || m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

package logger

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// keptRecords keeps each record with the attributes added through With.
type keptRecords struct {
	mu    *sync.Mutex
	recs  *[]map[string]slog.Value
	attrs []slog.Attr
}

func newKeptRecords() *keptRecords {
	return &keptRecords{mu: &sync.Mutex{}, recs: &[]map[string]slog.Value{}}
}

func (h *keptRecords) Enabled(context.Context, slog.Level) bool { return true }

func (h *keptRecords) Handle(_ context.Context, r slog.Record) error {
	m := map[string]slog.Value{}
	for _, a := range h.attrs {
		m[a.Key] = a.Value
	}
	r.Attrs(func(a slog.Attr) bool { m[a.Key] = a.Value; return true })
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.recs = append(*h.recs, m)
	return nil
}

func (h *keptRecords) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *keptRecords) WithGroup(string) slog.Handler { return h }

func (h *keptRecords) last() map[string]slog.Value {
	h.mu.Lock()
	defer h.mu.Unlock()
	return (*h.recs)[len(*h.recs)-1]
}

type longValuer struct{ s string }

func (v longValuer) LogValue() slog.Value { return slog.StringValue(v.s) }

// TestRecordValuesAreBounded checks that every string or error attribute value of a
// record logged through a wrapped logger is at most maxValueBytes long: a longer one is
// cut at a UTF-8 boundary and ends with "...", and a shorter one reaches the handler
// unchanged, an error still as an error.
func TestRecordValuesAreBounded(t *testing.T) {
	rec := newKeptRecords()
	log := Wrap(slog.New(rec))
	long := strings.Repeat("e", 900_000)
	// A 3-byte rune that straddles the cut must not be split.
	runes := strings.Repeat("a", maxValueBytes-4) + strings.Repeat("€", 10)
	short := errors.New("short")

	log.With("transport", long).Warn("socketio: test",
		"nsp", "/"+long, "event", runes, "err", errors.New(long), "short", short,
		"remote_addr", longValuer{long}, "n", 7, slog.Group("g", "event", long))
	m := rec.last()
	for _, key := range []string{"transport", "nsp", "event", "err", "remote_addr"} {
		s := m[key].Resolve().String()
		require.LessOrEqual(t, len(s), maxValueBytes, key)
		require.True(t, strings.HasSuffix(s, "..."), "%s ends with %q", key, s[len(s)-8:])
		require.True(t, utf8.ValidString(s), key)
	}
	require.Equal(t, "/"+strings.Repeat("e", maxValueBytes-4)+"...", m["nsp"].String())
	require.Equal(t, strings.Repeat("a", maxValueBytes-4)+"...", m["event"].String(), "a rune is not split")
	require.Same(t, short, m["short"].Any(), "a short error reaches the handler unchanged")
	require.Equal(t, int64(7), m["n"].Int64())
	group := m["g"].Group()
	require.Len(t, group, 1)
	require.Len(t, group[0].Value.String(), maxValueBytes)

	// A value of exactly the bound is not cut.
	exact := strings.Repeat("x", maxValueBytes)
	log.Debug("socketio: test", "nsp", exact)
	require.Equal(t, exact, rec.last()["nsp"].String())

	// Log, the fallback logger, bounds the records it passes to slog.Default.
	setOverride(t, LevelUnset)
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	fallback := newKeptRecords()
	slog.SetDefault(slog.New(fallback))
	Log.With("transport", long).Debug("engineio: test", "nsp", long)
	require.Len(t, fallback.last()["transport"].String(), maxValueBytes)
	require.Len(t, fallback.last()["nsp"].String(), maxValueBytes)
}

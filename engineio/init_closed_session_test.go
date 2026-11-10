package engineio_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

// recordList collects the records of a logger: message, level and sid/reason attributes.
type recordList struct {
	mu   *sync.Mutex
	recs *[]map[string]string
}

func newRecordList() recordList {
	return recordList{mu: new(sync.Mutex), recs: new([]map[string]string)}
}

func (h recordList) Enabled(context.Context, slog.Level) bool { return true }
func (h recordList) Handle(_ context.Context, r slog.Record) error {
	m := map[string]string{"msg": r.Message, "level": r.Level.String()}
	r.Attrs(func(a slog.Attr) bool { m[a.Key] = a.Value.String(); return true })
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.recs = append(*h.recs, m)
	return nil
}
func (h recordList) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordList) WithGroup(string) slog.Handler      { return h }

// above returns the records logged above DEBUG.
func (h recordList) above() (out []map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range *h.recs {
		if m["level"] != slog.LevelDebug.String() {
			out = append(out, m)
		}
	}
	return out
}

// A session that was open, and then closed by someone else while its handshake
// response was still being delivered, is not a rejected request: the close is
// the session's own record, the request is not reported to ConnectionError or at
// WARN, and the session is neither registered nor accepted afterwards.
func TestSessionClosedDuringHandshakeIsNotRejected(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close func(t *testing.T, s *engineio.Server, c engineio.Conn)
	}{
		{name: "session close", close: func(t *testing.T, _ *engineio.Server, c engineio.Conn) {
			require.NoError(t, c.Close())
		}},
		{name: "session abort", close: func(t *testing.T, _ *engineio.Server, c engineio.Conn) {
			require.NoError(t, engineio.Abort(c))
		}},
		{name: "server close", close: func(t *testing.T, s *engineio.Server, _ engineio.Conn) {
			go func() { _ = s.Close() }()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecordList()
			var rejected atomic.Int32
			s, release := newHeldOpenServer(t,
				engineio.WithLogger(slog.New(rec)),
				engineio.WithHooks(&engineio.Hooks{ConnectionError: func(*http.Request, int, string, error) { rejected.Add(1) }}))
			ts := httptest.NewServer(s)
			defer ts.Close()
			defer func() { _ = s.Close() }()
			defer release()

			sid := handshake(t, ts.URL)
			conn, ok := s.Clients()[sid]
			require.True(t, ok, "session is not listed after the client received OPEN")
			tc.close(t, s, conn)
			release()

			require.Eventually(t, func() bool { return s.Count() == 0 }, 5*time.Second, time.Millisecond,
				"closed session stays registered")
			// Init removes the session first and reports the failure after it; give a report
			// that is going to be made time to arrive.
			time.Sleep(100 * time.Millisecond)
			require.Empty(t, rec.above(), "a session that opened and closed was logged as a failure")
			require.Zero(t, rejected.Load(), "a session that opened and closed was reported to ConnectionError")
		})
	}
}

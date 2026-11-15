package engineio

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type emptyIDGenerator struct{}

func (emptyIDGenerator) NewID() string { return "" }

// TestServerSideRejectionsWarn checks the two request rejections whose cause is on the
// server's side, which stay at WARN while every rejection a peer causes is DEBUG: a
// websocket upgrade through a ResponseWriter that is no http.Hijacker, and a handshake
// for which the session ID generator returns no identifier. A handshake that finds the
// server closed while it creates the session is DEBUG.
func TestServerSideRejectionsWarn(t *testing.T) {
	var closing *Server
	closeServer := WithRequestChecker(func(*http.Request) (http.Header, error) { return nil, closing.Close() })
	upgrade := func(r *http.Request) *http.Request {
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Sec-WebSocket-Version", "13")
		r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		return r
	}
	for _, tc := range []struct {
		name, reason, level string
		opts                []Option
		req                 *http.Request
	}{
		{"websocket without hijacker", "accept", "WARN", nil,
			upgrade(httptest.NewRequest(http.MethodGet, "/?EIO=3&transport=websocket", nil))},
		{"non-upgrade websocket GET without hijacker", "accept", "DEBUG", nil,
			httptest.NewRequest(http.MethodGet, "/?EIO=3&transport=websocket", nil)},
		{"no session id", "init", "WARN", []Option{WithSessionIDGenerator(emptyIDGenerator{})},
			httptest.NewRequest(http.MethodGet, "/?EIO=3&transport=polling", nil)},
		{"server closed during the handshake", "init", "DEBUG", []Option{closeServer},
			httptest.NewRequest(http.MethodGet, "/?EIO=3&transport=polling", nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder()
			s := NewServer(append(tc.opts, WithLogger(slog.New(rec)))...)
			closing = s
			t.Cleanup(func() { _ = s.Close() })
			w := httptest.NewRecorder() // no http.Hijacker
			s.ServeHTTP(w, tc.req)
			require.GreaterOrEqual(t, w.Code, http.StatusBadRequest)
			got := rec.find("engineio: request rejected")
			require.Len(t, got, 1)
			require.Equal(t, tc.reason, got[0]["reason"])
			require.Equal(t, tc.level, got[0]["level"])
		})
	}
}

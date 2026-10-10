package engineio_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/internal/logtest"
)

// plainWriter hides http.Hijacker, as a wrapping middleware or HTTP/2 does.
type plainWriter struct{ http.ResponseWriter }

// TestWebsocketWithoutHijacker checks that a response writer that cannot be hijacked
// is answered with HTTP 501 and a logged reason, never a panic.
func TestWebsocketWithoutHijacker(t *testing.T) {
	rec := logtest.NewRecorder()
	srv := engineio.NewServer(&engineio.Options{Logger: slog.New(rec)})
	defer func() { _ = srv.Close() }()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/engine.io/?transport=websocket&EIO=4", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	require.NotPanics(t, func() { srv.ServeHTTP(plainWriter{w}, r) })

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	got := rec.Find("engineio: request rejected")
	require.Len(t, got, 1)
	assert.Equal(t, "no hijacker", got[0]["reason"])
	assert.Equal(t, "websocket", got[0]["transport"])
	assert.Equal(t, slog.LevelWarn.String(), got[0]["level"])
	assert.Zero(t, srv.Count(), "no session may be created")
}

// TestUpgradeWithoutHijacker covers the second accept site: an upgrade request of a
// live polling session.
func TestUpgradeWithoutHijacker(t *testing.T) {
	rec := logtest.NewRecorder()
	srv := engineio.NewServer(&engineio.Options{Logger: slog.New(rec)})
	defer func() { _ = srv.Close() }()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/?transport=polling&EIO=4")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	m := regexp.MustCompile(`"sid":"([^"]+)"`).FindSubmatch(body)
	require.NotNil(t, m, "no sid in %q", body)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/?transport=websocket&EIO=4&sid="+string(m[1]), nil)
	require.NotPanics(t, func() { srv.ServeHTTP(plainWriter{w}, r) })

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	got := rec.Find("engineio: request rejected")
	require.Len(t, got, 1)
	assert.Equal(t, "no hijacker", got[0]["reason"])
}

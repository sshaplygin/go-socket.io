package engineio_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/internal/logtest"
)

// TestHandshakeGate checks the answers of ServeHTTP that carry an Engine.IO v4 error
// body, with the status, the code, the logged reason and the level of the record.
func TestHandshakeGate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		method  string
		query   string
		checker engineio.CheckerFunc
		status  int
		body    string
		reason  string
		level   slog.Level
	}{
		{"EIO missing", http.MethodGet, "transport=polling", nil, 400,
			`{"code":5,"message":"Unsupported protocol version"}`, "bad eio", slog.LevelWarn},
		{"EIO 3", http.MethodGet, "transport=polling&EIO=3", nil, 400,
			`{"code":5,"message":"Unsupported protocol version"}`, "bad eio", slog.LevelWarn},
		{"EIO 5", http.MethodGet, "transport=polling&EIO=5", nil, 400,
			`{"code":5,"message":"Unsupported protocol version"}`, "bad eio", slog.LevelWarn},
		{"EIO checked before transport", http.MethodGet, "transport=nope&EIO=3", nil, 400,
			`{"code":5,"message":"Unsupported protocol version"}`, "bad eio", slog.LevelWarn},
		{"unknown transport", http.MethodGet, "transport=nope&EIO=4", nil, 400,
			`{"code":0,"message":"Transport unknown"}`, "bad transport", slog.LevelWarn},
		{"unknown sid", http.MethodGet, "transport=polling&EIO=4&sid=nope", nil, 400,
			`{"code":1,"message":"Session ID unknown"}`, "unknown sid", slog.LevelDebug},
		{"POST handshake", http.MethodPost, "transport=polling&EIO=4", nil, 400,
			`{"code":2,"message":"Bad handshake method"}`, "bad handshake method", slog.LevelWarn},
		{"PUT handshake", http.MethodPut, "transport=websocket&EIO=4", nil, 400,
			`{"code":2,"message":"Bad handshake method"}`, "bad handshake method", slog.LevelWarn},
		{"request checker", http.MethodGet, "transport=polling&EIO=4",
			func(*http.Request) (http.Header, error) { return nil, errors.New("denied") }, 403,
			`{"code":4,"message":"Forbidden"}`, "checker", slog.LevelWarn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := logtest.NewRecorder()
			srv := engineio.NewServer(&engineio.Options{Logger: slog.New(rec), RequestChecker: tc.checker})
			defer func() { _ = srv.Close() }()

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, httptest.NewRequest(tc.method, "/engine.io/?"+tc.query, nil))

			assert.Equal(t, tc.status, w.Code)
			assert.JSONEq(t, tc.body, w.Body.String())
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			assert.Zero(t, srv.Count(), "no session may be created")

			got := rec.Find("engineio: request rejected")
			require.Len(t, got, 1)
			assert.Equal(t, tc.reason, got[0]["reason"])
			assert.Equal(t, tc.level.String(), got[0]["level"])
		})
	}
}

// TestHandshakeEIO4 checks that EIO=4 opens a session.
func TestHandshakeEIO4(t *testing.T) {
	srv := engineio.NewServer(nil)
	defer func() { _ = srv.Close() }()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/?transport=polling&EIO=4")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, strings.HasPrefix(string(body), "0{"), "open packet expected, got %q", body)
	assert.Equal(t, 1, srv.Count())
}

// TestHandshakePreflight checks that an OPTIONS request without a sid is not
// answered as a bad handshake method.
func TestHandshakePreflight(t *testing.T) {
	srv := engineio.NewServer(nil)
	defer func() { _ = srv.Close() }()

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/?transport=polling&EIO=4", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

package engineio_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/engineio/internal/logtest"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
)

// TestHandshakeGate checks the answers of ServeHTTP that carry an Engine.IO v4 error
// body, with the status, the code, the logged reason and the level of the record.
func TestHandshakeGate(t *testing.T) {
	const badEIO = `{"code":5,"message":"Unsupported protocol version"}`
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
		{"EIO missing", http.MethodGet, "transport=polling", nil, 400, badEIO, "bad eio", slog.LevelWarn},
		{"EIO 3", http.MethodGet, "transport=polling&EIO=3", nil, 400, badEIO, "bad eio", slog.LevelWarn},
		{"EIO 5", http.MethodGet, "transport=polling&EIO=5", nil, 400, badEIO, "bad eio", slog.LevelWarn},
		{"EIO checked before transport", http.MethodGet, "transport=nope&EIO=3", nil, 400, badEIO, "bad eio", slog.LevelWarn},
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

// TestHandshakeBadUpgrade checks that a request moving a session to a transport it
// cannot upgrade to is answered with code 3: here websocket comes before polling, so a
// polling session has no upgrade.
func TestHandshakeBadUpgrade(t *testing.T) {
	rec := logtest.NewRecorder()
	srv := engineio.NewServer(&engineio.Options{
		Logger:     slog.New(rec),
		Transports: []transport.Transport{websocket.Default, polling.Default},
	})
	defer func() { _ = srv.Close() }()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	sid := openPolling(t, ts.URL)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/?transport=websocket&EIO=4&sid="+sid, nil))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"code":3,"message":"Bad request"}`, w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	got := rec.Find("engineio: request rejected")
	require.Len(t, got, 1)
	assert.Equal(t, "bad upgrade", got[0]["reason"])
	assert.Equal(t, slog.LevelWarn.String(), got[0]["level"])
}

// noDeadlineTransport is polling whose connections refuse a deadline, so no session can start.
type noDeadlineTransport struct{ transport.Transport }

func (noDeadlineTransport) Accept(http.ResponseWriter, *http.Request) (transport.Conn, error) {
	return noDeadlineConn{}, nil
}

type noDeadlineConn struct{ transport.Conn }

func (noDeadlineConn) SetReadDeadline(time.Time) error { return errors.New("no deadline") }
func (noDeadlineConn) Close() error                    { return nil }

// TestHandshakeInitFailure checks that a session that cannot start is answered with code 3.
func TestHandshakeInitFailure(t *testing.T) {
	rec := logtest.NewRecorder()
	srv := engineio.NewServer(&engineio.Options{
		Logger:     slog.New(rec),
		Transports: []transport.Transport{noDeadlineTransport{polling.Default}},
	})
	defer func() { _ = srv.Close() }()

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/?transport=polling&EIO=4", nil))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"code":3,"message":"Bad request"}`, w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Zero(t, srv.Count(), "no session may be created")
	got := rec.Find("engineio: request rejected")
	require.Len(t, got, 1)
	assert.Equal(t, "init", got[0]["reason"])
}

// TestHandshakeEIO4 checks that EIO=4 opens a session.
func TestHandshakeEIO4(t *testing.T) {
	srv := engineio.NewServer(nil)
	defer func() { _ = srv.Close() }()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	openPolling(t, ts.URL)
	assert.Equal(t, 1, srv.Count())
}

// TestHandshakePreflight checks that an OPTIONS request without a sid is answered as a
// CORS preflight, not as a bad handshake method, and opens no session.
func TestHandshakePreflight(t *testing.T) {
	srv := engineio.NewServer(nil)
	defer func() { _ = srv.Close() }()

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/?transport=polling&EIO=4", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Content-Type", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Zero(t, srv.Count(), "a preflight opens no session")
}

// openPolling performs a polling handshake with EIO=4 against url, checks that it is
// answered with an OPEN packet and returns the session id.
func openPolling(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url + "/?transport=polling&EIO=4")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Equal(t, http.StatusOK, resp.StatusCode, "body %q", body)
	require.True(t, strings.HasPrefix(string(body), "0{"), "open packet expected, got %q", body)
	m := regexp.MustCompile(`"sid":"([^"]+)"`).FindSubmatch(body)
	require.NotNil(t, m, "no sid in %q", body)
	return string(m[1])
}

package client_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// affinityServer serves a Socket.IO server whose "echo" event acknowledges its
// argument, and passes every request to observe first.
func affinityServer(t *testing.T, observe func(*http.Request)) *httptest.Server {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := socketio.NewServer(socketio.WithLogger(quiet), socketio.WithEngineOptions(
		append(append([]engineio.Option{}, steadyPing...), engineio.WithLogger(quiet))...))
	srv.OnConnect("/", func(socketio.Conn) error { return nil })
	srv.OnEvent("/", "echo", func(_ socketio.Conn, value string) string { return value })
	go func() { _ = srv.Serve() }()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe(r)
		srv.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })
	return ts
}

// The Go client with its default transports does not send the io cookie that
// the polling handshake sets: neither the session's polling requests nor its
// websocket upgrade request carry a Cookie header, so cookie affinity cannot
// route them (docs/V1.md, Deployment).
func TestDefaultTransportsSendNoSessionCookie(t *testing.T) {
	var polls, upgrades, cookies atomic.Int32
	polled, upgraded := make(chan struct{}), make(chan struct{})
	var pollOnce, upgradeOnce sync.Once
	ts := affinityServer(t, func(r *http.Request) {
		if r.URL.Query().Get("sid") == "" {
			return
		}
		if r.Header.Get("Cookie") != "" {
			cookies.Add(1)
		}
		if r.URL.Query().Get("transport") == "websocket" {
			// The probe can start before the session's first poll: hold it
			// until that poll has been recorded.
			select {
			case <-polled:
			case <-r.Context().Done():
			}
			upgrades.Add(1)
			upgradeOnce.Do(func() { close(upgraded) })
			return
		}
		polls.Add(1)
		pollOnce.Do(func() { close(polled) })
	})

	// The handshake does offer the cookie.
	resp, err := http.Get(ts.URL + "/socket.io/?EIO=3&transport=polling")
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	require.NoError(t, resp.Body.Close())
	require.Len(t, resp.Cookies(), 1)
	require.Equal(t, "io", resp.Cookies()[0].Name)

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	cl, err := client.New(ts.URL, client.WithReconnection(false), client.WithLogger(quiet))
	require.NoError(t, err)
	require.NoError(t, cl.Connect())
	t.Cleanup(func() { _ = cl.Close() })

	recv(t, polled, "a polling request of the session")
	recv(t, upgraded, "the websocket upgrade request")
	require.Positive(t, polls.Load())
	require.Positive(t, upgrades.Load())
	require.Zero(t, cookies.Load(), "requests with a Cookie header")
}

// A Go client that connects over websocket only makes one HTTP request for its
// session, so a load balancer needs no affinity for it (docs/V1.md, Deployment).
func TestWebsocketOnlyClientMakesOneRequest(t *testing.T) {
	var requests atomic.Int32
	ts := affinityServer(t, func(*http.Request) { requests.Add(1) })

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	cl, err := client.New(ts.URL, client.WithReconnection(false), client.WithLogger(quiet),
		client.WithTransports(websocket.Default))
	require.NoError(t, err)
	require.NoError(t, cl.Connect())
	t.Cleanup(func() { _ = cl.Close() })

	// The round trip shows the session works after its only request.
	acked := make(chan string, 1)
	cl.Emit("echo", "hello", func(value string) { acked <- value })
	require.Equal(t, "hello", recv(t, acked, "the acknowledgement"))
	require.EqualValues(t, 1, requests.Load())
}

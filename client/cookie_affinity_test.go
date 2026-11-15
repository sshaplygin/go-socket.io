package client_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// The polling transport sends the io cookie of the handshake only through an
// http.Client with a cookie jar, and the websocket transport sends no cookie: the
// upgrade request of a client whose polling requests carry the cookie has none
// (docs/V1.md, Deployment).
func TestWebsocketUpgradeCarriesNoCookie(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := socketio.NewServer(socketio.WithLogger(quiet), socketio.WithEngineOptions(engineio.WithLogger(quiet)))
	srv.OnConnect("/", func(socketio.Conn) error { return nil })
	go func() { _ = srv.Serve() }()
	type seen struct{ sid, cookie string }
	polls, upgrades := make(chan seen, 1), make(chan seen, 1)
	polled := make(chan struct{})
	var once sync.Once
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sid := r.URL.Query().Get("sid"); sid != "" {
			req := seen{sid, r.Header.Get("Cookie")}
			if r.URL.Query().Get("transport") == "websocket" {
				// The probe can start before the session's first poll: hold it
				// until that poll has been recorded.
				select {
				case <-polled:
				case <-r.Context().Done():
				}
				select {
				case upgrades <- req:
				default:
				}
			} else {
				once.Do(func() { polls <- req; close(polled) })
			}
		}
		srv.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	cl, err := client.New(ts.URL, client.WithReconnection(false), client.WithLogger(quiet),
		client.WithTransports(&polling.Transport{Client: &http.Client{Jar: jar}}, websocket.Default))
	require.NoError(t, err)
	require.NoError(t, cl.Connect())
	t.Cleanup(func() { _ = cl.Close() })

	poll := receiveRequest(t, polls, "a polling request of the session")
	require.Equal(t, "io="+poll.sid, poll.cookie)
	upgrade := receiveRequest(t, upgrades, "the websocket upgrade request")
	require.Equal(t, poll.sid, upgrade.sid)
	require.Empty(t, upgrade.cookie)
}

func receiveRequest[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}

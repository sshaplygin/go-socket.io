package client_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

// With an http.Server WriteTimeout shorter than the time a poll waits, the
// response to a pending poll is lost (docs/V1.md, Deployment). The Go client's
// reason depends on what is lost: an idle client whose PONG is lost reports
// "ping timeout" and the server "transport error"; when an event is lost and
// then the PONG on the poll that http.Transport retried, the client reports
// "transport error" and the server "ping timeout" after its heartbeat window.
func TestPollingClientReasonsUnderServerWriteTimeout(t *testing.T) {
	for _, tc := range []struct {
		name           string
		emit           bool
		client, server string
	}{
		{"idle client loses the PONG", false, "ping timeout", "transport error"},
		{"event and PONG lost", true, "transport error", "ping timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const writeTimeout = 500 * time.Millisecond
			quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
			srv := socketio.NewServer(socketio.WithLogger(quiet), socketio.WithEngineOptions(
				engineio.WithLogger(quiet),
				engineio.WithPingInterval(3*time.Second), engineio.WithPingTimeout(2*time.Second)))
			conns, serverReasons := make(chan socketio.Conn, 1), make(chan string, 1)
			srv.OnConnect("/", func(c socketio.Conn) error { conns <- c; return nil })
			srv.OnDisconnect("/", func(_ socketio.Conn, reason string) { serverReasons <- reason })
			go func() { _ = srv.Serve() }()
			var pendingSince atomic.Int64 // when the poll inside the handler arrived
			ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				poll := r.Method == http.MethodGet && r.URL.Query().Get("sid") != ""
				if poll {
					pendingSince.Store(time.Now().UnixNano())
				}
				srv.ServeHTTP(w, r)
				if poll {
					pendingSince.Store(0)
				}
			}))
			ts.Config.WriteTimeout = writeTimeout
			ts.Start()
			t.Cleanup(func() { _ = srv.Close(); ts.Close() })

			cl, err := client.New(ts.URL, client.WithTransports(polling.Default),
				client.WithReconnection(false), client.WithLogger(quiet))
			require.NoError(t, err)
			clientReasons := make(chan string, 1)
			cl.OnDisconnect(func(_ client.Conn, reason string) { clientReasons <- reason })
			require.NoError(t, cl.Connect())
			t.Cleanup(func() { _ = cl.Close() })
			c := <-conns

			if tc.emit {
				// Emit into a poll that has waited past WriteTimeout; the client's
				// ping, PingInterval after the handshake, is still a second away.
				require.Eventually(t, func() bool {
					since := pendingSince.Load()
					return since != 0 && time.Since(time.Unix(0, since)) >= 2*writeTimeout
				}, 5*time.Second, 10*time.Millisecond)
				c.Emit("push", "x")
			}
			select {
			case reason := <-clientReasons:
				require.Equal(t, tc.client, reason)
			case <-time.After(30 * time.Second):
				t.Fatal("the client did not disconnect")
			}
			select {
			case reason := <-serverReasons:
				require.Equal(t, tc.server, reason)
			case <-time.After(30 * time.Second):
				t.Fatal("the server did not disconnect")
			}
		})
	}
}

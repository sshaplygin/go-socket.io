package client_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
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
			// The second case needs the emit to precede the client's ping, which
			// normally comes about four seconds later. A host too loaded to emit
			// in time repeats the attempt and then leaves the case undecided.
			for attempt := 0; attempt < 3; attempt++ {
				if pollingClientReasons(t, tc.emit, tc.client, tc.server) {
					return
				}
			}
			t.Skip("no attempt emitted before the client's ping")
		})
	}
}

// pollingClientReasons runs one case and reports false when the emit came too
// late to precede the client's ping, which leaves the case undecided.
func pollingClientReasons(t *testing.T, emit bool, clientReason, serverReason string) bool {
	const (
		writeTimeout = 500 * time.Millisecond
		pingInterval = 5 * time.Second
	)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := socketio.NewServer(socketio.WithLogger(quiet), socketio.WithEngineOptions(
		engineio.WithLogger(quiet),
		engineio.WithPingInterval(pingInterval), engineio.WithPingTimeout(time.Second)))
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
	stop := sync.OnceFunc(func() { _ = srv.Close(); ts.Close() })
	t.Cleanup(stop)

	cl, err := client.New(ts.URL, client.WithTransports(polling.Default),
		client.WithReconnection(false), client.WithLogger(quiet))
	require.NoError(t, err)
	clientReasons := make(chan string, 1)
	cl.OnDisconnect(func(_ client.Conn, reason string) { clientReasons <- reason })
	// The client pings pingInterval after the handshake, which starts after this.
	started := time.Now()
	require.NoError(t, cl.Connect())
	t.Cleanup(func() { _ = cl.Close() })
	var c socketio.Conn
	select {
	case c = <-conns:
	case <-time.After(30 * time.Second):
		t.Fatal("the server did not see the connection")
	}

	if emit {
		// Emit into a poll that has waited past WriteTimeout, at least half a
		// second before the client's ping.
		require.Eventually(t, func() bool {
			since := pendingSince.Load()
			return since != 0 && time.Since(time.Unix(0, since)) >= 2*writeTimeout
		}, 5*time.Second, 10*time.Millisecond)
		c.Emit("push", "x")
		if time.Since(started) > pingInterval-500*time.Millisecond {
			_ = cl.Close()
			stop()
			return false
		}
	}
	select {
	case reason := <-clientReasons:
		require.Equal(t, clientReason, reason)
	case <-time.After(30 * time.Second):
		t.Fatal("the client did not disconnect")
	}
	select {
	case reason := <-serverReasons:
		require.Equal(t, serverReason, reason)
	case <-time.After(30 * time.Second):
		t.Fatal("the server did not disconnect")
	}
	return true
}

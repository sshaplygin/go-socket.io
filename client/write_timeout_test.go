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
// response to a pending poll over HTTP/1.x is lost (docs/V1.md, Deployment). The Go client's
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
			// normally comes about four seconds later, and the retried poll to have
			// waited past WriteTimeout by then. A host too loaded for that repeats
			// the attempt and then leaves the case undecided.
			for attempt := 0; attempt < 3; attempt++ {
				if pollingClientReasons(t, tc.emit, tc.client, tc.server) {
					return
				}
			}
			t.Skip("no attempt lost the event and then the PONG")
		})
	}
}

// pollingClientReasons runs one case. It reports false, which leaves the case
// undecided, when the client's ping did not find the event already emitted and
// the retried poll pending for longer than WriteTimeout.
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
	var (
		pendingSince atomic.Int64 // when the poll inside the handler arrived
		emitted      atomic.Bool  // the event has been emitted
		pinged       atomic.Bool  // the client's ping has arrived
		undecided    atomic.Bool  // the ping could not lose its PONG after the event
	)
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sid") == "" {
			srv.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet {
			arrived := time.Now().UnixNano()
			pendingSince.Store(arrived)
			srv.ServeHTTP(w, r)
			pendingSince.CompareAndSwap(arrived, 0) // a retried poll may have replaced it
			return
		}
		// The first POST of this polling-only client is its ping. net/http started
		// the pending poll's WriteTimeout before the handler saw that poll.
		if emit && pinged.CompareAndSwap(false, true) {
			since := pendingSince.Load()
			if !emitted.Load() || since == 0 || time.Since(time.Unix(0, since)) < writeTimeout {
				undecided.Store(true)
			}
		}
		srv.ServeHTTP(w, r)
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
	require.NoError(t, cl.Connect())
	t.Cleanup(func() { _ = cl.Close() })
	var c socketio.Conn
	select {
	case c = <-conns:
	case <-time.After(30 * time.Second):
		t.Fatal("the server did not see the connection")
	}

	if emit {
		// Emit into a poll that has waited past WriteTimeout.
		require.Eventually(t, func() bool {
			since := pendingSince.Load()
			return since != 0 && time.Since(time.Unix(0, since)) >= 2*writeTimeout
		}, 5*time.Second, 10*time.Millisecond)
		c.Emit("push", "x")
		emitted.Store(true)
	}
	select {
	case reason := <-clientReasons:
		// The ping precedes the client's disconnect in both outcomes.
		if undecided.Load() {
			_ = cl.Close()
			stop()
			return false
		}
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

// Over HTTP/2 net/http resets the stream of a poll that is still pending when
// the http.Server's WriteTimeout expires (docs/V1.md, Deployment): an idle Go
// client and the server both report "transport error" at that moment, long
// before the heartbeat window ends.
func TestPollingClientOverHTTP2EndsAtServerWriteTimeout(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := socketio.NewServer(socketio.WithLogger(quiet), socketio.WithEngineOptions(
		engineio.WithLogger(quiet),
		engineio.WithPingInterval(time.Minute), engineio.WithPingTimeout(time.Minute)))
	serverReasons := make(chan string, 1)
	srv.OnConnect("/", func(socketio.Conn) error { return nil })
	srv.OnDisconnect("/", func(_ socketio.Conn, reason string) { serverReasons <- reason })
	go func() { _ = srv.Serve() }()
	protos := make(chan string, 16)
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case protos <- r.Proto:
		default:
		}
		srv.ServeHTTP(w, r)
	}))
	ts.EnableHTTP2 = true
	ts.Config.WriteTimeout = 2 * time.Second
	ts.StartTLS()
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })

	cl, err := client.New(ts.URL, client.WithTransports(&polling.Transport{Client: ts.Client()}),
		client.WithReconnection(false), client.WithLogger(quiet))
	require.NoError(t, err)
	clientReasons := make(chan string, 1)
	cl.OnDisconnect(func(_ client.Conn, reason string) { clientReasons <- reason })
	require.NoError(t, cl.Connect())
	t.Cleanup(func() { _ = cl.Close() })
	for side, reasons := range map[string]chan string{"client": clientReasons, "server": serverReasons} {
		select {
		case reason := <-reasons:
			require.Equal(t, "transport error", reason, side)
		case <-time.After(30 * time.Second):
			t.Fatalf("the %s did not disconnect", side)
		}
	}
	require.NotEmpty(t, protos)
	for len(protos) > 0 {
		require.Equal(t, "HTTP/2.0", <-protos)
	}
}

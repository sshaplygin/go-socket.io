package client_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

// TestPollingCookieJarTraffic runs polling traffic through an http.Client with
// a cookie jar. The jar adds cookies to every request; the polling GET and
// POST requests shared one header map, which raced (#285).
func TestPollingCookieJarTraffic(t *testing.T) {
	s := socketio.NewServer()
	conns := make(chan socketio.Conn, 1)
	s.OnConnect("/", func(c socketio.Conn) error { conns <- c; return nil })
	s.OnEvent("/", "up", func(c socketio.Conn, v int) { c.Emit("down", v) })
	go func() { _ = s.Serve() }()
	ts := httptest.NewServer(s)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	httpClient := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	t.Cleanup(func() { _ = s.Close(); ts.Close(); httpClient.CloseIdleConnections() })

	cl, err := client.New(ts.URL, client.WithReconnection(false),
		client.WithTransports(&polling.Transport{Client: httpClient}))
	require.NoError(t, err)
	down := make(chan int, 64)
	cl.OnEvent("down", func(_ client.Conn, v int) { down <- v })
	require.NoError(t, cl.Connect())
	t.Cleanup(func() { _ = cl.Close() })
	select {
	case <-conns:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not see the connection")
	}

	// Keep eight events in flight, so that POSTs overlap pending polls.
	n := 0
	for ; n < 8; n++ {
		cl.Emit("up", n)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); n++ {
		select {
		case <-down:
		case <-time.After(5 * time.Second):
			t.Fatal("traffic stalled")
		}
		cl.Emit("up", n)
	}
}

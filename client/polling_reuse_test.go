package client_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/stretchr/testify/require"
)

func TestPollingClientsReuseConnections(t *testing.T) {
	srv := socketio.NewServer(socketio.WithEngineOptions(engineio.WithPingInterval(time.Hour)))
	srv.OnConnect("/", func(socketio.Conn) error { return nil })
	go func() { _ = srv.Serve() }()
	var dials atomic.Int64
	ts := httptest.NewUnstartedServer(srv)
	ts.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			dials.Add(1)
		}
	}
	ts.Start()
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })
	deliveries := make(chan int, 10)
	connected := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		cl, err := client.New(ts.URL, client.WithTransports(polling.Default))
		require.NoError(t, err)
		t.Cleanup(func() { _ = cl.Close() })
		cl.OnConnect(func(client.Conn) error { connected <- struct{}{}; return nil })
		cl.OnEvent("sequence", func(_ client.Conn, n int) { deliveries <- n })
		require.NoError(t, cl.Connect())
	}
	for i := 0; i < 10; i++ {
		select {
		case <-connected:
		case <-time.After(5 * time.Second):
			t.Fatal("connect timed out")
		}
	}
	for n := 0; n < 200; n++ {
		require.True(t, srv.BroadcastToNamespace("/", "sequence", n))
		for i := 0; i < 10; i++ {
			select {
			case got := <-deliveries:
				require.Equal(t, n, got)
			case <-time.After(5 * time.Second):
				t.Fatal("broadcast timed out")
			}
		}
	}
	require.LessOrEqual(t, dials.Load(), int64(40), "polling clients should reuse pooled connections")
}

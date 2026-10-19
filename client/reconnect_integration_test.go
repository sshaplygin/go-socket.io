package client_test

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// stop drops the transport without sending namespace DISCONNECT, then releases
// the listener so a new server can bind the very same address.
func reconnectServer(t *testing.T, address string) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", address)
	require.NoError(t, err)
	srv := socketio.NewServer()
	for _, ns := range []string{"/", "/chat", "/gone"} {
		srv.OnConnect(ns, func(socketio.Conn) error { return nil })
		srv.OnEvent(ns, "echo", func(_ socketio.Conn, value string) string { return value })
		srv.OnEvent(ns, "request-disconnect", func(c socketio.Conn) { c.Disconnect() })
	}
	httpServer := &http.Server{Handler: srv, ReadHeaderTimeout: time.Second}
	done := make(chan struct{})
	go func() { _ = srv.Serve() }()
	go func() { defer close(done); _ = httpServer.Serve(listener) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = httpServer.Close()
			for _, c := range srv.Engine().Clients() {
				_ = c.Close()
			}
			_ = srv.Close()
			<-done
		})
	}
	t.Cleanup(stop)
	return listener.Addr().String(), stop
}

func retryOptions() []client.Option {
	return []client.Option{client.WithTransports([]transport.Transport{websocket.Default}...), client.WithTimeout(200 * time.Millisecond), client.WithReconnectionDelay(10 * time.Millisecond), client.WithReconnectionDelayMax(20 * time.Millisecond), client.WithRandomizationFactor(-1)}
}

// Covers C3
func TestClientReconnect(t *testing.T) {
	t.Run("restart", func(t *testing.T) {
		address, stop := reconnectServer(t, "127.0.0.1:0")
		cl, err := client.New("http://"+address, retryOptions()...)
		require.NoError(t, err)
		t.Cleanup(func() { _ = cl.Close() })
		connected, disconnected := make(chan client.Conn, 12), make(chan string, 12)
		root, chat, gone := cl.Socket("/"), cl.Socket("/chat"), cl.Socket("/gone")
		for _, socket := range []*client.Socket{root, chat, gone} {
			socket.OnConnect(func(c client.Conn) error { connected <- c; return nil })
			socket.OnDisconnect(func(c client.Conn, reason string) { disconnected <- c.Namespace() + ":" + reason })
		}
		attempts, failures, recovered := make(chan int, 16), make(chan error, 16), make(chan int, 2)
		cl.OnReconnectAttempt(func(n int) { attempts <- n })
		cl.OnReconnectError(func(err error) { failures <- err })
		reconnectState := make(chan bool, 2)
		cl.OnReconnect(func(n int) {
			reconnectState <- !root.Connected() && root.ID() == ""
			recovered <- n
		})
		require.NoError(t, cl.Connect())
		initial := map[string]client.Conn{}
		for i := 0; i < 3; i++ {
			c := recv(t, connected, "initial namespace")
			initial[c.Namespace()] = c
		}
		gone.Emit("request-disconnect")
		require.Equal(t, "/gone:io server disconnect", recv(t, disconnected, "server namespace disconnect"))
		stop()
		for i := 0; i < 2; i++ {
			reason := recv(t, disconnected, "transport disconnect")
			require.Contains(t, []string{"transport close", "transport error"}, strings.SplitN(reason, ":", 2)[1])
		}
		require.False(t, root.Connected())
		require.False(t, initial["/"].Connected())
		require.Empty(t, root.ID())
		require.Equal(t, 1, recv(t, attempts, "first retry"))
		require.Error(t, recv(t, failures, "failed retry while server down"))
		// Retained namespace Conns continue to buffer ordinary emits during recovery.
		ack := make(chan string, 1)
		initial["/chat"].Emit("echo", "buffered", func(value string) { ack <- value })
		_, stopNew := reconnectServer(t, address)
		defer stopNew()
		require.Positive(t, recv(t, recovered, "reconnect"))
		require.True(t, recv(t, reconnectState, "state before namespace CONNECT"))
		newConns := map[string]client.Conn{}
		for i := 0; i < 2; i++ {
			c := recv(t, connected, "recovered namespace")
			newConns[c.Namespace()] = c
		}
		require.Equal(t, "buffered", recv(t, ack, "buffered event ACK"))
		require.True(t, root.Connected())
		require.Equal(t, newConns["/"].ID(), root.ID())
		require.NotEqual(t, initial["/"].ID(), root.ID())
		require.Equal(t, "/chat#"+root.ID(), chat.ID())
		require.False(t, gone.Connected())
		require.Empty(t, gone.ID())
		require.Empty(t, connected, "server-disconnected namespace stays closed")
	})
	t.Run("attempt limit and explicit retry", func(t *testing.T) {
		address, stop := reconnectServer(t, "127.0.0.1:0")
		opts := retryOptions()
		opts = append(opts, client.WithReconnectionAttempts(2))
		cl, err := client.New("http://"+address, opts...)
		require.NoError(t, err)
		t.Cleanup(func() { _ = cl.Close() })
		ready := make(chan struct{}, 2)
		cl.OnConnect(func(client.Conn) error { ready <- struct{}{}; return nil })
		events := make(chan string, 10)
		cl.OnReconnectAttempt(func(n int) {
			if n == 1 {
				events <- "attempt1"
			} else {
				events <- "attempt2"
			}
		})
		cl.OnReconnectError(func(error) { events <- "error" })
		cl.OnReconnectFailed(func() { events <- "failed" })
		require.NoError(t, cl.Connect())
		recv(t, ready, "initial connect")
		stop()
		for _, want := range []string{"attempt1", "error", "attempt2", "error", "failed"} {
			require.Equal(t, want, recv(t, events, "bounded retries"))
		}
		_, stopNew := reconnectServer(t, address)
		defer stopNew()
		require.NoError(t, cl.Connect())
		recv(t, ready, "explicit Connect after exhausted retries")
		require.Empty(t, events)
	})
	for _, mode := range []string{"disabled", "server disconnect", "client close", "last socket close"} {
		t.Run(mode, func(t *testing.T) {
			address, stop := reconnectServer(t, "127.0.0.1:0")
			opts := retryOptions()
			opts = append(opts, client.WithReconnection(mode != "disabled"))
			cl, err := client.New("http://"+address, opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			ready, disconnected := make(chan struct{}, 1), make(chan string, 2)
			attempts := make(chan int, 1)
			cl.OnConnect(func(client.Conn) error { ready <- struct{}{}; return nil })
			cl.OnDisconnect(func(_ client.Conn, reason string) { disconnected <- reason })
			cl.OnReconnectAttempt(func(n int) { attempts <- n })
			require.NoError(t, cl.Connect())
			recv(t, ready, "connect")
			switch mode {
			case "disabled":
				stop()
			case "server disconnect":
				cl.Emit("request-disconnect")
			case "client close":
				require.NoError(t, cl.Close())
			case "last socket close":
				require.NoError(t, cl.Socket("/").Close())
			}
			recv(t, disconnected, "disconnect")
			select {
			case n := <-attempts:
				t.Fatalf("unexpected retry %d", n)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}

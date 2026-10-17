package client_test

import (
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/stretchr/testify/require"
)

// Covers C8
func TestClientSocketState(t *testing.T) {
	serverReady, serverClosed := make(chan bool, 2), make(chan bool, 2)
	ts := newTestServer(t, func(srv *socketio.Server) {
		for _, ns := range []string{"/", "/chat"} {
			srv.OnConnect(ns, func(c client.Conn) error { serverReady <- c.Connected(); return nil })
			srv.OnDisconnect(ns, func(c client.Conn, _ string) { serverClosed <- c.Connected() })
			srv.OnEvent(ns, "fire", func(c client.Conn, event string) { c.Emit(event, "value"); c.Emit("barrier") })
		}
	})
	cl, err := client.New(ts.URL, retryOptions())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	root, chat := cl.Socket("/"), cl.Socket("/chat")
	ready := make(chan client.Conn, 2)
	closed := make(chan bool, 2)
	for _, s := range []*client.Socket{root, chat} {
		require.False(t, s.Connected())
		require.Empty(t, s.ID())
		s.OnConnect(func(c client.Conn) error { ready <- c; return nil })
		s.OnDisconnect(func(c client.Conn, _ string) { closed <- c.Connected() })
	}
	ping, pong := make(chan struct{}, 16), make(chan time.Duration, 16)
	cl.OnPing(func() { ping <- struct{}{} })
	cl.OnPong(func(rtt time.Duration) { pong <- rtt })
	require.NoError(t, cl.Connect())
	conns := map[string]client.Conn{}
	for i := 0; i < 2; i++ {
		c := recv(t, ready, "namespace connect")
		conns[c.Namespace()] = c
		require.True(t, c.Connected())
		require.True(t, recv(t, serverReady, "server Connected inside OnConnect"))
	}
	require.True(t, root.Connected())
	require.Equal(t, conns["/"].ID(), root.ID())
	require.Equal(t, "/chat#"+root.ID(), chat.ID())
	calls := make(chan string, 16)
	barrier := make(chan struct{}, 8)
	root.OnEvent("barrier", func(client.Conn) { barrier <- struct{}{} })
	root.OnEvent("event", func(client.Conn, string) { calls <- "legacy" })
	off := root.On("event", func(client.Conn, string) { calls <- "on" })
	root.Once("event", func(client.Conn, string) { calls <- "once" })
	fire := func(event string) { root.Emit("fire", event); recv(t, barrier, "listener dispatch barrier") }
	fire("event")
	for _, want := range []string{"legacy", "on", "once"} {
		require.Equal(t, want, recv(t, calls, "listener"))
	}
	fire("event")
	for _, want := range []string{"legacy", "on"} {
		require.Equal(t, want, recv(t, calls, "persistent listener"))
	}
	off()
	off()
	fire("event")
	require.Equal(t, "legacy", recv(t, calls, "remaining legacy handler"))
	require.Empty(t, calls)
	root.Off("event")
	fire("event")
	require.Empty(t, calls)
	removeOnce := root.Once("event", func(client.Conn, string) { calls <- "removed" })
	removeOnce()
	fire("event")
	require.Empty(t, calls)
	// Both callbacks are optional and may be replaced or removed while connected.
	recv(t, ping, "PING observer")
	require.GreaterOrEqual(t, recv(t, pong, "PONG observer"), time.Duration(0))
	cl.OnPing(nil)
	cl.OnPong(nil)
	require.NoError(t, chat.Close())
	require.False(t, chat.Connected())
	require.Empty(t, chat.ID())
	require.False(t, recv(t, closed, "client state inside OnDisconnect"))
	require.False(t, recv(t, serverClosed, "server state inside OnDisconnect"))
	require.True(t, root.Connected())
	require.NoError(t, cl.Close())
	require.False(t, root.Connected())
	require.Empty(t, root.ID())
	require.False(t, recv(t, closed, "root client disconnected"))
	require.False(t, recv(t, serverClosed, "root server disconnected"))
}

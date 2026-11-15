package socketio

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// Sends, room operations, broadcasts, server queries and closes of both roles may be
// called from application goroutines while handlers run and while other goroutines
// make the same calls; run under -race.
func TestConcurrentUseFromApplicationGoroutines(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(WithEngineOptions(engineio.WithLogger(quiet)))
	conns := make(chan Conn, 8)
	srv.OnConnect("/", func(c Conn) error { conns <- c; return nil })
	srv.OnConnect("/chat", func(Conn) error { return nil })
	srv.OnEvent("/", "echo", func(c Conn, i int) int {
		c.Emit("msg", i) // a handler sends while application goroutines do
		c.Join("echoed")
		return i
	})
	srv.OnEvent("/chat", "echo", func(_ Conn, i int) int { return i })
	go func() { _ = srv.Serve() }()
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })

	var clients []*client.Client
	var server []Conn
	var clientConns []client.Conn
	for _, tr := range []transport.Transport{polling.Default, websocket.Default, polling.Default, websocket.Default} {
		cl, err := client.New(ts.URL, client.WithTransports(tr), client.WithReconnection(false), client.WithLogger(quiet))
		require.NoError(t, err)
		t.Cleanup(func() { _ = cl.Close() })
		connected := make(chan client.Conn, 1)
		cl.OnConnect(func(c client.Conn) error { connected <- c; return nil })
		cl.OnEvent("msg", func(client.Conn, int) {})
		cl.Socket("/chat")
		require.NoError(t, cl.Connect())
		clients = append(clients, cl)
		server = append(server, recv(t, conns, "OnConnect"))
		clientConns = append(clientConns, recv(t, connected, "the client's OnConnect"))
	}

	const rounds = 200
	var wg sync.WaitGroup
	run := func(f func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				f(i)
			}
		}()
	}
	nsp := srv.Of("/")
	for _, c := range server {
		run(func(i int) { c.Emit("msg", i) })
		run(func(i int) { c.Emit("msg", i, func(int) {}); c.Send(i) })
		run(func(i int) { _ = c.Volatile().Emit("msg", i) })
		run(func(int) { c.Join("a", "b"); c.Leave("a"); _ = c.Rooms() })
		run(func(int) { c.LeaveAll() })
		run(func(i int) { _ = c.To("b").Emit("msg", i); _ = c.Broadcast().Emit("msg", i) })
		run(func(int) { srv.JoinRoom("/", "c", c); srv.LeaveRoom("/", "c", c); srv.LeaveAllRooms("/", c) })
	}
	run(func(i int) { srv.BroadcastToRoom("/", "b", "msg", i); srv.BroadcastToNamespace("/", "msg", i) })
	run(func(i int) { _ = nsp.To("b", "c").Emit("msg", i); _ = nsp.Except("a").Emit("msg", i) })
	run(func(int) { _ = srv.RoomLen("/", "b"); _ = srv.Rooms("/"); _ = srv.Count(); srv.ClearRoom("/", "c") })
	run(func(int) { srv.ForEach("/", "b", func(s adapter.Socket) { _ = s.ID() }) })
	for _, cl := range clients {
		run(func(i int) { cl.Emit("echo", i, func(int) {}) })
		run(func(i int) { cl.Socket("/chat").Emit("echo", i, func(int) {}) })
		run(func(i int) { cl.Emit("msg", i) })
	}
	for _, c := range clientConns {
		run(func(i int) { c.Emit("echo", i, func(int) {}); c.Volatile().Emit("msg", i); c.Send(i) })
	}
	// Closes race the calls above: a close from another goroutine ends each connection
	// while it is in use, and later calls on it are dropped.
	run(func(i int) {
		if i != rounds/2 {
			return
		}
		server[1].Disconnect()
		clientConns[0].Disconnect()
		for _, err := range []error{server[0].Close(), clients[2].Close(), clients[3].Socket("/chat").Close(), clientConns[1].Close()} {
			if err != nil {
				t.Error(err)
			}
		}
	})
	wg.Wait()

	for _, cl := range clients {
		require.NoError(t, cl.Close())
	}
	require.NoError(t, srv.Close())
	require.Zero(t, srv.Count())
}

package socketio

import (
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

const waitFor = 5 * time.Second

func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(waitFor):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

// TestLifecycleRootNamespace drives one Go client through connect, an event
// with an acknowledgement, room membership and broadcasts, and a
// server-initiated disconnect.
func TestLifecycleRootNamespace(t *testing.T) { lifecycleRootNamespace(t, nil) }

// lifecycleRootNamespace runs the TestLifecycleRootNamespace scenario on NewServer(opts)
// and returns the session's ID.
func lifecycleRootNamespace(t *testing.T, opts *engineio.Options) string {
	srv := NewServer(opts)
	connected := make(chan Conn, 1)
	disconnected := make(chan string, 1)
	srv.OnConnect("/", func(s Conn) error {
		s.Join("lobby")
		connected <- s
		return nil
	})
	srv.OnEvent("/", "echo", func(_ Conn, msg string) string { return "echo:" + msg })
	srv.OnEvent("/", "bye", func(s Conn) { _ = s.Close() })
	srv.OnDisconnect("/", func(_ Conn, reason string) { disconnected <- reason })
	srv.OnError("/", func(Conn, error) {})

	go func() { _ = srv.Serve() }()
	defer func() { _ = srv.Close() }()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	cl, err := NewClient(ts.URL, nil)
	require.NoError(t, err)
	news := make(chan string, 8)
	cl.OnConnect(func(Conn) error { return nil })
	cl.OnEvent("news", func(_ Conn, msg string) { news <- msg })
	require.NoError(t, cl.Connect())
	defer func() { _ = cl.Close() }()

	sc := recv(t, connected, "server OnConnect")
	require.Equal(t, "/", sc.Namespace())
	require.Equal(t, 1, srv.Count())

	// Event with acknowledgement: the handler's return value is the ack.
	acks := make(chan string, 1)
	cl.Emit("echo", "hi", func(reply string) { acks <- reply })
	require.Equal(t, "echo:hi", recv(t, acks, "ack"))

	// Rooms: OnConnect joined "lobby"; every connection is also in its sid room.
	require.Equal(t, 1, srv.RoomLen("/", "lobby"))
	require.Contains(t, srv.Rooms("/"), "lobby")
	require.ElementsMatch(t, []string{"lobby", sc.ID()}, sc.Rooms())

	require.True(t, srv.BroadcastToRoom("/", "lobby", "news", "room"))
	require.Equal(t, "room", recv(t, news, "room broadcast"))

	// Known defect, pinned until roadmap 2.2 fixes it: BroadcastToNamespace
	// sends one copy per room the connection is in (here "lobby" and its sid
	// room), where Socket.IO delivers one copy per socket.
	require.True(t, srv.BroadcastToNamespace("/", "news", "nsp"))
	require.Equal(t, "nsp", recv(t, news, "first namespace broadcast copy"))
	require.Equal(t, "nsp", recv(t, news, "second namespace broadcast copy"))
	select {
	case extra := <-news:
		t.Fatalf("unexpected third copy %q", extra)
	case <-time.After(200 * time.Millisecond):
	}

	seen := 0
	require.True(t, srv.ForEach("/", "lobby", func(c Conn) { seen++ }))
	require.Equal(t, 1, seen)

	require.True(t, srv.LeaveRoom("/", "lobby", sc))
	require.Equal(t, 0, srv.RoomLen("/", "lobby"))
	require.True(t, srv.JoinRoom("/", "lobby", sc))
	require.True(t, srv.ClearRoom("/", "lobby"))
	require.Equal(t, 0, srv.RoomLen("/", "lobby"))
	require.True(t, srv.LeaveAllRooms("/", sc))
	require.Empty(t, sc.Rooms())

	// Unknown namespaces are reported, not created.
	require.False(t, srv.JoinRoom("/missing", "r", sc))
	require.False(t, srv.LeaveRoom("/missing", "r", sc))
	require.False(t, srv.LeaveAllRooms("/missing", sc))
	require.False(t, srv.ClearRoom("/missing", "r"))
	require.False(t, srv.BroadcastToRoom("/missing", "r", "e"))
	require.False(t, srv.BroadcastToNamespace("/missing", "e"))
	require.False(t, srv.ForEach("/missing", "r", func(Conn) {}))
	require.Equal(t, -1, srv.RoomLen("/missing", "r"))
	require.Nil(t, srv.Rooms("/missing"))

	// Server-side close runs OnDisconnect. Known defect, pinned until stage 2.3
	// defines disconnect reasons: the reason reported is clientDisconnectMsg
	// ("client namespace disconnect") although the server closed the connection.
	cl.Emit("bye")
	require.Equal(t, clientDisconnectMsg, recv(t, disconnected, "OnDisconnect"))
	return sc.ID()
}

// rawClient is an engine.io connection that speaks socket.io packets as text.
type rawClient struct {
	t    *testing.T
	conn engineio.Conn
}

func dialRaw(t *testing.T, url string) *rawClient {
	t.Helper()
	d := engineio.Dialer{Transports: []transport.Transport{polling.Default}}
	conn, err := d.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return &rawClient{t: t, conn: conn}
}

func (c *rawClient) send(pkt string) {
	c.t.Helper()
	w, err := c.conn.NextWriter(session.TEXT)
	require.NoError(c.t, err)
	_, err = w.Write([]byte(pkt))
	require.NoError(c.t, err)
	require.NoError(c.t, w.Close())
}

func (c *rawClient) read() string {
	c.t.Helper()
	got := make(chan string, 1)
	go func() {
		_, r, err := c.conn.NextReader()
		if err != nil {
			got <- "error: " + err.Error()
			return
		}
		b, _ := io.ReadAll(r)
		_ = r.Close()
		got <- string(b)
	}()
	return recv(c.t, got, "packet from server")
}

// TestLifecycleNamespace connects a second namespace over the same
// connection, sends an event that asks for an ack, and disconnects the
// namespace.
func TestLifecycleNamespace(t *testing.T) {
	// The engine.io client sends no CLOSE packet, so the server holds its
	// last long poll until pingTimeout; keep that short for teardown.
	srv := NewServer(&engineio.Options{PingTimeout: 500 * time.Millisecond, PingInterval: 200 * time.Millisecond})
	nspConnected := make(chan string, 1)
	nspDisconnected := make(chan string, 1)
	srv.OnConnect("/", func(Conn) error { return nil })
	srv.OnConnect("/chat", func(s Conn) error {
		nspConnected <- s.Namespace()
		return nil
	})
	srv.OnEvent("/chat", "ping", func(_ Conn, n int) int { return n + 1 })
	srv.OnDisconnect("/chat", func(s Conn, _ string) { nspDisconnected <- s.Namespace() })

	go func() { _ = srv.Serve() }()
	defer func() { _ = srv.Close() }()
	ts := httptest.NewServer(srv)
	defer ts.Close()

	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read(), "root namespace CONNECT from the server")

	c.send("0/chat")
	require.Equal(t, "/chat", recv(t, nspConnected, "OnConnect(/chat)"))
	// Known deviation, pinned until the v2 rewrite (stage 2.3): the server's
	// namespace CONNECT reply carries an empty JSON array and a trailing
	// newline ("0/chat,[]\n") instead of the bare "0/chat" of protocol v4.
	require.Equal(t, "0/chat,[]\n", c.read(), "namespace CONNECT acknowledged")

	c.send(`2/chat,7["ping",41]`)
	require.Equal(t, "3/chat,7[42]\n", c.read(), "event acknowledged with the handler's return value")

	c.send("1/chat")
	require.Equal(t, "/chat", recv(t, nspDisconnected, "OnDisconnect(/chat)"))
}

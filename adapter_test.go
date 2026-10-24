package socketio

import (
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/sshaplygin/go-socket.io/adapter"
	redisadapter "github.com/sshaplygin/go-socket.io/adapter/redis"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
	"io"
	"net/http/httptest"
	"testing"
	"time"
)

// redisTestConn is a Conn that records the events emitted to it. Only ID and
// Emit are used by the Redis broadcast; the embedded Conn is nil.
type redisTestConn struct {
	Conn
	id     string
	events chan string
	onEmit func()
}

func newRedisTestConn(id string) *redisTestConn {
	return &redisTestConn{id: id, events: make(chan string, 64)}
}

func (c *redisTestConn) ID() string { return c.id }

func (c *redisTestConn) Emit(event string, v ...interface{}) {
	if c.onEmit != nil {
		c.onEmit()
	}
	c.events <- fmt.Sprintf("%s %v", event, v)
}

func expectEvent(t *testing.T, c *redisTestConn, want string) {
	t.Helper()
	select {
	case got := <-c.events:
		require.Equal(t, want, got)
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: %q not delivered", c.id, want)
	}
}

func (c *redisTestConn) EmitWithOptions(_ adapter.Options, event string, args ...interface{}) {
	c.Emit(event, args...)
}

func newTestRedisBroadcast(t *testing.T, s *miniredis.Miniredis) adapter.Adapter {
	t.Helper()
	const channel = "socket.io-request#/#"
	before := s.PubSubNumSub(channel)[channel]
	bc, err := redisadapter.New(redisadapter.WithAddr(s.Addr()))("/")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, bc.(io.Closer).Close()) })
	require.Eventually(t, func() bool { return s.PubSubNumSub(channel)[channel] == before+1 }, 2*time.Second, 5*time.Millisecond)
	return bc
}

func waitRedisSubscribers(t *testing.T, s *miniredis.Miniredis, channel string, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return s.PubSubNumSub(channel)[channel] == n }, 2*time.Second, 5*time.Millisecond)
}

func TestRoomMethodsRejectForeignAndDisconnectedSockets(t *testing.T) {
	connected := make(chan Conn, 2)
	s, url := defectServer(t, func(s *Server) { s.OnConnect("/chat", func(c Conn) error { connected <- c; return nil }) })
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	chat := recv(t, connected, "chat socket")
	require.False(t, s.JoinRoom("/", "foreign", chat))
	require.False(t, s.LeaveRoom("/", "foreign", chat))
	require.False(t, s.LeaveAllRooms("/", chat))
	require.True(t, s.JoinRoom("/chat", "room", chat))
	chat.Disconnect()
	require.Equal(t, "1/chat,", c.read())
	require.False(t, s.JoinRoom("/chat", "room", chat))
	require.False(t, s.LeaveRoom("/chat", "room", chat))
	require.False(t, s.LeaveAllRooms("/chat", nil))
}

type sharedSessionID struct{}

func (sharedSessionID) NewID() string { return "shared" }

// A live socket of another server whose ID is registered here is not this
// server's socket: room methods compare identity, not the ID alone.
func TestRoomMethodsRejectSocketOfAnotherServerWithSameID(t *testing.T) {
	start := func() (*Server, Conn) {
		connected := make(chan Conn, 1)
		s := NewServer(WithEngineOptions(append(shortPingOptions(), engineio.WithSessionIDGenerator(sharedSessionID{}))...))
		s.OnConnect("/", func(c Conn) error { connected <- c; return nil })
		go func() { _ = s.Serve() }()
		ts := httptest.NewServer(s)
		t.Cleanup(func() { _ = s.Close(); ts.Close() })
		c := dialRaw(t, ts.URL)
		require.Equal(t, "0", c.read())
		return s, recv(t, connected, "root socket")
	}
	first, own := start()
	_, other := start()
	require.Equal(t, own.ID(), other.ID())
	require.False(t, first.JoinRoom("/", "room", other))
	require.False(t, first.LeaveRoom("/", "room", other))
	require.False(t, first.LeaveAllRooms("/", other))
	require.Zero(t, first.RoomLen("/", "room"))
	require.True(t, first.JoinRoom("/", "room", own))
	require.Equal(t, 1, first.RoomLen("/", "room"))
}

func TestNilRoomConnection(t *testing.T) {
	s := NewServer()
	defer func() { _ = s.Close() }()
	s.Of("/")
	require.NotPanics(t, func() { require.False(t, s.JoinRoom("/", "room", nil)) })
}

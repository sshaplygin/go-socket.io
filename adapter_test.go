package socketio

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
	redisadapter "github.com/sshaplygin/go-socket.io/adapter/redis"
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
	const channel = "socket.io-request#/"
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

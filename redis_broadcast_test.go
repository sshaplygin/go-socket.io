package socketio

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

const testRedisReqChannel = "socket.io-request#/"

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

func newTestRedisBroadcast(t *testing.T, s *miniredis.Miniredis) *redisBroadcast {
	t.Helper()
	before := s.PubSubNumSub(testRedisReqChannel)[testRedisReqChannel]
	bc, err := newRedisBroadcast("/", &RedisAdapterOptions{Addr: s.Addr(), Prefix: "socket.io", Network: "tcp"})
	require.NoError(t, err)
	waitRedisSubscribers(t, s, before+1)
	return bc
}

func waitRedisSubscribers(t *testing.T, s *miniredis.Miniredis, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return s.PubSubNumSub(testRedisReqChannel)[testRedisReqChannel] == n
	}, 2*time.Second, 5*time.Millisecond)
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

func TestRedisBroadcastAcrossInstances(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	b := newTestRedisBroadcast(t, s)

	a1, b1, b2 := newRedisTestConn("a1"), newRedisTestConn("b1"), newRedisTestConn("b2")
	a.Join("room", a1)
	b.Join("room", b1)
	b.Join("other", b2)

	a.Send("room", "msg", "hi")
	expectEvent(t, a1, "msg [hi]")
	expectEvent(t, b1, "msg [hi]")

	b.SendAll("all", 1)
	expectEvent(t, a1, "all [1]")
	expectEvent(t, b1, "all [1]")
	expectEvent(t, b2, "all [1]")

	require.Equal(t, 2, a.Len("room"))
	require.Equal(t, 1, a.Len("other"))
	require.ElementsMatch(t, []string{"room", "other"}, a.AllRooms())

	a.Clear("other")
	require.Eventually(t, func() bool { return b.Len("other") == 0 }, 2*time.Second, 10*time.Millisecond)
	require.ElementsMatch(t, []string{"room"}, b.AllRooms())
	require.Empty(t, b2.events)
}

// The publishing connection is shared by every caller of the broadcast;
// redigo allows only one concurrent user per connection.
func TestRedisBroadcastConcurrentPublish(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	b := newTestRedisBroadcast(t, s)
	b1 := newRedisTestConn("b1")
	b.Join("room", b1)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				a.Send("room", "msg")
			}
		}()
	}
	wg.Wait()
	for i := 0; i < 40; i++ {
		expectEvent(t, b1, "msg []")
	}
}

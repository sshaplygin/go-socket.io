package socketio

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRedisReqChannel = "socket.io-request#/"

func init() {
	redisRequestTimeout = 300 * time.Millisecond
	redisReconnectMin = 5 * time.Millisecond
	redisReconnectMax = 50 * time.Millisecond
}

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
	t.Cleanup(bc.close)
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

// Len and AllRooms register their request in bc.requests, which the dispatch
// goroutine reads when a response arrives.
func TestRedisBroadcastConcurrentRequests(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	newTestRedisBroadcast(t, s)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				assert.Equal(t, 0, a.Len("room"))
				assert.Empty(t, a.AllRooms())
			}
		}()
	}
	wg.Wait()
	require.Empty(t, a.requests)
}

// Requests are answered on the dispatch goroutine from the local rooms while
// connections join and leave on others.
func TestRedisBroadcastRequestsReadRoomsUnderLock(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	c := newRedisTestConn("a1")

	stop := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		for {
			select {
			case <-stop:
				return
			default:
				a.Join("room", c)
				a.Leave("room", c)
				// Pause so that the requests progress with GOMAXPROCS=1.
				time.Sleep(time.Microsecond)
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			a.Len("room")
			a.Rooms(nil)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Len and Rooms did not return")
	}
	close(stop)
	<-joined
}

// A connection may leave its rooms while a message is emitted to it, as a
// connection that closes itself does; leaving takes the write lock.
func TestRedisBroadcastEmitWithoutLock(t *testing.T) {
	cases := map[string]func(a, b *redisBroadcast){
		"Send":           func(a, _ *redisBroadcast) { a.Send("room", "msg") },
		"SendAll":        func(a, _ *redisBroadcast) { a.SendAll("msg") },
		"ForEach":        func(a, _ *redisBroadcast) { a.ForEach("room", func(c Conn) { c.Emit("msg") }) },
		"remote Send":    func(_, b *redisBroadcast) { b.Send("room", "msg") },
		"remote SendAll": func(_, b *redisBroadcast) { b.SendAll("msg") },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			s := miniredis.RunT(t)
			a := newTestRedisBroadcast(t, s)
			b := newTestRedisBroadcast(t, s)
			c := newRedisTestConn("a1")
			c.onEmit = func() { a.LeaveAll(c) }
			a.Join("room", c)

			go run(a, b)
			expectEvent(t, c, "msg []")
			require.Empty(t, a.Rooms(c))
		})
	}
}

// PUBSUB NUMSUB also counts a subscriber of the request channel that never
// answers, such as an instance that hangs.
func TestRedisBroadcastRequestTimeout(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	silent, err := redis.Dial("tcp", s.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = silent.Close() })
	require.NoError(t, redis.PubSubConn{Conn: silent}.Subscribe(testRedisReqChannel))
	waitRedisSubscribers(t, s, 2)
	a.Join("room", newRedisTestConn("a1"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		assert.Equal(t, 1, a.Len("room"))
		assert.Equal(t, []string{"room"}, a.AllRooms())
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Len and AllRooms did not return")
	}
	require.Empty(t, a.requests)
}

// The server accepts the first connection and refuses the AUTH of the second,
// so construction fails after one connection is open.
func TestRedisBroadcastConstructionFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	firstClosed := make(chan struct{})
	go func() {
		for first := true; ; first = false {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(first bool) {
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				for i := 0; i < 5; i++ { // *2, $4, AUTH, $2, pw
					if _, err := r.ReadString('\n'); err != nil {
						return
					}
				}
				if !first {
					_, _ = c.Write([]byte("-ERR refused\r\n"))
					return
				}
				_, _ = c.Write([]byte("+OK\r\n"))
				_, _ = io.Copy(io.Discard, r)
				close(firstClosed)
			}(first)
		}
	}()

	opts := &RedisAdapterOptions{Addr: ln.Addr().String(), Network: "tcp", Prefix: "socket.io", Password: "pw"}
	_, err = newRedisBroadcast("/", opts)
	require.Error(t, err)
	select {
	case <-firstClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("the first connection was not closed")
	}

	require.NoError(t, ln.Close())
	_, err = newRedisBroadcast("/", opts)
	require.Error(t, err)
}

func TestRedisBroadcastSkipsMalformedMessages(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	b := newTestRedisBroadcast(t, s)
	a1 := newRedisTestConn("a1")
	a.Join("room", a1)

	for _, msg := range []string{"not json", "{}", `{"opts":[1,"msg"]}`} {
		s.Publish("socket.io#/#peer", msg)
	}
	s.Publish("socket.io-response#/", "{}")
	s.Publish("socket.io-response#/", `{"RequestID":1}`)

	b.Send("room", "msg")
	expectEvent(t, a1, "msg []")
}

func TestRedisBroadcastResubscribesAfterRestart(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	b := newTestRedisBroadcast(t, s)
	a1 := newRedisTestConn("a1")
	a.Join("room", a1)

	// Keep the server down across several reconnect attempts.
	s.Close()
	time.Sleep(3 * redisReconnectMax)
	require.NoError(t, s.Restart())
	waitRedisSubscribers(t, s, 2)

	// The first publish on a pooled connection opened before the restart
	// fails; later ones dial again.
	require.Eventually(t, func() bool {
		b.Send("room", "msg")
		select {
		case <-a1.events:
			return true
		case <-time.After(50 * time.Millisecond):
			return false
		}
	}, 2*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return b.Len("room") == 1 }, 2*time.Second, time.Millisecond)

	// close stops the dispatcher instead of making it subscribe again.
	a.close()
	waitRedisSubscribers(t, s, 1)
	time.Sleep(4 * redisReconnectMax)
	require.Equal(t, 1, s.PubSubNumSub(testRedisReqChannel)[testRedisReqChannel])
}

// A server that accepts the connection but refuses the subscription, as on
// NOAUTH or an ACL error, fails every reconnect after a successful dial.
func TestRedisBroadcastReconnectBackoff(t *testing.T) {
	s := miniredis.RunT(t)
	newTestRedisBroadcast(t, s)
	s.RequireAuth("pw")
	s.Close()
	require.NoError(t, s.Restart())

	// Doubling from 5 ms up to 50 ms allows about 8 attempts in 300 ms.
	time.Sleep(300 * time.Millisecond)
	require.LessOrEqual(t, s.TotalConnectionCount(), 12)
}

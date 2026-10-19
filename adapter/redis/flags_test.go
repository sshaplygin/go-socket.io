package redis

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
)

// flagTarget records whether an adapter used the public single-socket Emitter.
type flagTarget struct {
	*redisTestConn
	writable bool
	volatile int
}

func (c *flagTarget) EmitWithOptions(opts adapter.Options, event string, args ...interface{}) {
	if opts.Volatile {
		c.volatile++
		if !c.writable {
			return
		}
	}
	c.Emit(event, args...)
}

func flagSocket(id string, writable bool) *flagTarget {
	return &flagTarget{redisTestConn: newRedisTestConn(id), writable: writable}
}
func isolatedRedis(t *testing.T) (*broadcast, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	pool := &redis.Pool{Dial: func() (redis.Conn, error) { calls.Add(1); return nil, errors.New("unexpected redis I/O") }}
	t.Cleanup(func() { _ = pool.Close() })
	return &broadcast{nsp: "/", uid: "local", key: "socket.io#/#local", Memory: adapter.NewMemory(), pub: pool}, &calls
}

// Covers A4
func TestRedisBroadcastExceptSupported(t *testing.T) {
	b, calls := isolatedRedis(t)
	c := newRedisTestConn("c")
	b.Join("r", c)
	require.NoError(t, b.SendTo(adapter.Options{Except: []string{"c"}, Local: true}, "nothing"))
	require.Empty(t, c.events)
	require.Zero(t, calls.Load())
	require.NoError(t, b.SendTo(adapter.Options{Except: []string{"other"}, Local: true}, "selected"))
	expectEvent(t, c, "selected []")
}

func TestRedisBroadcastDropsUnknownOpts(t *testing.T) {
	b, calls := isolatedRedis(t)
	c := newRedisTestConn("c")
	b.Join("r", c)
	require.Error(t, b.onMessage("socket.io#/#remote", []byte(`{"opts":["r","news",{"except":["c"]}],"args":[]}`)))
	require.Empty(t, c.events)
	require.Zero(t, calls.Load())
	data, err := encodeBroadcast("remote", "/", "news", adapter.Options{Rooms: []string{"r"}}, nil)
	require.NoError(t, err)
	require.NoError(t, b.onMessage("socket.io#/#r#", data))
	expectEvent(t, c, "news []")
}

func TestRedisBroadcastDropsUnknownKeys(t *testing.T) {
	b, _ := isolatedRedis(t)
	c := newRedisTestConn("c")
	b.Join("r", c)
	require.Error(t, b.onMessage("socket.io#/#remote", []byte(`{"opts":["r","news"],"args":[],"except":["c"]}`)))
	require.Empty(t, c.events)
}

func TestRedisBroadcastLocalFlag(t *testing.T) {
	b, calls := isolatedRedis(t)
	c := newRedisTestConn("c")
	b.Join("r", c)
	b.Join("s", c)
	require.NoError(t, b.SendTo(adapter.Options{Rooms: []string{"r", "s"}, Local: true}, "local"))
	expectEvent(t, c, "local []")
	require.Empty(t, c.events)
	require.Zero(t, calls.Load())
	require.Equal(t, []string{"c"}, b.Memory.IDs("r", "s"))
}

func TestRedisBroadcastVolatileFlag(t *testing.T) {
	b, calls := isolatedRedis(t)
	a, c := flagSocket("a", true), flagSocket("c", false)
	b.Join("r", a)
	b.Join("r", c)
	require.Error(t, b.SendTo(adapter.Options{Volatile: true}, "volatile"), "publication errors are visible after local delivery")
	expectEvent(t, a.redisTestConn, "volatile []")
	require.Empty(t, c.events)
	require.Equal(t, 1, a.volatile)
	require.Equal(t, 1, c.volatile)
	require.Equal(t, int32(1), calls.Load())
}

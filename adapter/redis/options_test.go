package redis_test

import (
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/sshaplygin/go-socket.io/adapter"
	redisadapter "github.com/sshaplygin/go-socket.io/adapter/redis"
	"github.com/stretchr/testify/require"
)

type socket struct {
	id     string
	events chan string
}

func (s *socket) ID() string                          { return s.id }
func (s *socket) Emit(event string, _ ...interface{}) { s.events <- event }
func (s *socket) EmitWithOptions(_ adapter.Options, event string, args ...interface{}) {
	s.Emit(event, args...)
}

func TestFactoryOptionsAndIndependentNamespaces(t *testing.T) {
	redis := miniredis.RunT(t)
	redis.RequireAuth("secret")
	var selects atomic.Int32
	redis.Server().SetPreHook(func(_ *server.Peer, command string, _ ...string) bool {
		if command == "SELECT" {
			selects.Add(1)
		}
		return false
	})
	address := redisadapter.WithAddr(redis.Addr())
	factory := redisadapter.New(nil,
		redisadapter.WithAddr("unreachable"), address,
		redisadapter.WithNetwork("invalid"), redisadapter.WithNetwork("tcp"),
		redisadapter.WithPrefix("discarded"), redisadapter.WithPrefix("cluster"),
		redisadapter.WithPassword("wrong"), redisadapter.WithPassword("secret"),
		redisadapter.WithDB(3))
	require.Zero(t, redis.TotalConnectionCount(), "factory construction must not probe Redis")
	makeAdapter := func(namespace string) adapter.Adapter {
		t.Helper()
		a, err := factory(namespace)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, a.(io.Closer).Close()) })
		return a
	}
	first, remote, isolated := makeAdapter("/one"), makeAdapter("/one"), makeAdapter("/two")
	require.Eventually(t, func() bool {
		return redis.PubSubNumSub("cluster-request#/one#")["cluster-request#/one#"] == 1 && redis.PubSubNumSub("cluster-request#/two#")["cluster-request#/two#"] == 1
	}, time.Second, 5*time.Millisecond)
	require.GreaterOrEqual(t, selects.Load(), int32(2), "subscriber and publishing connection select configured database")
	localSocket := &socket{id: "local", events: make(chan string, 4)}
	remoteSocket := &socket{id: "remote", events: make(chan string, 4)}
	isolatedSocket := &socket{id: "isolated", events: make(chan string, 4)}
	first.Join("room", localSocket)
	remote.Join("room", remoteSocket)
	isolated.Join("room", isolatedSocket)
	first.Send("room", "news")
	for _, target := range []*socket{localSocket, remoteSocket} {
		select {
		case event := <-target.events:
			require.Equal(t, "news", event)
		case <-time.After(time.Second):
			t.Fatal("namespace delivery timed out")
		}
	}
	require.Equal(t, 2, first.Len("room"))
	require.Empty(t, localSocket.events, "own UID must prevent duplicate delivery")
	// A request/reply through the isolated subscriber is a barrier after the publish.
	require.Equal(t, 1, isolated.Len("room"))
	require.Empty(t, isolatedSocket.events, "namespace state and channels must stay independent")
	first.LeaveAll(localSocket)
	require.Equal(t, []string{"remote"}, first.IDs())
	require.Equal(t, []string{"remote"}, remote.IDs())
	require.Equal(t, []string{"isolated"}, isolated.IDs())
	// Reusing a targeted option cannot retain another factory's prefix or password.
	plain := miniredis.RunT(t)
	independent, err := redisadapter.New(address, redisadapter.WithAddr(plain.Addr()), redisadapter.WithPrefix(""), redisadapter.WithNetwork(""), redisadapter.WithPassword(""))("/defaults")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, independent.(io.Closer).Close()) })
	require.Eventually(t, func() bool {
		return plain.PubSubNumSub("socket.io-request#/defaults#")["socket.io-request#/defaults#"] == 1
	}, time.Second, 5*time.Millisecond)
}

func TestFactoryDefersConstructionErrors(t *testing.T) {
	factory := redisadapter.New(redisadapter.WithNetwork("invalid-network"))
	instance, err := factory("/failed")
	require.Error(t, err)
	require.Nil(t, instance)
}

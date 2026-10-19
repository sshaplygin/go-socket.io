package redis

import (
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
	"time"
)

// Covers G14
func TestFactorySharesRedisConnectionsAcrossNamespaces(t *testing.T) {
	s := miniredis.RunT(t)
	factory := New(WithAddr(s.Addr()))
	var namespaces []io.Closer
	for i := 0; i < 30; i++ {
		a, err := factory(fmt.Sprintf("/room-%d", i))
		require.NoError(t, err)
		namespaces = append(namespaces, a.(io.Closer))
	}
	t.Cleanup(func() {
		for _, n := range namespaces {
			_ = n.Close()
		}
	})
	// TCP dial can return before miniredis accepts the publishing connection.
	// Wait for both shared connections before checking that closing one namespace
	// retains them; the exact count also detects per-namespace allocation.
	require.Eventually(t, func() bool { return s.CurrentConnectionCount() == 2 }, time.Second, time.Millisecond,
		"namespace count must not allocate a connection pair per namespace")
	require.NoError(t, namespaces[0].Close())
	require.Equal(t, 2, s.CurrentConnectionCount(), "other namespaces still own the shared connections")
	for _, n := range namespaces {
		require.NoError(t, n.Close())
	}
	require.Eventually(t, func() bool { return s.CurrentConnectionCount() == 0 }, time.Second, time.Millisecond)
}

func TestSharedFactoryPreservesNamespaceAndRoomDelivery(t *testing.T) {
	s := miniredis.RunT(t)
	factories := []func(string) (adapter.Adapter, error){New(WithAddr(s.Addr())), New(WithAddr(s.Addr()))}
	var all []*broadcast
	for _, factory := range factories {
		for _, namespace := range []string{"/room#/room", "/room", "/room[*]"} {
			a, err := factory(namespace)
			require.NoError(t, err)
			bc := a.(*broadcast)
			all = append(all, bc)
			t.Cleanup(func() { _ = bc.Close() })
			require.Eventually(t, func() bool { return s.PubSubNumSub(bc.reqChannel)[bc.reqChannel] > 0 }, time.Second, time.Millisecond)
		}
	}
	for _, bc := range all {
		require.Eventually(t, func() bool { return s.PubSubNumSub(bc.reqChannel)[bc.reqChannel] == 2 }, time.Second, time.Millisecond)
	}
	targets := make([]*redisTestConn, len(all))
	for i, bc := range all {
		targets[i] = newRedisTestConn(fmt.Sprint(i))
		bc.Join("", targets[i])
		bc.Join("ordinary", targets[i])
	}
	all[0].SendAll("hash")
	expectEvent(t, targets[0], "hash []")
	expectEvent(t, targets[3], "hash []")
	require.Equal(t, 2, all[0].Len("ordinary")) // request/reply barrier
	for _, i := range []int{1, 2, 4, 5} {
		require.Empty(t, targets[i].events)
	}
	all[2].Send("", "empty")
	expectEvent(t, targets[2], "empty []")
	expectEvent(t, targets[5], "empty []")
	require.NoError(t, all[0].Close())
	require.Equal(t, 2, all[1].Len("ordinary"), "closing the initial namespace must preserve the shared dispatcher")
}

func TestSharedFactoryReconnectsRemainingNamespaces(t *testing.T) {
	s := miniredis.RunT(t)
	factory := New(WithAddr(s.Addr()))
	first, err := factory("/closed")
	require.NoError(t, err)
	active, err := factory("/active")
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.(io.Closer).Close(); _ = active.(io.Closer).Close() })
	c := newRedisTestConn("active")
	active.Join("room", c)
	require.Eventually(t, func() bool { return s.PubSubNumSub("socket.io-request#/active#")["socket.io-request#/active#"] == 1 }, time.Second, time.Millisecond)
	require.NoError(t, first.(io.Closer).Close())
	s.Close()
	require.NoError(t, s.Restart())
	require.Eventually(t, func() bool { return s.PubSubNumSub("socket.io-request#/active#")["socket.io-request#/active#"] == 1 }, 2*time.Second, time.Millisecond)
	require.Equal(t, 0, s.PubSubNumSub("socket.io-request#/closed#")["socket.io-request#/closed#"])
	require.Eventually(t, func() bool { return active.Len("room") == 1 }, 2*time.Second, time.Millisecond)
	require.Equal(t, []string{"room"}, active.AllRooms())
}

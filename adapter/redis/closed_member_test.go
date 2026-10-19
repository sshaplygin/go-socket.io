package redis

import (
	"github.com/alicebob/miniredis/v2"
	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
	"time"
)

func TestClosedSharedMemberCannotDeliverOrPublish(t *testing.T) {
	server := miniredis.RunT(t)
	factory := New(WithAddr(server.Addr()))
	closed, err := factory("/closed")
	require.NoError(t, err)
	active, err := factory("/active")
	require.NoError(t, err)
	peer, err := New(WithAddr(server.Addr()))("/closed")
	require.NoError(t, err)
	defer func() { _ = active.(io.Closer).Close() }()
	defer func() { _ = peer.(io.Closer).Close() }()
	local, remote := newRedisTestConn("local"), newRedisTestConn("remote")
	closed.Join("room", local)
	peer.Join("room", remote)
	require.Eventually(t, func() bool {
		return server.PubSubNumSub("socket.io-request#/closed#")["socket.io-request#/closed#"] == 2
	}, time.Second, time.Millisecond)
	require.NoError(t, closed.(io.Closer).Close())
	require.Error(t, closed.SendTo(adapter.Options{}, "forbidden"))
	// The remote room count acts as a subscriber processing barrier.
	require.Equal(t, 1, peer.Len("room"))
	require.Empty(t, local.events)
	require.Empty(t, remote.events)
	closed.Clear("room")
	require.Equal(t, 1, peer.Len("room"))
	require.Equal(t, -1, closed.Len("room"))
	require.Empty(t, closed.AllRooms())
	// The closed initial member still owns the transport on behalf of /active.
	target := newRedisTestConn("active")
	active.Join("room", target)
	require.NoError(t, active.SendTo(adapter.Options{Local: true}, "alive"))
	expectEvent(t, target, "alive []")
}

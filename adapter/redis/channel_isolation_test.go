package redis

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/stretchr/testify/require"
)

// Covers G13
func TestReviewG13NamespaceChannelIsolation(t *testing.T) {
	s := miniredis.RunT(t)
	b := newTestRedisBroadcast(t, s)
	c := newRedisTestConn("victim")
	b.Join("room", c)
	data, err := encodeBroadcast("peer", "/#/", "injected", adapter.Options{}, nil)
	require.NoError(t, err)
	require.NoError(t, b.onMessage("socket.io#/#/#", data))
	require.Empty(t, c.events, "a /#/ channel must not deliver to namespace /")
}

package redis

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

// Covers A5
// Covers S13
func TestRedisClusterIDsIncludesRemoteRooms(t *testing.T) {
	s := miniredis.RunT(t)
	a, b := newTestRedisBroadcast(t, s), newTestRedisBroadcast(t, s)
	a.Join("one", newRedisTestConn("a"))
	b.Join("two", newRedisTestConn("b"))
	b.Join("one", newRedisTestConn("c"))
	require.ElementsMatch(t, []string{"a", "b", "c"}, a.IDs("one", "two"))
}

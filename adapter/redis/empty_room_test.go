package redis

import (
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestEmptyRoomDoesNotBroadcastNamespace(t *testing.T) {
	s := miniredis.RunT(t)
	a, b := newTestRedisBroadcast(t, s), newTestRedisBroadcast(t, s)
	c := newRedisTestConn("not-in-empty-room")
	b.Join("other", c)
	a.Send("", "leaked")
	a.SendAll("barrier")
	select {
	case event := <-c.events:
		require.Equal(t, "barrier []", event, "empty room must not reach sockets in other rooms")
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}
}

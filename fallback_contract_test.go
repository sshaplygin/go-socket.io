package socketio

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnavailableAdapterRemainsEmpty(t *testing.T) {
	handler := newNamespaceHandler("/unavailable", &RedisAdapterOptions{Network: "invalid-socket-network", Addr: "unreachable"})
	require.Error(t, handler.err)
	bc := handler.broadcast
	c := newRedisTestConn("unavailable-socket")
	bc.Join("room", c)
	bc.Send("room", "event", "payload")
	bc.SendAll("event", "payload")
	require.NoError(t, bc.SendTo(BroadcastOptions{Rooms: []string{"room"}, Except: []string{"other"}, Volatile: true, NoCompress: true}, "event", "payload"))
	called := false
	bc.ForEach("room", func(Conn) { called = true })
	require.False(t, called, "unavailable adapters cannot visit recipients")
	require.Empty(t, c.events, "unavailable adapters must not deliver packets")
	require.Empty(t, bc.Rooms(c))
	require.Empty(t, bc.AllRooms())
	require.Empty(t, bc.IDs())
	require.Empty(t, bc.IDs("room"))
	require.Equal(t, -1, bc.Len("room"), "unavailable is distinct from an empty available room")
	bc.Leave("room", c)
	bc.LeaveAll(c)
	bc.Clear("room")
	require.Empty(t, bc.Rooms(c))
	require.Empty(t, bc.AllRooms())
	require.Empty(t, c.events)
}

func TestNamespaceErrorMessageKeepsDiagnosticContext(t *testing.T) {
	cause := errors.New("adapter unavailable")
	var err error = newErrorMessage("/chat", cause)
	require.EqualError(t, err, "error in namespace: (/chat) with error: (adapter unavailable)")
}

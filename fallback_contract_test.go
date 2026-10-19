package socketio

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
)

func TestUnavailableAdapterRemainsEmpty(t *testing.T) {
	unavailable := errors.New("adapter unavailable")
	srv := NewServer(WithAdapter(func(string) (adapter.Adapter, error) { return nil, unavailable }))
	defer func() { _ = srv.Close() }()
	handler := srv.createNamespace("/unavailable")
	require.ErrorIs(t, handler.Err(), unavailable)
	bc := handler.Adapter()
	c := newRedisTestConn("unavailable-socket")
	bc.Join("room", c)
	bc.Send("room", "event", "payload")
	bc.SendAll("event", "payload")
	require.ErrorIs(t, bc.SendTo(adapter.Options{Rooms: []string{"room"}, Except: []string{"other"}, Volatile: true, NoCompress: true}, "event", "payload"), unavailable)
	called := false
	bc.ForEach("room", func(adapter.Socket) { called = true })
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

func TestUnavailableNamespaceEmitterReportsConstructionError(t *testing.T) {
	unavailable := errors.New("adapter unavailable")
	srv := NewServer(WithAdapter(func(string) (adapter.Adapter, error) { return nil, unavailable }))
	defer func() { _ = srv.Close() }()
	ns := srv.Of("/unavailable")
	require.Nil(t, ns.Adapter(), "a failed adapter is not exposed to consumers")
	for _, target := range []Emitter{ns.To("room"), ns.Except("other"), ns.Local().Volatile().Compress(false)} {
		require.ErrorIs(t, target.Emit("event", "payload"), unavailable)
		require.ErrorIs(t, target.Send("payload"), unavailable)
		require.ErrorIs(t, target.Emit("event", func() {}), ErrBroadcastAck)
		require.ErrorIs(t, target.Send(func() {}), ErrBroadcastAck)
	}
	require.ErrorIs(t, srv.Serve(), unavailable)
}

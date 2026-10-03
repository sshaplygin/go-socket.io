package socketio

import (
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

// useRedis sets the Adapter of srv to s and closes srv at cleanup.
func useRedis(t *testing.T, srv *Server, addr string) {
	t.Helper()
	ok, err := srv.Adapter(&RedisAdapterOptions{Addr: addr})
	require.True(t, ok)
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })
}

// serveAsync runs Serve in a goroutine and returns its result channel.
func serveAsync(srv *Server) <-chan error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve() }()
	return errc
}

// TestRedisConstructionErrorReturnedByServe checks that Serve returns the first recorded
// construction error and that a later registration does not rebuild the broadcast.
//
// Covers 1I-T7 (S).
func TestRedisConstructionErrorReturnedByServe(t *testing.T) {
	s := miniredis.RunT(t)
	srv := NewServer(nil)
	useRedis(t, srv, s.Addr())
	s.Close()
	srv.OnConnect("/first", func(Conn) error { return nil })
	srv.OnConnect("/second", func(Conn) error { return nil })
	require.NoError(t, s.Restart())
	srv.OnEvent("/first", "e", func(Conn) {})

	select {
	case err := <-serveAsync(srv):
		require.ErrorContains(t, err, `"/first"`)
		require.NotContains(t, err.Error(), "/second")
		var opErr *net.OpError
		require.ErrorAs(t, err, &opErr)
	case <-time.After(time.Second):
		t.Fatal("Serve did not return the construction error")
	}
	require.Equal(t, -1, srv.RoomLen("/first", "r"))
}

// TestRedisFailedNamespaceRoomMethods checks the Server room methods of a namespace whose
// Redis broadcast could not be created.
//
// Covers 1I-T9 (S).
func TestRedisFailedNamespaceRoomMethods(t *testing.T) {
	s := miniredis.RunT(t)
	srv := NewServer(nil)
	useRedis(t, srv, s.Addr())
	s.Close()
	srv.OnConnect("/f", func(Conn) error { return nil })

	c := newRedisTestConn("c")
	require.False(t, srv.JoinRoom("/f", "r", c), "JoinRoom")
	require.False(t, srv.LeaveRoom("/f", "r", c), "LeaveRoom")
	require.False(t, srv.LeaveAllRooms("/f", c), "LeaveAllRooms")
	require.False(t, srv.ClearRoom("/f", "r"), "ClearRoom")
	require.False(t, srv.BroadcastToRoom("/f", "r", "e"), "BroadcastToRoom")
	require.False(t, srv.BroadcastToNamespace("/f", "e"), "BroadcastToNamespace")
	require.Equal(t, -1, srv.RoomLen("/f", "r"))
	require.Nil(t, srv.Rooms("/f"))
	require.False(t, srv.ForEach("/f", "r", func(Conn) { t.Error("ForEach called its function") }))
	require.Empty(t, c.events)
}

package socketio

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConnLeaveAll checks that Conn.LeaveAll on a connected socket leaves every
// room, its own ID room included, as socket.io 2.5.0 leaveAll does, and that a
// room broadcast no longer reaches it.
func TestConnLeaveAll(t *testing.T) {
	type rooms struct{ before, after []string }
	left := make(chan rooms, 1)
	var srv *Server
	ts := newTestServer(t, func(s *Server) {
		srv = s
		s.OnConnect("/", func(c Conn) error {
			c.Join("a")
			c.Join("b")
			return nil
		})
		s.OnEvent("/", "leave", func(c Conn) string {
			before := c.Rooms()
			c.LeaveAll()
			left <- rooms{before, c.Rooms()}
			return c.ID()
		})
		s.OnEvent("/", "alive", func(Conn) string { return "alive" })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send(`21["leave"]`)
	got := recv(t, left, "leave event")
	reply := c.read()
	require.Regexp(t, `^31\[".+"\]\n$`, reply)
	id := reply[len(`31["`) : len(reply)-len("\"]\n")]
	require.ElementsMatch(t, []string{id, "a", "b"}, got.before)
	require.Empty(t, got.after)
	for _, room := range []string{"a", "b", id} {
		require.Zero(t, srv.RoomLen("/", room), "room %q", room)
	}
	require.True(t, srv.BroadcastToRoom("/", "a", "room"))
	c.send(`22["alive"]`)
	require.Equal(t, "32[\"alive\"]\n", c.read(), "a room broadcast reached the socket after LeaveAll")
}

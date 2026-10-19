package socketio

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Covers G3
func TestRegressionG3OldDisconnectPreservesNewRooms(t *testing.T) {
	connected := make(chan Conn, 2)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	s, url := defectServer(t, func(s *Server) {
		s.OnEvent("/", "alive", func(Conn) string { return "alive" })
		s.OnConnect("/chat", func(c Conn) error { connected <- c; return nil })
		s.Of("/chat").On("disconnecting", func(Conn, string) { entered <- struct{}{}; <-release })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	old := recv(t, connected, "old namespace")
	done := make(chan struct{})
	go func() { old.Disconnect(); close(done) }()
	recv(t, entered, "old disconnecting callback")
	require.Equal(t, "1/chat,", c.read())
	c.send("0/chat,")
	c.send(`27["alive"]`)
	require.Equal(t, "37[\"alive\"]\n", c.read(), "retiring namespace does not stall the reader")
	select {
	case <-connected:
		t.Error("replacement accepted before old cleanup")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	recv(t, done, "old disconnect completed")
	require.Equal(t, "0/chat,", c.read())
	next := recv(t, connected, "new namespace")
	require.Equal(t, 1, s.RoomLen("/chat", next.ID()), "old LeaveAll removed newly accepted socket's ID room")
}

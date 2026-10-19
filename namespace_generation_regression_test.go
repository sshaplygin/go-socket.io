package socketio

import (
	"sync"
	"testing"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/stretchr/testify/require"
)

func TestLateAckCannotCrossNamespaceReconnect(t *testing.T) {
	acks := make(chan AckFunc, 2)
	_, url := defectServer(t, func(s *Server) {
		s.OnConnect("/chat", func(Conn) error { return nil })
		s.OnEvent("/chat", "ask", func(_ Conn, ack AckFunc) { acks <- ack })
		s.OnEvent("/chat", "barrier", func(c Conn) { c.Emit("barrier") })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	c.send(`2/chat,0["ask"]`)
	oldAck := recv(t, acks, "old request")
	c.send("1/chat,")
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	c.send(`2/chat,0["ask"]`)
	_ = recv(t, acks, "replacement request")
	oldAck("stale result")
	c.send(`2/chat,["barrier"]`)
	require.Equal(t, "2/chat,[\"barrier\"]\n", c.read(), "old ACK must not answer replacement request ID 0")
}

type joinDisconnectAdapter struct{ *adapter.Memory }

func (a *joinDisconnectAdapter) Join(room string, c adapter.Socket) {
	if room == "trigger" {
		c.(Conn).Disconnect()
	}
	a.Memory.Join(room, c)
}
func TestServerJoinRoomCannotResurrectDisconnectedSocket(t *testing.T) {
	b := &joinDisconnectAdapter{adapter.NewMemory()}
	connected := make(chan Conn, 1)
	disconnected := make(chan struct{}, 1)
	s, url := defectServer(t, func(s *Server) {
		s.SetAdapter(func(string) (adapter.Adapter, error) { return b, nil })
		s.OnConnect("/chat", func(c Conn) error { connected <- c; return nil })
		s.OnDisconnect("/chat", func(Conn, string) { disconnected <- struct{}{} })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	nc := recv(t, connected, "connected socket")
	require.True(t, s.JoinRoom("/chat", "trigger", nc))
	require.Equal(t, "1/chat,", c.read())
	recv(t, disconnected, "retired socket")
	require.False(t, nc.Connected())
	require.Empty(t, b.AllRooms(), "JoinRoom adapter reentry must not recreate rooms after disconnect cleanup")
}

type blockedLeaveAdapter struct {
	*adapter.Memory
	entered, release chan struct{}
	old              Conn
}

func (a *blockedLeaveAdapter) Leave(room string, c adapter.Socket) {
	if room == "live" && c == a.old {
		close(a.entered)
		<-a.release
	}
	a.Memory.Leave(room, c)
}
func TestServerLeaveRoomDoesNotRemoveReplacementRooms(t *testing.T) {
	b := &blockedLeaveAdapter{Memory: adapter.NewMemory(), entered: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(b.release) })
	defer release()
	connected := make(chan Conn, 2)
	s, url := defectServer(t, func(s *Server) {
		s.SetAdapter(func(string) (adapter.Adapter, error) { return b, nil })
		s.OnEvent("/", "barrier", func(Conn) string { return "ready" })
		s.OnConnect("/chat", func(c Conn) error { connected <- c; return nil })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	old := recv(t, connected, "old socket")
	b.old = old
	old.Join("live")
	done := make(chan bool, 1)
	go func() { done <- s.LeaveRoom("/chat", "live", old) }()
	recv(t, b.entered, "LeaveRoom adapter entry")
	old.Disconnect()
	c.send("0/chat,")
	c.send(`27["barrier"]`)
	require.Equal(t, "37[\"ready\"]\n", c.read(), "replacement must wait until the old operation finishes")
	release()
	require.True(t, recv(t, done, "old LeaveRoom return"))
	require.Equal(t, "1/chat,", c.read())
	require.Equal(t, "0/chat,", c.read())
	fresh := recv(t, connected, "replacement socket")
	fresh.Join("live")

	require.Equal(t, 1, s.RoomLen("/chat", "live"), "old helper removed replacement's room")
}

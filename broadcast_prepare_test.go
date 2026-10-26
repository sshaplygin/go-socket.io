package socketio

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
)

type countedBroadcastValue struct{ calls *atomic.Int32 }

func (v countedBroadcastValue) MarshalJSON() ([]byte, error) {
	v.calls.Add(1)
	return []byte(`"payload"`), nil
}

func TestBroadcastMarshalsPayloadOnce(t *testing.T) {
	srv, addr := defectServer(t, func(s *Server) {
		s.OnConnect("/", func(c Conn) error { c.Join("r"); return nil })
	})
	var peers []*rawClient
	for i := 0; i < 10; i++ {
		c := dialRaw(t, addr)
		require.Equal(t, "0", c.read())
		peers = append(peers, c)
	}
	var calls atomic.Int32
	require.True(t, srv.BroadcastToRoom("/", "r", "news", countedBroadcastValue{&calls}))
	for _, c := range peers {
		require.Equal(t, "2[\"news\",\"payload\"]\n", c.read())
	}
	require.Equal(t, int32(1), calls.Load())
}

// A standalone Memory can hold sockets of several namespaces; a shared packet
// must still carry each recipient's own namespace header.
func TestBroadcastPreparedAcrossNamespaces(t *testing.T) {
	connected := make(chan Conn, 2)
	_, addr := defectServer(t, func(s *Server) {
		s.OnConnect("/", func(c Conn) error { connected <- c; return nil })
		s.OnConnect("/chat", func(c Conn) error { connected <- c; return nil })
	})
	m := adapter.NewMemory()
	root := dialRaw(t, addr)
	require.Equal(t, "0", root.read())
	m.Join("g", recv(t, connected, "root socket").(adapter.Socket))
	chat := dialRaw(t, addr)
	require.Equal(t, "0", chat.read())
	<-connected // chat client's root socket stays out of the room
	chat.send("0/chat,")
	require.Equal(t, "0/chat,", chat.read())
	m.Join("g", recv(t, connected, "chat socket").(adapter.Socket))

	require.NoError(t, m.SendTo(adapter.Options{Rooms: []string{"g"}}, "news", "x"))
	require.Equal(t, "2[\"news\",\"x\"]\n", root.read())
	require.Equal(t, "2/chat,[\"news\",\"x\"]\n", chat.read())
}

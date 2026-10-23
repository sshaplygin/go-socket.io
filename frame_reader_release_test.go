package socketio

import (
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestMalformedPacketReleasesFrameReader(t *testing.T) {
	disconnected := make(chan struct{}, 1)
	srv, addr := defectServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnError("/", func(Conn, error) {})
		s.OnDisconnect("/", func(Conn, string) { disconnected <- struct{}{} })
	})
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(addr, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	for i := 0; i < 2; i++ {
		_, _, err := c.ReadMessage()
		require.NoError(t, err)
	}
	require.NoError(t, c.WriteMessage(websocket.TextMessage, []byte("49")))
	recv(t, disconnected, "disconnect after malformed packet")
	require.NoError(t, srv.Close())
	require.NoError(t, c.Close())
	require.Eventually(t, func() bool { return len(leakedModuleStacks()) == 0 }, time.Second, 10*time.Millisecond,
		"frame reader or another library worker remains after close")
}

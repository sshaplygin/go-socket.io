package socketio

import (
	"github.com/sshaplygin/go-socket.io/engineio"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestIdleWebsocketConnectionGoroutines(t *testing.T) {
	srv := NewServer(WithEngineOptions(engineio.WithPingInterval(time.Hour), engineio.WithPingTimeout(time.Hour)))
	srv.OnConnect("/", func(Conn) error { return nil })
	go func() { _ = srv.Serve() }()
	httpServer := httptest.NewServer(srv)
	t.Cleanup(func() { _ = srv.Close(); httpServer.Close() })
	addr := httpServer.URL
	baseline := runtime.NumGoroutine()
	const clients = 50
	for i := 0; i < clients; i++ {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(addr, "http")+"/?EIO=3&transport=websocket", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		for j := 0; j < 2; j++ {
			_, _, err := c.ReadMessage()
			require.NoError(t, err)
		}
	}
	require.Equal(t, clients, srv.Count(), "every measured connection must still be live")
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine()-baseline <= clients*5+5
	}, time.Second, 10*time.Millisecond, "more than five workers per idle connection")
}

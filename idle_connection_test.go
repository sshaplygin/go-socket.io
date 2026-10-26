package socketio

import (
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
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
	// Five per connection: four library workers and the net/http connection goroutine.
	for deadline := time.Now().Add(time.Second); ; time.Sleep(10 * time.Millisecond) {
		added := runtime.NumGoroutine() - baseline
		if added <= clients*5+5 {
			t.Logf("%d idle connections added %d goroutines", clients, added)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d idle connections added %d goroutines (%.2f each), want at most 5 each",
				clients, added, float64(added)/clients)
		}
	}
}

package socketio

import (
	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Covers G2
func TestRegressionG2FailedRootRemovesEngineSession(t *testing.T) {
	s := NewServer(WithEngineOptions(shortPingOptions()...), WithWriteBufferSize(1))
	s.OnConnect("/", func(c Conn) error { c.Emit("one"); c.Emit("two"); return nil })
	go func() { _ = s.Serve() }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	defer ws.Close()
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(time.Second)))
	for err == nil {
		_, _, err = ws.ReadMessage()
	}
	require.Eventually(t, func() bool { return s.Engine().Count() == 0 }, 300*time.Millisecond, 5*time.Millisecond, "closed failed-root session remains in engine registry")
}

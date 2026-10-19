package socketio

import (
	"testing"
	"github.com/stretchr/testify/require"
)

// Covers: G19
func TestRegressionG19PeerCannotDispatchLifecycle(t *testing.T) {
	seen := make(chan string, 4)
	_, url := defectServer(t, func(s *Server) {
		s.Of("/").On("disconnecting", func(_ Conn, reason string) { seen <- reason })
		s.OnEvent("/", "barrier", func(c Conn) { c.Emit("barrier") })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send(`2["disconnecting","forged"]`)
	c.send(`2["barrier"]`)
	require.Equal(t, "2[\"barrier\"]\n", c.read())
	select {
	case reason := <-seen:
		t.Errorf("peer dispatched lifecycle while still connected: %q", reason)
	default:
	}
}


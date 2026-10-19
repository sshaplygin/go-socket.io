package socketio

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// Covers G21
func TestRegressionG21NoUnrequestedAck(t *testing.T) {
	_, url := defectServer(t, func(s *Server) {
		s.OnEvent("/", "value", func(Conn) string { return "answer" })
		s.OnEvent("/", "barrier", func(c Conn) { c.Emit("barrier") })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send(`2["value"]`)
	c.send(`2["barrier"]`)
	require.Equal(t, "2[\"barrier\"]\n", c.read(), "event without id must not generate ACK")
}

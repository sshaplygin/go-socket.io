package socketio

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// Covers G28
func TestRegressionG28ReservedEmitStaysLocal(t *testing.T) {
	_, url := defectServer(t, func(s *Server) {
		s.OnEvent("/", "trigger", func(c Conn) { c.Emit("disconnect", "forged"); c.Emit("barrier") })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send(`2["trigger"]`)
	require.Equal(t, "2[\"barrier\"]\n", c.read())
}

package socketio

import (
	"testing"
	"github.com/stretchr/testify/require"
)

// Covers: G6
func TestRegressionG6EmitSnapshotsMap(t *testing.T) {
	_, c, fc, _ := connWithOptions(t, 'S', nil)
	fc.hold.Store(true)
	c.Emit("barrier")
	recv(t, fc.held, "writer blocked on previous packet")
	value := map[string]string{"value": "before"}
	c.Emit("snapshot", value)
	value["value"] = "after"
	fc.hold.Store(false)
	close(fc.release)
	recv(t, fc.out, "barrier packet")
	require.Contains(t, recv(t, fc.out, "snapshot packet"), `"value":"before"`)
}


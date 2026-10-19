package connection

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Covers G6
func TestClientEmitSnapshotsQueuedAndBufferedValues(t *testing.T) {
	t.Run("connected", func(t *testing.T) {
		p := start(t, 'C', hooks{})
		p.fc.hold.Store(true)
		p.nc.Emit("blocker")
		recv(t, p.fc.held, "blocked writer")
		value := map[string]string{"value": "before"}
		p.nc.Emit("snapshot", value)
		value["value"] = "after"
		close(p.fc.release)
		require.Equal(t, ev("blocker"), recv(t, p.fc.out, "blocker"))
		require.Equal(t, ev("snapshot", map[string]string{"value": "before"}), recv(t, p.fc.out, "snapshot"))
	})
	t.Run("before connect", func(t *testing.T) {
		p := newPeer(t, 'C', hooks{})
		value := map[string]string{"value": "before"}
		p.cl.Socket("/").Emit("snapshot", value)
		value["value"] = "after"
		p.connect(t)
		require.Equal(t, ev("snapshot", map[string]string{"value": "before"}), recv(t, p.fc.out, "buffered snapshot"))
	})
}

type clientReentrantJSON struct {
	check    func()
	timedOut *bool
}

func (v clientReentrantJSON) MarshalJSON() ([]byte, error) {
	done := make(chan struct{})
	go func() { v.check(); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		*v.timedOut = true
	}
	return json.Marshal("snapshot")
}
func TestClientSnapshotMarshalerRunsOutsideManagerLocks(t *testing.T) {
	p := start(t, 'C', hooks{})
	timedOut := false
	p.nc.Emit("snapshot", clientReentrantJSON{check: func() { p.cl.Socket("/").Connected() }, timedOut: &timedOut})
	require.Equal(t, ev("snapshot", "snapshot"), recv(t, p.fc.out, "custom JSON"))
	require.False(t, timedOut, "custom JSON must run before manager locks")
}

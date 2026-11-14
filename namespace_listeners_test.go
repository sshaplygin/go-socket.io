package socketio

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnceSurvivesEarlierListenerFailure(t *testing.T) {
	h := newNamespaceHandler()
	attempts, called := 0, 0
	h.OnEvent("event", func(Conn) {
		attempts++
		if attempts == 1 {
			panic("stop dispatch")
		}
	})
	h.AddListener("event", func(Conn) { called++ }, true)
	_, err := h.DispatchEvent(&testConn{}, "event")
	require.Error(t, err)
	require.Zero(t, called)
	_, err = h.DispatchEvent(&testConn{}, "event")
	require.NoError(t, err)
	require.Equal(t, 1, called, "a Once listener is consumed only when it is invoked")
}

// TestOnceListenerClaimedAcrossSnapshots checks that a Once listener runs once when
// two connections dispatch the same event concurrently. Each packet handler takes
// its listener snapshot before dispatching, so both snapshots can hold the listener;
// the dispatch that runs second must skip it.
func TestOnceListenerClaimedAcrossSnapshots(t *testing.T) {
	h := newNamespaceHandler()
	called, every := 0, 0
	h.AddListener("event", func(Conn) { every++ }, false)
	h.AddListener("event", func(Conn) { called++ }, true)
	first, second := h.Listeners("event"), h.Listeners("event")
	require.Len(t, second, 2)
	for _, snapshot := range [][]*eventListener{first, second} {
		_, _, err := h.DispatchListeners(&testConn{}, "event", snapshot, func(...interface{}) {})
		require.NoError(t, err)
	}
	require.Equal(t, 2, every)
	require.Equal(t, 1, called, "a Once listener ran for two events")
	require.Len(t, h.Listeners("event"), 1, "only the persistent listener remains registered")
}

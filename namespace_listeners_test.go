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

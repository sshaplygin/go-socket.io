package socketio

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCallbacksCanReregister(t *testing.T) {
	h := newNamespaceHandler()
	h.OnError(func(Conn, error) { h.OnError(nil) })
	h.OnDisconnect(func(Conn, string) { h.OnDisconnect(nil) })
	finished := make(chan struct{})
	go func() {
		h.ErrorHandler()(nil, errors.New("reported"))
		h.NotifyDisconnect(nil, "closed")
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("callbacks ran under registration locks")
	}
	require.Nil(t, h.ErrorHandler())
	h.eventsLock.RLock()
	disconnected := h.onDisconnect
	h.eventsLock.RUnlock()
	require.Nil(t, disconnected)
}

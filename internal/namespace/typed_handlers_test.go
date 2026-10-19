package namespace

import (
	"context"
	eventImpl "github.com/sshaplygin/go-socket.io/internal/event"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
)

func testTypedHandler() eventImpl.Handler {
	return eventImpl.NewHandler(eventImpl.SchemaOf[int](), false, func(context.Context, eventImpl.Endpoint, eventImpl.Payload) (eventImpl.Payload, error) {
		return eventImpl.Payload{}, nil
	})
}

// Covers T1
func TestTypedRegistrationIsAtomicAndRespectsLegacyReplacement(t *testing.T) {
	h := NewHandler()
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if h.RegisterEvent("event", testTypedHandler()) == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), successes.Load())
	require.ErrorIs(t, h.RegisterEvent("event", testTypedHandler()), eventImpl.ErrDuplicateEvent)
	require.Panics(t, func() { h.AddListener("event", func(Conn, int) {}, false) })
	child := NewHandler()
	h.CopyRegistrations(child)
	_, ok := child.Listeners("event")[0].TypedHandler()
	require.True(t, ok)
	h.OnEvent("event", func(Conn, int) {})
	_, ok = h.Listeners("event")[0].TypedHandler()
	require.False(t, ok)
	require.ErrorIs(t, h.RegisterEvent("event", testTypedHandler()), eventImpl.ErrDuplicateEvent)
	h.Off("event")
	require.NoError(t, h.RegisterEvent("event", testTypedHandler()))
	for _, name := range []string{"", "connect", "disconnect", "disconnecting", "error"} {
		require.ErrorIs(t, h.RegisterEvent(name, testTypedHandler()), eventImpl.ErrInvalidEventName)
	}
	require.ErrorIs(t, h.RegisterEvent("invalid", eventImpl.Handler{}), eventImpl.ErrInvalidPayload)
}

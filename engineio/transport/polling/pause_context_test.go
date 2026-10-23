package polling

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPollingPauseCancellationBeforeStart(t *testing.T) {
	c := &clientConn{payload: newPayload(false), started: true, polling: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Resume()
	pauser, ok := any(c).(interface{ PauseContext(context.Context) error })
	require.True(t, ok, "polling must accept cancellation before Pause starts")
	done := make(chan error, 1)
	go func() { done <- pauser.PauseContext(ctx) }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("canceled pause blocked")
	}
	require.False(t, c.pausing)
}

func TestPollingPauseCancellationWhileWaiting(t *testing.T) {
	c := &clientConn{payload: newPayload(false), started: true, polling: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	pauser, ok := any(c).(interface{ PauseContext(context.Context) error })
	require.True(t, ok, "polling must support cancellable pauses")
	go func() { done <- pauser.PauseContext(ctx) }()
	require.Eventually(t, func() bool { c.writableMu.Lock(); defer c.writableMu.Unlock(); return c.pausing }, time.Second, time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("pause ignored cancellation")
	}
	require.True(t, c.beginPoll(), "rollback permits new polling work")
}

package socketio

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
)

// blockingCloser is a namespace adapter whose Close reports err, after release
// is closed when it is non-nil.
type blockingCloser struct {
	*adapter.Memory
	entered chan struct{}
	release chan struct{}
	err     error
}

func (a *blockingCloser) Close() error {
	close(a.entered)
	if a.release != nil {
		<-a.release
	}
	return a.err
}

// A Close that gives up waiting for an OnDisconnect handler must say so, and must
// still report the engine and adapter errors it had already collected.
func TestServerCloseTimeoutReportsPendingConnections(t *testing.T) {
	failure := errors.New("adapter close failed")
	entered, release := make(chan struct{}), make(chan struct{})
	probe := &blockingCloser{Memory: adapter.NewMemory(), entered: make(chan struct{}), err: failure}
	s := NewServer(
		WithEngineOptions(shortPingOptions()...),
		WithAdapter(func(string) (adapter.Adapter, error) { return probe, nil }),
	)
	s.OnConnect("/", func(Conn) error { return nil })
	s.OnDisconnect("/", func(Conn, string) { close(entered); <-release })
	go func() { _ = s.Serve() }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { close(release); _ = s.Close(); ts.Close() })

	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())

	err := s.closeWithTimeout(50 * time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded, "timeout is reported")
	require.ErrorContains(t, err, "1 connections still open")
	recv(t, entered, "blocked OnDisconnect")
	recv(t, probe.entered, "adapter Close")
	recv(t, s.closeDone, "engine and adapter shutdown")

	err = s.closeWithTimeout(50 * time.Millisecond)
	require.ErrorIs(t, err, context.DeadlineExceeded, "timeout is still reported")
	require.ErrorIs(t, err, failure, "adapter close error accompanies the timeout")
}

// TestServerCloseWaitsTenSeconds checks the bound of Server.Close: it waits ten
// seconds for an OnDisconnect handler that does not return. The wait's timer fires
// at once here, so the test observes the configured bound without waiting for it.
func TestServerCloseWaitsTenSeconds(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s := NewServer(WithEngineOptions(shortPingOptions()...))
	waits := make(chan time.Duration, 4)
	var expired atomic.Bool
	s.closeTimer = func(d time.Duration) *time.Timer {
		waits <- d
		if expired.Swap(true) {
			return time.NewTimer(d)
		}
		return time.NewTimer(0)
	}
	s.OnConnect("/", func(Conn) error { return nil })
	s.OnDisconnect("/", func(Conn, string) { close(entered); <-release })
	go func() { _ = s.Serve() }()
	ts := httptest.NewServer(s)
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(func() { unblock(); _ = s.Close(); ts.Close() })

	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())

	err := s.Close()
	require.Equal(t, 10*time.Second, recv(t, waits, "Close wait"))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "close timed out after 10s with 1 connections still open")
	recv(t, entered, "blocked OnDisconnect")
	unblock()
	require.NoError(t, s.Close(), "Close after OnDisconnect returned")
}

// An adapter whose Close outlives the wait is a timeout as well, even with no
// connection left to wait for.
func TestServerCloseTimeoutReportsBlockedAdapter(t *testing.T) {
	probe := &blockingCloser{Memory: adapter.NewMemory(), entered: make(chan struct{}), release: make(chan struct{})}
	s := NewServer(WithAdapter(func(string) (adapter.Adapter, error) { return probe, nil }))
	s.Of("/")
	t.Cleanup(func() { close(probe.release); _ = s.Close() })

	err := s.closeWithTimeout(50 * time.Millisecond)
	recv(t, probe.entered, "adapter Close")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "unfinished")
}

// A shutdown that completes returns the adapter error and no timeout.
func TestServerCloseCompletedReportsAdapterErrorOnly(t *testing.T) {
	failure := errors.New("adapter close failed")
	probe := &blockingCloser{Memory: adapter.NewMemory(), entered: make(chan struct{}), err: failure}
	s := NewServer(WithAdapter(func(string) (adapter.Adapter, error) { return probe, nil }))
	s.Of("/")
	err := s.closeWithTimeout(time.Minute)
	require.ErrorIs(t, err, failure)
	require.NotErrorIs(t, err, context.DeadlineExceeded)
}

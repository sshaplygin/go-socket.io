package socketio

import (
	"errors"
	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
	"time"
)

type closingAdapter struct {
	*adapter.Memory
	closed atomic.Int32
	err    error
}

func (a *closingAdapter) Close() error { a.closed.Add(1); return a.err }

func TestWithAdapterFactoryLifecycle(t *testing.T) {
	failure := errors.New("adapter close failed")
	first := &closingAdapter{Memory: adapter.NewMemory(), err: failure}
	var names []string
	selected := func(name string) (adapter.Adapter, error) { names = append(names, name); return first, nil }
	ignored := func(string) (adapter.Adapter, error) { t.Error("superseded option called"); return nil, nil }
	option := WithAdapter(selected)
	srv := NewServer(WithAdapter(ignored), option)
	require.Empty(t, names, "options do not construct namespace adapters")
	require.Same(t, first, srv.Of("/first").Adapter())
	require.Same(t, first, srv.Of("/first").Adapter())
	require.Equal(t, []string{"/first"}, names)
	srv.SetAdapter(nil)
	memory := srv.Of("/memory").Adapter()
	require.IsType(t, &adapter.Memory{}, memory)
	require.NotSame(t, first, memory)
	require.Same(t, first, srv.Of("/first").Adapter())
	require.ErrorIs(t, srv.Close(), failure)
	require.ErrorIs(t, srv.Close(), failure)
	require.Equal(t, int32(1), first.closed.Load())
	// A reused option does not share the namespace registry with another server.
	other := NewServer(option, WithAdapter(nil))
	defer func() { _ = other.Close() }()
	require.IsType(t, &adapter.Memory{}, other.Of("/first").Adapter())
	require.Equal(t, []string{"/first"}, names)
}

func TestAdapterNilFactoryError(t *testing.T) {
	var calls int
	srv := NewServer(WithAdapter(func(string) (adapter.Adapter, error) { calls++; return nil, nil }))
	defer func() { _ = srv.Close() }()
	require.Nil(t, srv.Of("/failed").Adapter())
	require.ErrorContains(t, srv.Serve(), `broadcast of namespace "/failed": adapter returned nil`)
	require.Nil(t, srv.Of("/failed").Adapter())
	require.Equal(t, 1, calls)
}

// reentrantCloser bounds a blocked callback so a regression fails without
// leaving the server's shutdown goroutine permanently holding its locks.
type reentrantCloser struct {
	*adapter.Memory
	callback func()
	done     chan struct{}
}

func (a *reentrantCloser) Close() error {
	go func() { a.callback(); close(a.done) }()
	select {
	case <-a.done:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("adapter close callback blocked by server locks")
	}
}

func TestAdapterCloseCanReenterServer(t *testing.T) {
	for _, operation := range []string{"existing namespace", "adapter setter"} {
		t.Run(operation, func(t *testing.T) {
			instance := &reentrantCloser{Memory: adapter.NewMemory(), done: make(chan struct{})}
			srv := NewServer(WithAdapter(func(string) (adapter.Adapter, error) { return instance, nil }))
			existing := srv.Of("/existing")
			instance.callback = func() {
				if operation == "adapter setter" {
					srv.SetAdapter(nil)
				}
				if got := srv.Of("/existing"); got != existing {
					t.Error("existing namespace identity changed during shutdown")
				}
			}
			t.Cleanup(func() { recv(t, instance.done, "adapter close callback to finish") })
			require.NoError(t, srv.Close())
			recv(t, srv.closeDone, "server shutdown to complete")
			require.Same(t, existing, srv.Of("/existing"))
		})
	}
}

func TestSlowFactoryDoesNotBlockOtherNamespace(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s := NewServer(WithAdapter(func(nsp string) (adapter.Adapter, error) {
		if nsp == "/slow" {
			close(entered)
			<-release
		}
		return adapter.NewMemory(), nil
	}))
	defer func() { _ = s.Close() }()
	slow, fast := make(chan struct{}), make(chan struct{})
	go func() { s.Of("/slow"); close(slow) }()
	recv(t, entered, "slow factory started")
	go func() { s.Of("/fast"); close(fast) }()
	select {
	case <-fast:
	case <-time.After(100 * time.Millisecond):
		t.Error("unrelated namespace creation waits for slow factory")
	}
	close(release)
	recv(t, slow, "slow factory finished")
	recv(t, fast, "fast factory finished")
}

type factoryCloseAdapter struct {
	*adapter.Memory
	closed chan struct{}
}

func (a *factoryCloseAdapter) Close() error { close(a.closed); return nil }

func TestFactoryCreationSharedAndClosedAfterCompletion(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	built := &factoryCloseAdapter{Memory: adapter.NewMemory(), closed: make(chan struct{})}
	var calls atomic.Int32
	s := NewServer(WithAdapter(func(name string) (adapter.Adapter, error) { calls.Add(1); close(entered); <-release; return built, nil }))
	first, second := make(chan *ServerNamespace, 1), make(chan *ServerNamespace, 1)
	go func() { first <- s.Of("/slow") }()
	recv(t, entered, "factory started")
	go func() { second <- s.Of("/slow") }()
	configured := make(chan struct{})
	go func() { s.SetAdapter(nil); close(configured) }()
	recv(t, configured, "SetAdapter while another factory runs")
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	require.Eventually(t, func() bool { return s.closed.Load() }, time.Second, time.Millisecond)
	s.Of("/after-close")
	close(release)
	require.Same(t, recv(t, first, "first namespace view"), recv(t, second, "second namespace view"))
	require.NoError(t, recv(t, closed, "Close after factory completion"))
	recv(t, built.closed, "built adapter closed")
	require.Equal(t, int32(1), calls.Load(), "one factory call for one name")
}

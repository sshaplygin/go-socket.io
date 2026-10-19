package socketio

import (
	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
	"time"
)

// Covers G5
func TestRegressionG5SlowFactoryDoesNotBlockOtherNamespace(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	s := NewServer(WithAdapter(func(nsp string) (adapter.Adapter, error) {
		if nsp == "/slow" {
			close(entered)
			<-release
		}
		return adapter.NewMemory(), nil
	}))
	defer s.Close()
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

// Covers G5
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

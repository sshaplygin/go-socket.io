package socketio

import (
	"testing"
	"time"
	"github.com/sshaplygin/go-socket.io/adapter"
)

// Covers: G5
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


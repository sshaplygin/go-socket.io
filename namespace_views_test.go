package socketio

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
)

func TestConcurrentNamespaceViews(t *testing.T) {
	var constructed atomic.Int32
	server := NewServer(WithAdapter(func(string) (adapter.Adapter, error) { constructed.Add(1); return adapter.NewMemory(), nil }))
	defer func() { _ = server.Close() }()
	views := make(chan *ServerNamespace, 32)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; views <- server.Of("chat") }()
	}
	close(start)
	wg.Wait()
	close(views)
	expected := server.Of("/chat")
	for view := range views {
		require.Same(t, expected, view)
	}
	require.Equal(t, int32(1), constructed.Load())
}

package redis

import (
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func TestRedisPublishPoolReusesConnections(t *testing.T) {
	s := miniredis.RunT(t)
	// Measure pool retention independently of publication deadline expiry on
	// race-instrumented or heavily loaded hosts. Deadline behavior has its own test.
	bc, err := newBroadcast("/", &options{Addr: s.Addr(), Prefix: "socket.io", Network: "tcp", RequestsTimeout: time.Minute})
	require.NoError(t, err)
	t.Cleanup(func() { _ = bc.Close() })
	before := s.TotalConnectionCount()
	var workers sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for j := 0; j < 100; j++ {
				bc.SendAll("e", "x")
			}
		}()
	}
	close(start)
	workers.Wait()
	opened := s.TotalConnectionCount() - before
	t.Logf("publishing connections opened: %d", opened)
	require.LessOrEqual(t, opened, 20)
}

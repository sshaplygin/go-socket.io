package redis

import (
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
)

// The bound holds only while concurrent publishers do not outnumber
// redisPublishMaxIdle: the pool closes every connection above that number on
// return, so more publishers keep redialing.
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
	for i := 0; i < redisPublishMaxIdle; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for j := 0; j < 1000; j++ {
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

// Publishers that outnumber the retained connections dial their own instead
// of waiting for one to be returned, so WithRequestsTimeout still bounds each
// publication. A pool that queued callers on a bounded set of connections
// would make the last of them wait for several earlier timeouts.
func TestRedisPublishersBeyondPoolBoundedByRequestsTimeout(t *testing.T) {
	const timeout = 250 * time.Millisecond
	s := miniredis.RunT(t)
	addr, stall := stallProxy(t, s.Addr())
	bc, err := newBroadcast("/", &options{
		Addr: addr, Network: "tcp", Prefix: "socket.io", RequestsTimeout: timeout,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = bc.Close() })
	require.NoError(t, bc.SendTo(adapter.Options{}, "e", "before"))
	stall()
	const publishers = 4 * redisPublishMaxIdle
	elapsed := make(chan time.Duration, publishers)
	errs := make(chan error, publishers)
	start := make(chan struct{})
	for i := 0; i < publishers; i++ {
		go func() {
			<-start
			began := time.Now()
			err := bc.SendTo(adapter.Options{}, "e", "stalled")
			elapsed <- time.Since(began)
			errs <- err
		}()
	}
	close(start)
	guard := time.After(5 * time.Second)
	var slowest time.Duration
	for i := 0; i < publishers; i++ {
		select {
		case d := <-elapsed:
			require.Error(t, <-errs)
			slowest = max(slowest, d)
		case <-guard:
			t.Fatalf("%d of %d stalled publications did not return", publishers-i, publishers)
		}
	}
	t.Logf("slowest stalled publication: %v", slowest)
	// WithRequestsTimeout documents up to about three times the timeout per publication.
	require.Less(t, slowest, 3*timeout)
}

package redis

import (
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/sshaplygin/go-socket.io/adapter"
)

// benchRedisSendTo measures publishing one broadcast through Redis. With
// SOCKETIO_BENCH_METRICS=1 it also reports new Redis connections per
// operation as conns/op.
func benchRedisSendTo(b *testing.B, parallel bool) {
	s := miniredis.RunT(b)
	// Keep the test fixture's short request timeout out of the pool-throughput
	// measurement. Deadline behavior has separate regression tests.
	bc := newTestRedisBroadcast(b, s, func(o *options) { o.RequestsTimeout = time.Minute })
	before := s.TotalConnectionCount()
	b.ReportAllocs()
	b.ResetTimer()
	if parallel {
		b.SetParallelism(16)
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if err := bc.SendTo(adapter.Options{}, "e", "x"); err != nil {
					b.Error(err)
					return
				}
			}
		})
	} else {
		for i := 0; i < b.N; i++ {
			if err := bc.SendTo(adapter.Options{}, "e", "x"); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.StopTimer()
	if os.Getenv("SOCKETIO_BENCH_METRICS") == "1" {
		b.ReportMetric(float64(s.TotalConnectionCount()-before)/float64(b.N), "conns/op")
	}
}

// BenchmarkRedisSendTo publishes broadcasts sequentially; see benchRedisSendTo
// for SOCKETIO_BENCH_METRICS.
func BenchmarkRedisSendTo(b *testing.B) { benchRedisSendTo(b, false) }

// BenchmarkRedisSendToParallel publishes broadcasts from 16*GOMAXPROCS
// goroutines; see benchRedisSendTo for SOCKETIO_BENCH_METRICS.
func BenchmarkRedisSendToParallel(b *testing.B) { benchRedisSendTo(b, true) }

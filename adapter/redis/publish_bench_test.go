package redis

import (
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/sshaplygin/go-socket.io/adapter"
)

func benchRedisSendTo(b *testing.B, parallel bool) {
	s := miniredis.RunT(b)
	// Keep the test fixture's short request timeout out of the pool-throughput
	// measurement. Deadline behavior has separate regression tests.
	bc, err := newBroadcast("/", &options{Addr: s.Addr(), Prefix: "socket.io", Network: "tcp", RequestsTimeout: time.Minute})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = bc.Close() })
	waitRedisSubscribers(b, s, testRedisReqChannel, 1)
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
func BenchmarkRedisSendTo(b *testing.B)         { benchRedisSendTo(b, false) }
func BenchmarkRedisSendToParallel(b *testing.B) { benchRedisSendTo(b, true) }

package redis

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
)

// stallProxy keeps established TCP connections open while withholding traffic.
// Cleanup releases the stall before adapter cleanup so a failing baseline run
// also lets its blocked publisher and subscriber finish.
func stallProxy(t *testing.T, addr string) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var stalled atomic.Bool
	resume := make(chan struct{})
	var release sync.Once
	var workers sync.WaitGroup
	var mu sync.Mutex
	var connections []net.Conn
	forward := func(dst, src net.Conn) {
		defer workers.Done()
		buffer := make([]byte, 32*1024)
		for {
			n, readErr := src.Read(buffer)
			if n > 0 {
				if stalled.Load() {
					<-resume
				}
				if _, err := dst.Write(buffer[:n]); err != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			server, err := net.DialTimeout("tcp", addr, time.Second)
			if err != nil {
				_ = client.Close()
				continue
			}
			mu.Lock()
			connections = append(connections, client, server)
			mu.Unlock()
			workers.Add(2)
			go forward(client, server)
			go forward(server, client)
		}
	}()
	t.Cleanup(func() {
		release.Do(func() { close(resume) })
		_ = listener.Close()
		<-accepted
		mu.Lock()
		for _, conn := range connections {
			_ = conn.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	return listener.Addr().String(), func() {
		stalled.Store(true)
		t.Cleanup(func() { release.Do(func() { close(resume) }) })
	}
}

func TestRedisPublishBoundedByRequestsTimeout(t *testing.T) {
	s := miniredis.RunT(t)
	addr, stall := stallProxy(t, s.Addr())
	bc, err := newBroadcast("/", &options{
		Addr: addr, Network: "tcp", Prefix: "socket.io", RequestsTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = bc.Close() })
	require.NoError(t, bc.SendTo(adapter.Options{}, "e", "before"))
	stall()
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- bc.SendTo(adapter.Options{}, "e", "stalled") }()
	select {
	case err := <-done:
		var ne net.Error
		require.ErrorAs(t, err, &ne)
		require.True(t, ne.Timeout(), "publication error is not a timeout: %v", err)
		require.Less(t, time.Since(started), time.Second)
	case <-time.After(2 * time.Second):
		t.Fatal("Redis publication did not honor WithRequestsTimeout")
	}
}

// A publication that redials the pool must also bound the AUTH reply: the
// stalled first publication breaks its pooled connection, so the second one
// dials a new connection whose AUTH reply never arrives.
func TestRedisPublishRedialBoundedByRequestsTimeout(t *testing.T) {
	s := miniredis.RunT(t)
	s.RequireAuth("p")
	addr, stall := stallProxy(t, s.Addr())
	bc, err := newBroadcast("/", &options{
		Addr: addr, Network: "tcp", Prefix: "socket.io", Password: "p",
		RequestsTimeout: 200 * time.Millisecond,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = bc.Close() })
	require.NoError(t, bc.SendTo(adapter.Options{}, "e", "before"))
	stall()
	for i := 0; i < 2; i++ {
		done := make(chan error, 1)
		started := time.Now()
		go func() { done <- bc.SendTo(adapter.Options{}, "e", "stalled") }()
		select {
		case err := <-done:
			// The redial's read deadline and its dial context expire together,
			// so the error is either an I/O timeout or a closed connection.
			require.Error(t, err, "publication %d", i)
			require.Less(t, time.Since(started), time.Second, "publication %d", i)
		case <-time.After(2 * time.Second):
			t.Fatalf("Redis publication %d did not honor WithRequestsTimeout", i)
		}
	}
}

func TestRedisSubscriberIdleLongerThanRequestsTimeout(t *testing.T) {
	s := miniredis.RunT(t)
	newAdapter := func() *broadcast {
		bc, err := newBroadcast("/", &options{
			Addr: s.Addr(), Network: "tcp", Prefix: "socket.io", RequestsTimeout: 50 * time.Millisecond,
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = bc.Close() })
		return bc
	}
	a, b := newAdapter(), newAdapter()
	socket := newRedisTestConn("receiver")
	b.Join("room", socket)
	// Waiting longer than the publication deadline exercises the independent
	// lifetime of the subscriber's blocking read. A read timeout on the
	// subscriber would end that read and make it reconnect, which delivery
	// alone does not reveal, so no new connection may appear while idle.
	dialed := s.TotalConnectionCount()
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, dialed, s.TotalConnectionCount(), "subscriber reconnected while idle")
	require.NoError(t, a.SendTo(adapter.Options{Rooms: []string{"room"}}, "e", "after idle"))
	expectEvent(t, socket, "e [after idle]")
}

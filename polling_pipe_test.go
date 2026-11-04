package socketio

import (
	"context"
	"net"
	"runtime"
	"sync"
	"testing"
)

// pipeListener runs the real HTTP stack over a synchronous transport. A poll
// response cannot complete until the test client consumes every byte.
type pipeListener struct {
	addr  net.Addr
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func (l *pipeListener) Addr() net.Addr { return l.addr }
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-ctx.Done():
		_ = client.Close()
		_ = server.Close()
		return nil, ctx.Err()
	case <-l.done:
		_ = client.Close()
		_ = server.Close()
		return nil, net.ErrClosed
	}
}

// runStalledTransports exercises the HTTP deadline/lifecycle contract with a
// synchronous transport on every OS, and separately with real TCP backpressure
// where the small-buffer fixture has reproduced the stalled response.
func runStalledTransports(t *testing.T, run func(*testing.T, bool)) {
	t.Helper()
	t.Run("pipe", func(t *testing.T) { run(t, true) })
	t.Run("tcp", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows buffers the 1 MB response despite 4 KiB socket buffers; the pipe case verifies the HTTP deadline and lifecycle contract")
		}
		run(t, false)
	})
}

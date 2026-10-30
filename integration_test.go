package socketio

import (
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

// connWithOptions returns the connection that a Server (S) or a Client (C) built with opts
// creates over a fakeConn.
func connWithOptions(t *testing.T, side byte, opts []Option) (*Server, Conn, *fakeConn, <-chan error) {
	t.Helper()
	fc := newFakeConn(t)
	srv := NewServer(opts...)
	conns := make(chan Conn, 1)
	errs := make(chan error, 4)
	srv.OnConnect("/", func(c Conn) error { conns <- c; return nil })
	srv.OnError("/", func(_ Conn, err error) { errs <- err })
	srv.serveConn(fc)
	nc := recv(t, conns, "root OnConnect")
	require.Equal(t, "0", recv(t, fc.out, "root CONNECT"))
	closeAtEnd(t, fc, serverCompletion(srv, fc.ID()))
	return srv, nc, fc, errs
}

// TestOptionsReachConnections checks resolved options and the real Server queue budget.
// Owner-local tests retain the private queue/deadline storage assertions.
func TestOptionsReachConnections(t *testing.T) {
	cases := []struct {
		name  string
		opts  []Option
		size  int
		drain time.Duration
	}{
		{"nil options", nil, 1024, time.Minute},
		{"zero", []Option{}, 1024, time.Minute},
		{"heartbeat only", []Option{WithEngineOptions(engineio.WithPingTimeout(3 * time.Second))}, 1024, time.Minute},
		{"negative", []Option{WithWriteBufferSize(-1), WithCloseTimeout(-time.Second)}, 1024, time.Minute},
		{"custom", []Option{WithWriteBufferSize(5), WithCloseTimeout(3 * time.Second)}, 5, 3 * time.Second},
		{"ordered", []Option{WithWriteBufferSize(2), WithCloseTimeout(time.Second), WithWriteBufferSize(5), WithCloseTimeout(3 * time.Second)}, 5, 3 * time.Second},
	}
	sides(t, "S", func(t *testing.T, side byte) {
		for _, tc := range cases {
			srv, nc, fc, errs := connWithOptions(t, side, tc.opts)
			require.Equal(t, tc.size, srv.limits.writeBufferSize, "%s: resolved queue size", tc.name)
			require.Equal(t, tc.drain, srv.limits.drainTimeout, "%s: resolved drain deadline", tc.name)
			fc.hold.Store(true)
			nc.Emit("inflight")
			recv(t, fc.held, "held writer")
			flood(nc, tc.size)
			require.False(t, isDone(fc.closed), "%s: queue accepts its configured budget", tc.name)
			nc.Emit("overflow")
			overflowErr := recv(t, errs, "configured queue overflow")
			require.ErrorIs(t, overflowErr, ErrWriteBufferFull)
			require.EqualError(t, overflowErr, "socketio: write buffer full")
			recv(t, fc.closed, "configured overflow closes engine")
		}
	})
}

func defectServer(t *testing.T, configure func(*Server)) (*Server, string) {
	t.Helper()
	s := NewServer(WithEngineOptions(shortPingOptions()...))
	s.OnConnect("/", func(Conn) error { return nil })
	configure(s)
	go func() { _ = s.Serve() }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	return s, ts.URL
}

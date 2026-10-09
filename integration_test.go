package socketio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

// connWithOptions returns the connection that a Server (S) or a Client (C) built with opts
// creates over a fakeConn.
func connWithOptions(t *testing.T, side byte, opts *engineio.Options) *conn {
	t.Helper()
	fc := newFakeConn(t)
	if side == 'C' {
		cl, err := NewClient("http://127.0.0.1/", opts)
		require.NoError(t, err)
		cl.dial = func(string) (engineio.Conn, error) { return fc, nil }
		cl.OnConnect(func(Conn) error { return nil })
		require.NoError(t, cl.Connect())
		closeAtEnd(t, fc, cl.conn)
		return cl.conn
	}
	srv := NewServer(opts)
	conns := make(chan Conn, 1)
	srv.OnConnect("/", func(c Conn) error { conns <- c; return nil })
	srv.serveConn(fc)
	c := recv(t, conns, "root OnConnect").(*namespaceConn).conn
	closeAtEnd(t, fc, c)
	return c
}

// TestOptionsReachConnections checks the queue size and drain deadline each connection
// stores. Engine.io sessions with a negative PingTimeout expire at once, and on a live
// session the write deadline equals PingTimeout, so the stored values are what is checked.
//
// Covers 1I-T2 (S, C).
// Covers 1I-T3 (S, C).
// Covers 1I-T4 (S, C).
// Covers 1I-T5 (S, C).
// Covers 1I-T6 (S, C).
func TestOptionsReachConnections(t *testing.T) {
	cases := []struct {
		name  string
		opts  *engineio.Options
		size  int
		drain time.Duration
	}{
		{"nil options", nil, 64, time.Minute},
		{"zero", &engineio.Options{}, 64, time.Minute},
		{"negative", &engineio.Options{WriteBufferSize: -1, PingTimeout: -time.Second}, 64, time.Minute},
		{"custom", &engineio.Options{WriteBufferSize: 5, PingTimeout: 3 * time.Second}, 5, 3 * time.Second},
	}
	sides(t, "SC", func(t *testing.T, side byte) {
		for _, tc := range cases {
			c := connWithOptions(t, side, tc.opts)
			require.Equal(t, tc.size, cap(c.writeChan), "%s: queue size", tc.name)
			require.Equal(t, tc.drain, c.drainTimeout, "%s: drain deadline", tc.name)
		}
	})
}

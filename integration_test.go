package socketio

import (
	"github.com/sshaplygin/go-socket.io/engineio"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// connWithOptions returns the connection that a Server (S) or a Client (C) built with opts
// creates over a fakeConn.
func connWithOptions(t *testing.T, side byte, opts *Options) *conn {
	t.Helper()
	fc := newFakeConn(t)

	srv := NewServer(opts)
	conns := make(chan Conn, 1)
	srv.OnConnect("/", func(c Conn) error { conns <- c; return nil })
	srv.serveConn(fc)
	c := recv(t, conns, "root OnConnect").(*namespaceConn).conn
	closeAtEnd(t, fc, c)
	return c
}

// TestOptionsReachConnections checks the queue size and drain deadline each connection
// stores. Engine.io sessions with a negative CloseTimeout expire at once, and on a live
// session the write deadline equals CloseTimeout, so the stored values are what is checked.
//
// Covers 1I-T2 (S).
// Covers 1I-T3 (S).
// Covers 1I-T4 (S).
// Covers 1I-T5 (S).
// Covers 1I-T6 (S).
func TestOptionsReachConnections(t *testing.T) {
	cases := []struct {
		name  string
		opts  *Options
		size  int
		drain time.Duration
	}{
		{"nil options", nil, 64, time.Minute},
		{"zero", &Options{}, 64, time.Minute},
		{"heartbeat only", &Options{Engine: &engineio.Options{PingTimeout: 3 * time.Second}}, 64, time.Minute},
		{"negative", &Options{WriteBufferSize: -1, CloseTimeout: -time.Second}, 64, time.Minute},
		{"custom", &Options{WriteBufferSize: 5, CloseTimeout: 3 * time.Second}, 5, 3 * time.Second},
	}
	sides(t, "S", func(t *testing.T, side byte) {
		for _, tc := range cases {
			c := connWithOptions(t, side, tc.opts)
			require.Equal(t, tc.size, cap(c.writeChan), "%s: queue size", tc.name)
			require.Equal(t, tc.drain, c.drainTimeout, "%s: drain deadline", tc.name)
		}
	})
}

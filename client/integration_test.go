package client

import (
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
)

func connWithOptions(t *testing.T, side byte, opts *engineio.Options) *conn {
	t.Helper()
	fc := newFakeConn(t)
	cl, err := NewClient("http://127.0.0.1/", opts)
	require.NoError(t, err)
	cl.dial = func(string) (engineio.Conn, error) { return fc, nil }
	cl.OnConnect(func(Conn) error { return nil })
	require.NoError(t, cl.Connect())
	closeAtEnd(t, fc, cl.conn)
	return cl.conn
}

// TestOptionsReachConnections checks the queue size and drain deadline each connection
// stores. Engine.io sessions with a negative PingTimeout expire at once, and on a live
// session the write deadline equals PingTimeout, so the stored values are what is checked.
//
// Covers 1I-T2 (C).
// Covers 1I-T3 (C).
// Covers 1I-T4 (C).
// Covers 1I-T5 (C).
// Covers 1I-T6 (C).
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
	sides(t, "C", func(t *testing.T, side byte) {
		for _, tc := range cases {
			c := connWithOptions(t, side, tc.opts)
			require.Equal(t, tc.size, cap(c.writeChan), "%s: queue size", tc.name)
			require.Equal(t, tc.drain, c.drainTimeout, "%s: drain deadline", tc.name)
		}
	})
}

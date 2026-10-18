package connection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
)

func connWithOptions(t *testing.T, side byte, opts []testOption) *conn {
	t.Helper()
	fc := newFakeConn(t)
	cl, err := newTestManager("http://127.0.0.1/", opts...)
	require.NoError(t, err)
	cl.dial = func(string) (engineclient.Conn, error) { return fc, nil }
	cl.OnConnect(func(Conn) error { return nil })
	require.NoError(t, cl.Connect())
	closeAtEnd(t, fc, cl.state.conn)
	return cl.state.conn
}

// TestOptionsReachConnections checks the queue size and drain deadline each connection
// stores. Engine.io sessions with a negative CloseTimeout expire at once, and on a live
// session the write deadline equals CloseTimeout, so the stored values are what is checked.
func TestResolvedConnectionLimits(t *testing.T) {
	cases := []struct {
		name  string
		opts  []testOption
		size  int
		drain time.Duration
	}{
		{"nil options", nil, 64, time.Minute},
		{"zero", []testOption{}, 64, time.Minute},
		{"dial timeout only", []testOption{WithTimeout(3 * time.Second)}, 64, time.Minute},
		{"negative", []testOption{WithWriteBufferSize(-1), WithCloseTimeout(-time.Second)}, 64, time.Minute},
		{"custom", []testOption{WithWriteBufferSize(5), WithCloseTimeout(3 * time.Second)}, 5, 3 * time.Second},
		{"ordered", []testOption{WithWriteBufferSize(2), WithCloseTimeout(time.Second), WithWriteBufferSize(5), WithCloseTimeout(3 * time.Second)}, 5, 3 * time.Second},
	}
	sides(t, "C", func(t *testing.T, side byte) {
		for _, tc := range cases {
			c := connWithOptions(t, side, tc.opts)
			require.Equal(t, tc.size, cap(c.writeChan), "%s: queue size", tc.name)
			require.Equal(t, tc.drain, c.drainTimeout, "%s: drain deadline", tc.name)
		}
	})
}

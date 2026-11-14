package session

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// deadlineConn is a scriptConn that records the deadlines the session sets.
type deadlineConn struct {
	scriptConn
	read, write time.Time
}

func (c *deadlineConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.read = t
	return nil
}

func (c *deadlineConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.write = t
	return nil
}

func (c *deadlineConn) deadlines() (read, write time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.read, c.write
}

// TestHeartbeatWindowIsIntervalPlusTimeout checks the liveness window the session
// gives the transport: a client stays connected for pingInterval + pingTimeout after
// the session opens and after each packet it sends, not for pingTimeout alone. The
// interval is the larger value, as with the defaults (25 s and 20 s), so a window of
// either parameter alone would drop a client that pings once per interval. The
// deadline must fall between pingInterval + pingTimeout after the clock readings
// taken before and after the call that arms it; no wall-clock margin is involved.
func TestHeartbeatWindowIsIntervalPlusTimeout(t *testing.T) {
	const interval, timeout = time.Hour, time.Minute
	const window = interval + timeout
	conn := &deadlineConn{scriptConn: scriptConn{reads: []scriptedPacket{{ft: eioparser.String, pt: eioparser.MESSAGE, data: "hello"}}}}
	requireWindow := func(what string, before, after time.Time) {
		t.Helper()
		read, write := conn.deadlines()
		for _, deadline := range []time.Time{read, write} {
			require.False(t, deadline.Before(before.Add(window)), "%s: deadline %v after the call started, want pingInterval + pingTimeout", what, deadline.Sub(before))
			require.False(t, deadline.After(after.Add(window)), "%s: deadline %v after the call returned, want pingInterval + pingTimeout", what, deadline.Sub(after))
		}
	}

	before := time.Now()
	s, err := New(conn, "sid", "websocket", eioparser.ConnParameters{PingInterval: interval, PingTimeout: timeout}, nil)
	after := time.Now()
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	requireWindow("open", before, after)
	opened, _ := conn.deadlines()

	before = time.Now()
	_, r, err := s.NextReader()
	after = time.Now()
	require.NoError(t, err)
	body, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.Equal(t, "hello", string(body))
	requireWindow("message", before, after)
	renewed, _ := conn.deadlines()
	require.False(t, renewed.Before(opened), "a packet must not shorten the window")
}

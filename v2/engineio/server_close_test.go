package engineio_test

import (
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// TestServerCloseClosesUnacceptedSessions checks that Close closes and
// removes the sessions nobody accepted: one in the hand-off buffer, one whose
// sender waits, and one whose handshake completes after Close. It checks them
// before calling Accept, which would also drop a session it receives after
// Close.
//
// Covers 1I-T11 (S).
func TestServerCloseClosesUnacceptedSessions(t *testing.T) {
	svr := engineio.NewServer(nil)
	httpSvr := httptest.NewServer(svr)
	defer httpSvr.Close()

	dial := func() engineio.Conn {
		d := client.Dialer{Transports: []transport.Transport{websocket.Default}}
		c, err := d.Dial(httpSvr.URL, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	clients := []engineio.Conn{dial(), dial()}
	require.Eventually(t, func() bool { return engineio.ConnChanLen(svr) == 1 }, time.Second, time.Millisecond,
		"a session in the hand-off buffer")
	require.NoError(t, svr.Close())
	clients = append(clients, dial())

	deadline := time.After(time.Second)
	errs := make(chan error, len(clients))
	for _, c := range clients {
		go func(c engineio.Conn) { _, _, err := c.NextReader(); errs <- err }(c)
	}
	for range clients {
		select {
		case err := <-errs:
			require.Error(t, err)
		case <-deadline:
			t.Fatal("a client of an unaccepted session can still read")
		}
	}
	for svr.Count() != 0 {
		select {
		case <-deadline:
			t.Fatalf("%d unaccepted sessions still registered", svr.Count())
		case <-time.After(5 * time.Millisecond):
		}
	}

	_, err := svr.Accept()
	require.ErrorIs(t, err, io.EOF)
}

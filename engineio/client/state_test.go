package client

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// Covers E20
func TestEngineClientState(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			s := engineio.NewServer()
			ts := httptest.NewServer(s)
			t.Cleanup(func() { _ = s.Close(); ts.Close() })
			c, err := (&Dialer{Transports: []transport.Transport{tr}}).Dial(ts.URL, nil)
			require.NoError(t, err)
			require.Equal(t, StateOpen, c.ReadyState())
			require.Equal(t, tr.Name(), c.Transport())
			require.False(t, c.Upgraded())
			require.NoError(t, c.Close())
			require.Equal(t, StateClosed, c.ReadyState())
		})
	}
	c := &client{conn: &brokenConn{}, close: make(chan struct{}), params: eioparser.ConnParameters{PingInterval: time.Millisecond}}
	require.Equal(t, StateOpen, c.ReadyState())
	c.serve()
	require.Equal(t, StateClosed, c.ReadyState(), "ping-loop exit closes the client state")
}

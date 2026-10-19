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

type heldCloseTransport struct {
	brokenConn
	entered, release chan struct{}
}

func (c *heldCloseTransport) Close() error {
	close(c.entered)
	<-c.release
	return nil
}

func TestEngineClientDonePrecedesTransportClose(t *testing.T) {
	tr := &heldCloseTransport{entered: make(chan struct{}), release: make(chan struct{})}
	c := &client{conn: tr}
	signal, ok := interface{}(c).(interface{ Done() <-chan struct{} })
	require.True(t, ok, "built-in client must expose close-start notification")
	done := signal.Done()
	select {
	case <-done:
		t.Fatal("client is still open")
	default:
	}
	finished := make(chan error, 1)
	go func() { finished <- c.Close() }()
	t.Cleanup(func() { close(tr.release); require.NoError(t, <-finished) })
	select {
	case <-tr.entered:
	case <-time.After(time.Second):
		t.Fatal("transport close did not start")
	}
	select {
	case <-done:
	default:
		t.Fatal("close signal must precede transport close completion")
	}
	require.Equal(t, done, signal.Done())
}

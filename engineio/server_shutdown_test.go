package engineio

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func TestServerCloseClosesAcceptedSessions(t *testing.T) {
	s := NewServer()
	srv := httptest.NewServer(s)
	defer srv.Close()
	d := engineclient.Dialer{Transports: []transport.Transport{websocket.Default}}
	c, err := d.Dial(srv.URL, nil)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	accepted, err := s.Accept()
	require.NoError(t, err)
	defer func() { _ = accepted.Close() }()
	require.NoError(t, s.Close())
	done := make(chan error, 1)
	go func() { _, _, err := c.NextReader(); done <- err }()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("accepted session survived Server.Close")
	}
	require.Zero(t, s.Count())
}

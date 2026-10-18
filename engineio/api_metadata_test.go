package engineio

import (
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func TestDialedClientContextIsLocal(t *testing.T) {
	server := NewServer(WithTransports([]transport.Transport{websocket.Default}...))
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	defer func() { require.NoError(t, server.Close()) }()
	require.Nil(t, server.Addr(), "an HTTP handler does not own the listener that serves it")
	accepted := make(chan Conn, 1)
	acceptErrors := make(chan error, 1)
	go func() {
		c, err := server.Accept()
		if err != nil {
			acceptErrors <- err
			return
		}
		accepted <- c
	}()
	dialer := engineclient.Dialer{Transports: []transport.Transport{websocket.Default}}
	c, err := dialer.Dial(httpServer.URL, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, c.Close()) }()
	var peer Conn
	select {
	case peer = <-accepted:
	case err := <-acceptErrors:
		t.Fatalf("accept: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not accept client")
	}
	peer.SetContext("server context")
	require.Nil(t, c.Context())
	value := &struct{ Name string }{"client context"}
	c.SetContext(value)
	require.Same(t, value, c.Context())
	require.Equal(t, "server context", peer.Context())
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func(value int) {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				c.SetContext(value)
				_ = c.Context()
			}
		}(i)
	}
	workers.Wait()
	c.SetContext(nil)
	require.Nil(t, c.Context(), "applications can clear their local state")
	require.Equal(t, "server context", peer.Context())
}

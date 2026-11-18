package client_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// After Close, a websocket client sends a normal closure frame behind the
// namespace DISCONNECT, so that the server can tell it from a drop (1006).
func TestClientCloseSendsWebsocketCloseFrame(t *testing.T) {
	closed := make(chan []string, 1) // the packets read, then the close
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()
		_ = ws.WriteMessage(gorilla.TextMessage, []byte(`0{"sid":"c","upgrades":[],"pingInterval":25000,"pingTimeout":5000}`))
		_ = ws.WriteMessage(gorilla.TextMessage, []byte("40"))
		var seen []string
		for err == nil {
			var data []byte
			if _, data, err = ws.ReadMessage(); err == nil {
				seen = append(seen, string(data))
			}
		}
		closed <- append(seen, err.Error())
	}))
	defer ts.Close()
	cl, err := client.New(ts.URL, client.WithTransports([]transport.Transport{websocket.Default}...), client.WithReconnection(false))
	require.NoError(t, err)
	ready := make(chan struct{}, 1)
	cl.OnConnect(func(client.Conn) error { ready <- struct{}{}; return nil })
	require.NoError(t, cl.Connect())
	recv(t, ready, "CONNECT")
	require.NoError(t, cl.Close())
	require.Equal(t, []string{"41", "websocket: close 1000 (normal)"}, recv(t, closed, "the end of the connection"))
}

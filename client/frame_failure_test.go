package client_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	gorilla "github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
)

// Covers C5
func TestClientMidFrameFailure(t *testing.T) {
	for _, mode := range []string{"reset", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			start, stop := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(start) })
			defer close(stop)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				peer, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = peer.Close() }()
				if peer.WriteMessage(gorilla.TextMessage, []byte(`0{"sid":"fragment","upgrades":[],"pingInterval":300,"pingTimeout":300}`)) != nil {
					return
				}
				if peer.WriteMessage(gorilla.TextMessage, []byte("40")) != nil {
					return
				}
				<-start
				// PONG arms a real read deadline; keep the next MESSAGE unfinished.
				if mode == "timeout" && peer.WriteMessage(gorilla.TextMessage, []byte("3")) != nil {
					return
				}
				writer, err := peer.NextWriter(gorilla.TextMessage)
				if err != nil {
					return
				}
				if _, err := writer.Write([]byte(`42["event","partial` + strings.Repeat("x", 20000))); err != nil {
					return
				}
				if mode == "timeout" {
					<-stop
				}
				// Closing TCP without completing the websocket message reproduces a
				// body Read failure after Engine.IO's NextReader already succeeded.
				_ = peer.NetConn().Close()
			}))
			t.Cleanup(ts.Close)
			cl, err := client.New(ts.URL, &client.Options{Transports: []transport.Transport{websocket.Default}})
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			ready, reasons, failures := make(chan struct{}, 1), make(chan string, 1), make(chan error, 8)
			cl.OnConnect(func(client.Conn) error { ready <- struct{}{}; return nil })
			cl.OnEvent("event", func(client.Conn, string) {})
			cl.OnError(func(_ client.Conn, err error) { failures <- err })
			cl.OnDisconnect(func(_ client.Conn, reason string) { reasons <- reason })
			require.NoError(t, cl.Connect())
			recv(t, ready, "CONNECT before fragmented event")
			once.Do(func() { close(start) })
			failure := recv(t, failures, "original frame error")
			want := "transport error"
			if mode == "timeout" {
				want = "ping timeout"
				var timeout net.Error
				require.ErrorAs(t, failure, &timeout)
				require.True(t, timeout.Timeout())
			} else {
				var closed *gorilla.CloseError
				require.ErrorAs(t, failure, &closed)
				require.Equal(t, gorilla.CloseAbnormalClosure, closed.Code)
			}
			require.Equal(t, want, recv(t, reasons, "mid-frame disconnect cause"))
		})
	}
}

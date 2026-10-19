package client_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
)

// Covers G9
func TestClientIgnoredBinaryPacketKeepsConnection(t *testing.T) {
	for _, packet := range []string{`451-/missing,["ignored",{"_placeholder":true,"num":0}]`, `461-/missing,7[{"_placeholder":true,"num":0}]`} {
		t.Run(packet[:3], func(t *testing.T) {
			stop := make(chan struct{})
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ws, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = ws.Close() }()
				for _, frame := range []string{`0{"sid":"skip","upgrades":[],"pingInterval":25000,"pingTimeout":20000}`, "40", packet} {
					if ws.WriteMessage(gorilla.TextMessage, []byte(frame)) != nil {
						return
					}
				}
				if ws.WriteMessage(gorilla.BinaryMessage, []byte{4, 1, 2, 3}) != nil {
					return
				}
				if ws.WriteMessage(gorilla.TextMessage, []byte(`42["alive"]`)) != nil {
					return
				}
				<-stop
			}))
			defer ts.Close()
			defer close(stop)
			c, err := client.New(ts.URL, client.WithReconnection(false), client.WithTransports([]transport.Transport{websocket.Default}...))
			require.NoError(t, err)
			defer func() { _ = c.Close() }()
			alive := make(chan struct{}, 1)
			failures := make(chan error, 4)
			c.OnEvent("alive", func(client.Conn) { alive <- struct{}{} })
			c.OnError(func(_ client.Conn, err error) { failures <- err })
			require.NoError(t, c.Connect())
			select {
			case <-alive:
			case err := <-failures:
				t.Fatalf("ignored attachment broke session: %v", err)
			case <-time.After(time.Second):
				t.Fatal("valid event after ignored binary packet was lost")
			}
		})
	}
}

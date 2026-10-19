package client_test

import (
	"fmt"
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

// Covers G16
func TestClientMessageLimitOption(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		for _, limit := range []int64{512, 4096, 0, -1} {
			t.Run(tr.Name()+"/"+fmt.Sprint(limit), func(t *testing.T) {
				payload := strings.Repeat("x", 2048)
				ts := newTestServer(t, func(s *socketio.Server) {
					s.OnConnect("/", func(socketio.Conn) error { return nil })
					s.OnEvent("/", "request", func(c socketio.Conn) { c.Emit("reply", payload) })
				})
				c, err := client.New(ts.URL, client.WithTransports(tr), client.WithReconnection(false), client.WithMaxMessageSize(1), client.WithMaxMessageSize(limit))
				require.NoError(t, err)
				defer c.Close()
				replies := make(chan string, 1)
				failures := make(chan error, 8)
				c.OnEvent("reply", func(_ client.Conn, s string) { replies <- s })
				c.OnError(func(_ client.Conn, err error) {
					select {
					case failures <- err:
					default:
					}
				})
				require.NoError(t, c.Connect())
				c.Emit("request")
				select {
				case s := <-replies:
					require.NotEqual(t, int64(512), limit)
					require.Equal(t, payload, s)
				case err := <-failures:
					require.Equal(t, int64(512), limit, "unexpected error: %v", err)
				case <-time.After(3 * time.Second):
					t.Fatal("neither event nor size-limit error delivered")
				}
			})
		}
	}
}

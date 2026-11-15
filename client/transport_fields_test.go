package client_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// WithPerMessageDeflate alone decides whether the client offers websocket compression:
// EnableCompression and CompressionThreshold of a websocket.Transport passed to
// WithTransports are replaced on the client's copy, and the caller's value is kept.
func TestWithTransportsReplacesWebsocketCompressionFields(t *testing.T) {
	for _, deflate := range []bool{false, true} {
		t.Run(strconv.FormatBool(deflate), func(t *testing.T) {
			s := socketio.NewServer(socketio.WithEngineOptions(engineio.WithPerMessageDeflate(true)))
			connected := make(chan struct{}, 1)
			s.OnConnect("/", func(socketio.Conn) error { connected <- struct{}{}; return nil })
			go func() { _ = s.Serve() }()
			offered := make(chan string, 1)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("transport") == "websocket" {
					offered <- r.Header.Get("Sec-Websocket-Extensions")
				}
				s.ServeHTTP(w, r)
			}))
			t.Cleanup(func() { _ = s.Close(); ts.Close() })

			mine := &websocket.Transport{EnableCompression: !deflate, CompressionThreshold: 1}
			cl, err := client.New(ts.URL, client.WithTransports(mine), client.WithPerMessageDeflate(deflate), client.WithReconnection(false))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			require.NoError(t, cl.Connect())
			compressionReceive(t, connected)
			ext := compressionReceive(t, offered)
			if deflate {
				require.Contains(t, ext, "permessage-deflate")
			} else {
				require.NotContains(t, ext, "permessage-deflate")
			}
			require.Equal(t, &websocket.Transport{EnableCompression: !deflate, CompressionThreshold: 1}, mine)
		})
	}
}

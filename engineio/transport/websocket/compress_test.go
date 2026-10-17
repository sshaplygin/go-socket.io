package websocket

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/stretchr/testify/require"
)

// Covers E8
func TestInflatedLimitIsTerminalOnBothSides(t *testing.T) {
	for _, side := range []string{"server", "client"} {
		t.Run(side, func(t *testing.T) {
			tr := &Transport{EnableCompression: true, CompressionThreshold: 1}
			accepted := make(chan transport.Conn, 1)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := tr.Accept(w, r)
				if err == nil {
					accepted <- c
				}
			}))
			defer ts.Close()
			target, err := url.Parse(ts.URL)
			require.NoError(t, err)
			client, err := tr.Dial(target, nil)
			require.NoError(t, err)
			defer func() { _ = client.Close() }()
			var server transport.Conn
			select {
			case server = <-accepted:
			case <-time.After(time.Second):
				t.Fatal("websocket not accepted")
			}
			defer func() { _ = server.Close() }()
			limited, sender := server, client
			if side == "client" {
				limited, sender = client, server
			}
			limited.(interface{ SetReadLimit(int64) }).SetReadLimit(1024)
			require.NoError(t, limited.SetReadDeadline(time.Now().Add(time.Second)))
			write := func(pt packet.Type, value string) {
				t.Helper()
				w, err := sender.NextWriter(frame.String, pt)
				require.NoError(t, err)
				_, err = io.WriteString(w, value)
				require.NoError(t, err)
				require.NoError(t, w.Close())
			}
			write(packet.NOOP, strings.Repeat("a", 100_000))
			_, pt, r, err := limited.NextReader()
			require.NoError(t, err)
			require.Equal(t, packet.NOOP, pt)
			_, err = io.Copy(io.Discard, r)
			require.ErrorIs(t, err, gorilla.ErrReadLimit)
			require.ErrorIs(t, r.Close(), gorilla.ErrReadLimit)
			write(packet.MESSAGE, "after-limit")
			_, _, r, err = limited.NextReader()
			require.ErrorIs(t, err, gorilla.ErrReadLimit)
			require.Nil(t, r, "no later message may escape the failed websocket")
		})
	}
}

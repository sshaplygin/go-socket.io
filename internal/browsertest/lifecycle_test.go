//go:build js && wasm

package browsertest

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func TestReconnection(t *testing.T) {
	for event, tc := range map[string]struct {
		reason     string
		reconnects bool
	}{
		"close-engine": {"transport close", true}, // close code 1000
		"drop":         {"transport error", true}, // TCP reset
		"kick":         {"io server disconnect", false},
	} {
		t.Run(event, func(t *testing.T) {
			cl, sockets := connect(t, "/socket.io/", []string{"/"}, client.WithReconnection(true),
				client.WithReconnectionDelay(50*time.Millisecond), client.WithRandomizationFactor(-1))
			reasons, reconnected, connected := make(chan string, 1), make(chan int, 1), make(chan string, 1)
			sockets[0].OnDisconnect(func(_ client.Conn, reason string) { reasons <- reason })
			cl.OnReconnect(func(attempt int) { reconnected <- attempt })
			sockets[0].OnConnect(func(c client.Conn) error { connected <- c.ID(); return nil })
			sid := sockets[0].ID()
			sockets[0].Emit(event)
			require.Equal(t, tc.reason, receive(t, reasons, "disconnect"))
			if !tc.reconnects {
				// Ten reconnection delays without a reconnection.
				select {
				case <-reconnected:
					t.Fatal("reconnected after io server disconnect")
				case <-time.After(500 * time.Millisecond):
				}
				require.False(t, sockets[0].Connected())
				return
			}
			require.Equal(t, 1, receive(t, reconnected, "reconnect"))
			require.NotEqual(t, sid, receive(t, connected, "CONNECT after reconnection"))
			require.Equal(t, "again", ack[string](t, sockets[0], "echo", "again"))
		})
	}
}

func TestConnectTimeout(t *testing.T) {
	for name, tc := range map[string]struct {
		timeout   time.Duration
		transport transport.Transport
		err       error         // nil: an error other than ErrConnectTimeout
		bound     time.Duration // after Close, until no library goroutine remains
	}{
		// Under the 2 s Client.Timeout: only the abort of the timed-out dial ends the fetch in time.
		"polling":                 {300 * time.Millisecond, &polling.Transport{Client: &http.Client{Timeout: 2 * time.Second}}, client.ErrConnectTimeout, time.Second},
		"websocket":               {300 * time.Millisecond, &websocket.Transport{HandshakeTimeout: 2 * time.Second}, client.ErrConnectTimeout, 3 * time.Second},
		"websocket handshake 300": {2 * time.Second, &websocket.Transport{HandshakeTimeout: 300 * time.Millisecond}, nil, time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			awaitNoLibraryGoroutines(t, 5*time.Second)
			cl, err := client.New(os.Getenv("SOCKETIO_BROWSER_URL"), client.WithPath("/silent/"),
				client.WithTimeout(tc.timeout), client.WithTransports(tc.transport), client.WithReconnection(false))
			require.NoError(t, err)
			start := time.Now()
			err = cl.Connect()
			require.Less(t, time.Since(start), 1300*time.Millisecond)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.Error(t, err)
				require.NotErrorIs(t, err, client.ErrConnectTimeout)
			}
			require.NoError(t, cl.Close())
			awaitNoLibraryGoroutines(t, tc.bound)
		})
	}
}

// A message over WithMaxMessageSize closes the browser's WebSocket with code 4009.
func TestMaxMessageSize(t *testing.T) {
	_, sockets := connect(t, "/socket.io/", []string{"/"}, client.WithMaxMessageSize(512))
	reasons := make(chan string, 1)
	sockets[0].OnDisconnect(func(_ client.Conn, reason string) { reasons <- reason })
	sid := sockets[0].ID()
	sockets[0].Emit("echo", strings.Repeat("x", 1024))
	require.Equal(t, "transport error", receive(t, reasons, "disconnect"))
	_, observer := connect(t, "/socket.io/", []string{"/"})
	// The server records the close code when its read of the close frame fails.
	eventually(t, 5*time.Second, "close code of "+sid, func() bool {
		return ack[int](t, observer[0], "closecode", sid) == 4009
	})
}

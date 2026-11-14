//go:build conformance

package conformance

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// goClientLocalEvents are the names that socket.io-client 2.5.0 and 1.7.4 keep
// local (lib/socket.js events), and disconnecting.
var goClientLocalEvents = []string{
	"connect", "connect_error", "connect_timeout", "connecting",
	"disconnect", "disconnecting", "error",
	"reconnect", "reconnect_attempt", "reconnect_error", "reconnect_failed", "reconnecting",
	"ping", "pong",
}

// TestGoClientKeepsLocalEventsOffTheWire: the Go client emits every local name
// through each send path, before and after the namespaces connect, to the
// reference server, which records each event it receives and has no socket
// error listener. None reaches it: the server keeps running, its disconnect
// listeners do not run, and it receives only the ordinary events.
func TestGoClientKeepsLocalEventsOffTheWire(t *testing.T) {
	for _, name := range []string{"polling", "websocket"} {
		t.Run(name, func(t *testing.T) {
			tr := transport.Transport(websocket.Default)
			if name == "polling" {
				tr = polling.Default
			}
			// Without the rule, the buffer holds every local emit, and the server sees them.
			cl, err := client.New(goClientReference(t), client.WithPath("/client-engine"), client.WithTransports(tr), client.WithWriteBufferSize(1024))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			connected := make(chan client.Conn, 2)
			root, chat := cl.Socket("/"), cl.Socket("/chat")
			for _, s := range []*client.Socket{root, chat} {
				s.OnConnect(func(c client.Conn) error { connected <- c; return nil })
				for _, event := range goClientLocalEvents {
					s.Emit(event, "before connect", func() {})
				}
			}
			require.NoError(t, cl.Connect())
			conns := map[string]client.Conn{}
			for i := 0; i < 2; i++ {
				c := goClientReceive(t, connected, "namespace CONNECT")
				conns[c.Namespace()] = c
			}
			for _, s := range []*client.Socket{root, chat} {
				c := conns[s.Namespace()]
				for _, event := range goClientLocalEvents {
					cl.Emit(event, "client")
					s.Emit(event, "socket")
					s.Emit(event, "socket with ack", func() {})
					c.Emit(event, "conn")
					c.Emit(event, "conn with ack", func() {})
					c.Volatile().Emit(event, "volatile")
					c.Compress(false).Emit(event, "uncompressed", func() {})
				}
				echoed := make(chan string, 1)
				s.Emit("echo", s.Namespace(), func(value string) { echoed <- value })
				require.Equal(t, s.Namespace(), goClientReceive(t, echoed, "Node ACK after the local emits"))
			}
			meta := make(chan goClientMetadata, 1)
			root.Emit("inspect", func(value goClientMetadata) { meta <- value })
			got := goClientReceive(t, meta, "Node metadata ACK")
			require.Equal(t, [][]string{{"/", "echo"}, {"/chat", "echo"}, {"/", "inspect"}}, got.Received)
			require.Empty(t, got.Disconnected)
			require.ElementsMatch(t, []string{"/", "/chat"}, got.Connected)
		})
	}
}

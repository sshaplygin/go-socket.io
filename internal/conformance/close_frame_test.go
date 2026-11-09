//go:build conformance

package conformance

import (
	"net/http/httptest"
	"testing"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
)

// TestJSServerCloseFrame stops a session from the server and has the pinned
// JavaScript clients record the websocket close code: engine.io 3.6.2 closes its
// websocket with a close frame, and so does this server.
func TestJSServerCloseFrame(t *testing.T) {
	for _, mode := range []string{"websocket", "upgrade"} {
		for _, trigger := range []string{"drop-transport", "close-server"} {
			for _, compression := range []string{"", "deflate"} {
				name := mode + "/" + trigger
				if compression != "" {
					name += "/" + compression
				}
				t.Run(name, func(t *testing.T) {
					s := socketio.NewServer(socketio.WithEngineOptions(engineio.WithPerMessageDeflate(compression != "")))
					s.OnConnect("/", func(socketio.Conn) error { return nil })
					s.OnEvent("/", "echo", func(c socketio.Conn, value string) { c.Emit("reply", value) })
					s.OnEvent("/", "drop-transport", func(c socketio.Conn) { _ = c.EngineConn().Close() })
					s.OnEvent("/", "close-server", func(socketio.Conn) { go func() { _ = s.Close() }() })
					served := make(chan struct{})
					go func() { _ = s.Serve(); close(served) }()
					ts := httptest.NewServer(s)
					t.Cleanup(func() { _ = s.Close(); ts.Close(); <-served })
					runNode(t, "testdata/node/close-client.js", ts.URL, mode, trigger, compression)
				})
			}
		}
	}
}

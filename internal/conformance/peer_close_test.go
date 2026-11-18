//go:build conformance

package conformance

import (
	"net/http/httptest"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
)

// TestJSEngineCloseFrame closes the pinned JavaScript client's engine directly,
// without a Socket.IO DISCONNECT, over websocket and after an upgrade: its
// websocket sends a close frame, which socket.io 2.5.0 reports to its
// disconnect handler as "transport close", and so does this server.
func TestJSEngineCloseFrame(t *testing.T) {
	for _, mode := range []string{"websocket", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			reasons := make(chan string, 1)
			s := socketio.NewServer()
			s.OnConnect("/", func(socketio.Conn) error { return nil })
			s.OnDisconnect("/", func(_ socketio.Conn, reason string) {
				select {
				case reasons <- reason:
				default:
				}
			})
			served := make(chan struct{})
			go func() { _ = s.Serve(); close(served) }()
			ts := httptest.NewServer(s)
			t.Cleanup(func() { _ = s.Close(); ts.Close(); <-served })
			runNode(t, "testdata/node/engine-close-client.js", ts.URL, mode)
			select {
			case reason := <-reasons:
				if reason != "transport close" {
					t.Fatalf("disconnect reason = %q, want transport close", reason)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("server did not report the disconnect")
			}
		})
	}
}

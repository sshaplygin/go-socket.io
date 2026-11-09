//go:build conformance

package conformance

import (
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/stretchr/testify/require"
)

// TestNodeAckForUndecodableArguments: a Node client that calls an untyped Go
// handler with arguments of the wrong type receives the error ACK, so its
// callback runs, and the server reports every mismatch to OnError.
func TestNodeAckForUndecodableArguments(t *testing.T) {
	for _, transport := range []string{"polling", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			server := socketio.NewServer()
			reports := make(chan error, 16)
			ns := server.Of("/chat")
			ns.OnConnect(func(socketio.Conn) error { return nil })
			ns.OnError(func(_ socketio.Conn, err error) {
				var typeErr *json.UnmarshalTypeError
				if errors.As(err, &typeErr) {
					reports <- err
				} else if !errors.Is(err, io.EOF) {
					t.Logf("OnError: %v", err)
				}
			})
			ns.OnEvent("count", func(_ socketio.Conn, n int) int { return n + 1 })
			ns.OnEvent("pair", func(_ socketio.Conn, n int, s string) string { return s })
			served := make(chan error, 1)
			go func() { served <- server.Serve() }()
			httpServer := httptest.NewServer(server)
			t.Cleanup(func() {
				closeErr := server.Close()
				httpServer.Close()
				require.NoError(t, closeErr)
				require.NoError(t, <-served)
			})

			runNode(t, "testdata/node/ack-mismatch.js", httpServer.URL, transport)

			// Four mismatches asked for an ACK and two did not.
			for i := 0; i < 6; i++ {
				select {
				case <-reports:
				case <-time.After(5 * time.Second):
					t.Fatalf("OnError received %d of 6 decode failures", i)
				}
			}
		})
	}
}

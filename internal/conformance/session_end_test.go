//go:build conformance

package conformance

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
)

// TestJSSocketClose closes a pinned JavaScript client's socket with socket.close(). As
// with socket.io 2.5.0, whose server emits no error for a disconnect, the end of the
// session reaches OnDisconnect only: OnError is not called.
func TestJSSocketClose(t *testing.T) {
	for _, transport := range []string{"polling", "websocket", "upgrade"} {
		t.Run(transport, func(t *testing.T) {
			s := socketio.NewServer()
			errs, reasons := make(chan error, 8), make(chan string, 1)
			s.OnConnect("/", func(socketio.Conn) error { return nil })
			s.OnEvent("/", "echo", func(_ socketio.Conn, value string) string { return value })
			s.OnError("/", func(_ socketio.Conn, err error) { errs <- err })
			s.OnDisconnect("/", func(_ socketio.Conn, reason string) { reasons <- reason })
			served := make(chan struct{})
			go func() { _ = s.Serve(); close(served) }()
			ts := httptest.NewServer(s)
			t.Cleanup(func() { _ = s.Close(); ts.Close(); <-served })
			runNode(t, "testdata/node/client.js", ts.URL, transport, "close")
			select {
			case reason := <-reasons:
				require.Equal(t, "client namespace disconnect", reason)
			case <-time.After(5 * time.Second):
				t.Fatal("no OnDisconnect")
			}
			// The session ends after the namespace; Close then waits for its connection.
			require.Eventually(t, func() bool { return s.Count() == 0 }, 5*time.Second, 10*time.Millisecond)
			require.NoError(t, s.Close())
			if len(errs) > 0 {
				t.Fatalf("OnError: %v", <-errs)
			}
		})
	}
}

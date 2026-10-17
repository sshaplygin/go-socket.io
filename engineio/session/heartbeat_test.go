package session

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// Covers E1
// Covers P3
func TestPingTimeoutIdleClient(t *testing.T) {
	for _, name := range []string{"ping", "messages", "silent"} {
		t.Run(name, func(t *testing.T) {
			accepted := make(chan *Session, 1)
			done := make(chan error, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := websocket.Default.Accept(w, r)
				if err != nil {
					done <- err
					return
				}
				s, err := New(c, "heartbeat", "websocket", transport.ConnParameters{
					PingInterval: 100 * time.Millisecond, PingTimeout: 50 * time.Millisecond,
				}, nil)
				if err != nil {
					done <- err
					return
				}
				accepted <- s
				for {
					_, body, err := s.NextReader()
					if err != nil {
						done <- err
						return
					}
					_, err = io.Copy(io.Discard, body)
					closeErr := body.Close()
					if err != nil || closeErr != nil {
						done <- err
						return
					}
				}
			}))
			defer srv.Close()
			c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			require.NoError(t, err)
			defer func() { _ = c.Close() }()
			s := <-accepted
			defer func() { _ = s.Close() }()
			// Drain PONGs independently of the client's packet cadence.
			go func() {
				for {
					if _, _, err := c.ReadMessage(); err != nil {
						return
					}
				}
			}()
			if name != "silent" {
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()
				for i := 0; i < 5; i++ {
					select {
					case err := <-done:
						t.Fatalf("client closed before its next packet: %v", err)
					case <-ticker.C:
					}
					msg := "2"
					if name == "messages" {
						msg = "4hello"
					}
					require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte(msg)))
				}
				select {
				case err := <-done:
					t.Fatalf("active client closed: %v", err)
				default:
				}
			}
			select {
			case err := <-done:
				require.Error(t, err)
				require.NotNil(t, s.cause.Load())
				require.Equal(t, reasonPingTimeout, s.cause.Load().reason)
			case <-time.After(time.Second):
				t.Fatal("silent client did not time out")
			}
		})
	}
}

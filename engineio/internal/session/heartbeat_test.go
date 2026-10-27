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

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

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
				s, err := New(c, "heartbeat", "websocket", eioparser.ConnParameters{
					PingInterval: time.Second, PingTimeout: 500 * time.Millisecond,
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
			// Activity lasts longer than the entire idle window, so packets must reset it.
			// Leave enough scheduling margin for coverage and race instrumentation.
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
				for i := 0; i < 20; i++ {
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
			case <-time.After(3 * time.Second):
				t.Fatal("silent client did not time out")
			}
		})
	}
}

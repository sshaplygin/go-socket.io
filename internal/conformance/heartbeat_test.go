//go:build conformance

package conformance

import (
	"net/http/httptest"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
)

func TestJSHeartbeat(t *testing.T) {
	for _, transport := range []string{"polling", "websocket"} {
		for _, scenario := range []string{"heartbeat", "heartbeat-timeout"} {
			t.Run(transport+"/"+scenario, func(t *testing.T) {
				type disconnect struct {
					reason string
					at     time.Time
				}
				connected := make(chan time.Time, 1)
				disconnected := make(chan disconnect, 1)
				s := socketio.NewServer(socketio.WithEngineOptions(
					engineio.WithPingInterval(200*time.Millisecond),
					engineio.WithPingTimeout(200*time.Millisecond),
				))
				s.OnConnect("/", func(socketio.Conn) error { connected <- time.Now(); return nil })
				s.OnDisconnect("/", func(_ socketio.Conn, reason string) {
					disconnected <- disconnect{reason: reason, at: time.Now()}
				})
				s.OnEvent("/", "echo", func(_ socketio.Conn, value string) string { return value })
				served := make(chan error, 1)
				go func() { served <- s.Serve() }()
				ts := httptest.NewServer(s)
				t.Cleanup(func() {
					_ = s.Close()
					ts.Close()
					if err := <-served; err != nil {
						t.Errorf("Serve: %v", err)
					}
				})
				runNode(t, "testdata/node/client.js", ts.URL, transport, scenario)
				if scenario == "heartbeat-timeout" {
					select {
					case result := <-disconnected:
						if result.reason != "ping timeout" {
							t.Fatalf("disconnect reason = %q, want ping timeout", result.reason)
						}
						if elapsed := result.at.Sub(<-connected); elapsed >= time.Second {
							t.Fatalf("disconnect took %v, want less than 1s", elapsed)
						}
					default:
						t.Fatal("server did not disconnect the client while pings were suppressed")
					}
				}
			})
		}
	}
}

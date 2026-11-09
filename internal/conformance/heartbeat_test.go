//go:build conformance

package conformance

import (
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
)

// TestJSHeartbeat runs the pinned JavaScript client against a short heartbeat. The client
// script receives the configured interval and timeout and derives its waits from them:
// "heartbeat" waits for enough pongs to outlive one window, "heartbeat-timeout" never
// pings and waits for the server to close it. The timeout leaves a loaded host room to
// answer a ping; the interval keeps the scenarios short.
func TestJSHeartbeat(t *testing.T) {
	const pingInterval, pingTimeout = 200 * time.Millisecond, time.Second
	const window = pingInterval + pingTimeout
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
					engineio.WithPingInterval(pingInterval),
					engineio.WithPingTimeout(pingTimeout),
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
				runNode(t, "testdata/node/client.js", ts.URL, transport, scenario,
					strconv.FormatInt(pingInterval.Milliseconds(), 10), strconv.FormatInt(pingTimeout.Milliseconds(), 10))
				if scenario == "heartbeat-timeout" {
					// The client has seen the server close it; OnDisconnect reports why.
					select {
					case result := <-disconnected:
						if result.reason != "ping timeout" {
							t.Fatalf("disconnect reason = %q, want ping timeout", result.reason)
						}
						// The configured window, not a default one, ended the session.
						if elapsed := result.at.Sub(<-connected); elapsed >= 2*window {
							t.Fatalf("disconnect took %v, want less than %v", elapsed, 2*window)
						}
					case <-time.After(window):
						t.Fatal("server did not report the disconnect of the client it closed")
					}
				}
			})
		}
	}
}

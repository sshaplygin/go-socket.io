package socketio

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

func TestStalledReaderIsDisconnected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		frame   string
		upgrade bool
	}{
		{"ping-websocket", "2", false},
		{"message-websocket", `42["x"]`, false},
		{"ping-upgrade", "2", true},
		{"message-upgrade", `42["x"]`, true},
		{"silent-websocket", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer(WithEngineOptions(
				engineio.WithPingInterval(2*time.Second),
				engineio.WithPingTimeout(2*time.Second),
				engineio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
			))
			start, done := make(chan struct{}), make(chan struct{})
			connected := make(chan Conn, 1)
			failures := make(chan error, 8)
			stop := make(chan struct{})
			srv.OnConnect("/", func(c Conn) error {
				connected <- c
				go func() {
					select {
					case <-start:
					case <-stop:
						return
					}
					ticker := time.NewTicker(2 * time.Millisecond)
					defer ticker.Stop()
					payload := strings.Repeat("x", 256000)
					for i := 0; i < 400; i++ {
						select {
						case <-done:
							return
						case <-stop:
							return
						case <-ticker.C:
							c.Emit("large", payload)
						}
					}
				}()
				return nil
			})
			srv.OnEvent("/", "x", func(Conn) {})
			srv.OnError("/", func(_ Conn, err error) {
				select {
				case failures <- err:
				default:
				}
			})
			srv.OnDisconnect("/", func(Conn, string) { close(done) })
			go func() { _ = srv.Serve() }()
			ts := httptest.NewServer(srv)
			t.Cleanup(func() { _ = srv.Close(); ts.Close() })
			t.Cleanup(func() { close(stop) })
			wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/?EIO=3&transport=websocket"
			var poll string
			client := &http.Client{Timeout: 2 * time.Second}
			t.Cleanup(client.CloseIdleConnections)
			get := func(target string) []string {
				t.Helper()
				response, err := client.Get(target)
				require.NoError(t, err)
				defer func() { _ = response.Body.Close() }()
				require.Equal(t, http.StatusOK, response.StatusCode)
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				var packets []string
				for len(body) > 0 {
					prefix, rest, ok := strings.Cut(string(body), ":")
					require.True(t, ok, "invalid polling payload %q", body)
					n, err := strconv.Atoi(prefix)
					require.NoError(t, err)
					require.Positive(t, n)
					require.LessOrEqual(t, n, len(rest))
					packets = append(packets, rest[:n])
					body = []byte(rest[n:])
				}
				return packets
			}
			if tc.upgrade {
				packets := get(ts.URL + "/?EIO=3&transport=polling&b64=1")
				require.NotEmpty(t, packets)
				require.True(t, strings.HasPrefix(packets[0], "0"))
				params, err := eioparser.ReadConnParameters(strings.NewReader(packets[0][1:]))
				require.NoError(t, err)
				poll = ts.URL + "/?EIO=3&transport=polling&b64=1&sid=" + params.SID
				connectedPacket := false
				for !connectedPacket {
					for _, packet := range packets {
						connectedPacket = connectedPacket || packet == "40"
					}
					if !connectedPacket {
						packets = get(poll)
					}
				}
				wsURL += "&sid=" + params.SID
			}
			ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = ws.Close() })
			require.NoError(t, ws.SetReadDeadline(time.Now().Add(waitFor)))
			if tc.upgrade {
				require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("2probe")))
				_, packet, err := ws.ReadMessage()
				require.NoError(t, err)
				require.Equal(t, "3probe", string(packet))
				require.Equal(t, []string{"6"}, get(poll))
				require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("5")))
			} else {
				_, packet, err := ws.ReadMessage()
				require.NoError(t, err)
				require.True(t, strings.HasPrefix(string(packet), "0"))
				_, packet, err = ws.ReadMessage()
				require.NoError(t, err)
				require.Equal(t, "40", string(packet))
			}
			conn := recv(t, connected, "OnConnect")
			if tc.upgrade {
				require.Eventually(t, conn.EngineConn().Upgraded, time.Second, time.Millisecond)
			}
			close(start)
			if tc.frame != "" {
				go func() {
					ticker := time.NewTicker(100 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-stop:
							return
						case <-ticker.C:
							if ws.SetWriteDeadline(time.Now().Add(time.Second)) != nil || ws.WriteMessage(websocket.TextMessage, []byte(tc.frame)) != nil {
								return
							}
						}
					}
				}()
				err = recv(t, failures, "write buffer overflow")
				require.True(t, errors.Is(err, ErrWriteBufferFull), "unexpected error: %v", err)
			}
			// t.Error, not Fatal, so a missing disconnect and a slow Close are both reported.
			select {
			case <-done:
			case <-time.After(waitFor):
				t.Error("stalled reader did not receive OnDisconnect")
			}
			closed := make(chan error, 1)
			go func() { closed <- srv.Close() }()
			select {
			case err := <-closed:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Error("Server.Close did not finish within two seconds")
			}
		})
	}
}

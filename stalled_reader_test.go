package socketio

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
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
			// At most 400 emits of 256 KB overflow an explicit 64-packet queue; the default
			// is larger. A write blocked by the stalled reader fails at its deadline,
			// PingInterval + PingTimeout after the last packet read before the write began,
			// and a silent client times out after that window too. The overflow, which every
			// subtest awaits for that window, must end the session before its deadlines do.
			const writeBuffer, pingInterval, pingTimeout = 64, waitFor, waitFor
			start, done := make(chan struct{}), make(chan struct{})
			var created atomic.Int64          // MESSAGE packets the session has started to write
			inbound := make(chan struct{}, 1) // a packet was read after start
			var written atomic.Int64          // frames the client has written
			// Frames the client wrote between the read that releases the overflow and
			// the first emit of the overflow; -1 until a sending subtest gets there.
			var margin atomic.Int64
			margin.Store(-1)
			upgraded := make(chan struct{})
			srv := NewServer(WithEngineOptions(
				engineio.WithPingInterval(pingInterval),
				engineio.WithPingTimeout(pingTimeout),
				engineio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
				engineio.WithHooks(&engineio.Hooks{
					Packet: func(engineio.Conn, eioparser.PacketType, eioparser.FrameType) {
						select {
						case <-start:
							select {
							case inbound <- struct{}{}:
							default:
							}
						default:
						}
					},
					PacketCreate: func(_ engineio.Conn, pt eioparser.PacketType, _ eioparser.FrameType) {
						if pt == eioparser.MESSAGE {
							created.Add(1)
						}
					},
					Upgrade: func(engineio.Conn, string) { close(upgraded) },
				}),
			), WithWriteBufferSize(writeBuffer))
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
					// Emission follows the session, not a clock. Once the writer is half a
					// queue behind, as a reader that never reads leaves it, the emits that
					// overflow the queue wait until a sending client has delivered a packet
					// since emission began, so the server reads while its writer lags.
					payload := strings.Repeat("x", 256000)
					base, behind := created.Load(), false
					for i := int64(0); i < 400; i++ {
						select {
						case <-done:
							return
						case <-stop:
							return
						default:
						}
						if !behind && i-(created.Load()-base) >= writeBuffer/2 {
							behind = true
							if tc.frame != "" {
								select {
								case <-inbound:
								case <-done:
									return
								case <-stop:
									return
								}
								atRead := written.Load()
								margin.Store(written.Load() - atRead)
							}
						}
						c.Emit("large", payload)
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
			client := &http.Client{Timeout: waitFor}
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
				recv(t, upgraded, "Upgrade hook")
				require.True(t, conn.EngineConn().Upgraded())
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
							written.Add(1)
						}
					}
				}()
			}
			select {
			case err = <-failures:
				require.True(t, errors.Is(err, ErrWriteBufferFull), "unexpected error: %v", err)
				if tc.frame != "" {
					// The reader gets at least one client send period to reach the deadline
					// renewal that follows its read before the overflow closes the session.
					require.Positive(t, margin.Load(),
						"frames written between the read and the overflow emits")
				}
			case <-time.After(pingInterval + pingTimeout):
				t.Fatal("timed out waiting for write buffer overflow")
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

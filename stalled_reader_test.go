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
			// and a silent client times out after that window too. Every subtest awaits the
			// overflow for the same window, timed from after that deadline was set, so the
			// deadline can expire first only when the overflow takes about the whole window;
			// OnError then reports the i/o timeout, which fails the check, never passes it.
			const writeBuffer, pingInterval, pingTimeout = 64, waitFor, waitFor
			start, done := make(chan struct{}), make(chan struct{})
			var created atomic.Int64          // MESSAGE packets the session has started to write
			inbound := make(chan struct{}, 1) // a packet was read after start
			frames := newClientFrames()
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
					// overflow the queue wait, for a sending client, until the server has
					// read a packet since emission began and the client has then started
					// another frame. The client starts frames on a 100 ms ticker, so the
					// overflow follows the read frame by a later frame start, nominally one
					// send period, in which the reader can reach the deadline renewal after
					// its read; whether it does is not observed.
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
								if !frames.afterRead(done, stop) {
									return
								}
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
							if frames.write(func() error {
								if err := ws.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
									return err
								}
								return ws.WriteMessage(websocket.TextMessage, []byte(tc.frame))
							}) != nil {
								return
							}
						}
					}
				}()
			}
			select {
			case err = <-failures:
				require.True(t, errors.Is(err, ErrWriteBufferFull), "unexpected error: %v", err)
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

// clientFrames counts the frames the stalled-reader client writes and lets the emitter
// wait, after the server has read one of them, for the next frame. step and wait are seams
// for TestClientFramesAfterRead: step runs after each step of write, and wait blocks until
// woken, reporting false once done or stop is closed.
type clientFrames struct {
	written atomic.Int64
	wrote   chan struct{}
	step    func(string)
	wait    func(done, stop <-chan struct{}) bool
}

func newClientFrames() *clientFrames {
	f := &clientFrames{wrote: make(chan struct{}, 1)}
	f.wait = func(done, stop <-chan struct{}) bool {
		select {
		case <-f.wrote:
			return true
		case <-done:
			return false
		case <-stop:
			return false
		}
	}
	return f
}

func (f *clientFrames) note(step string) {
	if f.step != nil {
		f.step(step)
	}
}

// write counts one frame as started, wakes a waiting emitter, then sends the frame with
// send. Counting before sending means that every frame the server can have read is
// counted by the time the read is observed.
func (f *clientFrames) write(send func() error) error {
	f.written.Add(1)
	f.note("counted")
	select {
	case f.wrote <- struct{}{}:
	default:
	}
	f.note("signalled")
	if err := send(); err != nil {
		return err
	}
	f.note("sent")
	return nil
}

// afterRead is called once the server has read a client frame. It returns once the client
// has started a frame after that point, or false if done or stop is closed first. A
// wake-up only makes it check the count again, so a stale one cannot end the wait.
func (f *clientFrames) afterRead(done, stop <-chan struct{}) bool {
	atRead := f.written.Load()
	for f.written.Load() <= atRead {
		if !f.wait(done, stop) {
			return false
		}
	}
	return true
}

// TestClientFramesAfterRead: afterRead returns only after the client has started a frame
// after the read it follows, whatever the order of the read, the client's bookkeeping of
// the read frame and the emitter's wake-ups. The emitter observes the read at the named
// step of the client's write, or after the write if that step precedes the send.
func TestClientFramesAfterRead(t *testing.T) {
	for _, tc := range []struct{ name, observe string }{
		{"read-observed-before-the-frame-is-counted", "sent"},
		{"read-observed-before-the-frame-is-signalled", "counted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newClientFrames()
			waiting, resume := make(chan struct{}), make(chan bool)
			f.wait = func(_, _ <-chan struct{}) bool {
				waiting <- struct{}{}
				return <-resume
			}
			returned := make(chan bool, 1)
			started := false
			observe := func() { // the emitter sees the read and runs until it waits
				started = true
				go func() { returned <- f.afterRead(nil, nil) }()
				select {
				case <-waiting:
				case <-returned:
					t.Fatal("afterRead returned without waiting")
				}
			}
			sent := false
			f.step = func(step string) {
				sent = sent || step == "sent"
				if sent && !started && step == tc.observe {
					observe()
				}
			}
			send := func() error { return nil }
			require.NoError(t, f.write(send)) // the frame the server reads
			if !started {
				observe()
			}
			readFrames := f.written.Load()
			// A wake-up with no new frame, as the read frame's own signal can cause.
			resume <- true
			select {
			case <-waiting:
			case <-returned:
				t.Fatalf("afterRead returned with %d frames counted, all started before the read", readFrames)
			}
			f.step = nil
			require.NoError(t, f.write(send)) // a frame started after the read
			resume <- true
			for {
				select {
				case <-waiting:
					resume <- true
					continue
				case ok := <-returned:
					require.True(t, ok)
				}
				break
			}
		})
	}
}

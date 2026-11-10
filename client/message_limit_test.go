package client_test

import (
	"context"
	"fmt"
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// warnings is a slog.Handler that sends the message of each WARN record while it has room.
type warnings chan string

func (w warnings) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelWarn }
func (w warnings) WithAttrs([]slog.Attr) slog.Handler           { return w }
func (w warnings) WithGroup(string) slog.Handler                { return w }
func (w warnings) Handle(_ context.Context, r slog.Record) error {
	select {
	case w <- r.Message:
	default:
	}
	return nil
}

// A message past the limit ends the session with transport error and a WARN record.
func TestClientMessageLimitOption(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		for _, limit := range []int64{512, 4096, 0, -1} {
			t.Run(tr.Name()+"/"+fmt.Sprint(limit), func(t *testing.T) {
				payload := strings.Repeat("x", 2048)
				ts := newTestServer(t, func(s *socketio.Server) {
					s.OnConnect("/", func(socketio.Conn) error { return nil })
					s.OnEvent("/", "request", func(c socketio.Conn) { c.Emit("reply", payload) })
				})
				warns := make(warnings, 8)
				c, err := client.New(ts.URL, client.WithTransports(tr), client.WithReconnection(false), client.WithMaxMessageSize(1), client.WithMaxMessageSize(limit), client.WithLogger(slog.New(warns)))
				require.NoError(t, err)
				defer func() { _ = c.Close() }()
				replies, reasons := make(chan string, 1), make(chan string, 1)
				failures := make(chan error, 8)
				c.OnEvent("reply", func(_ client.Conn, s string) { replies <- s })
				c.OnError(func(_ client.Conn, err error) {
					select {
					case failures <- err:
					default:
					}
				})
				c.OnDisconnect(func(_ client.Conn, reason string) { reasons <- reason })
				require.NoError(t, c.Connect())
				c.Emit("request")
				select {
				case s := <-replies:
					require.NotEqual(t, int64(512), limit)
					require.Equal(t, payload, s)
				case reason := <-reasons:
					require.Equal(t, int64(512), limit, "unexpected disconnect: %s", reason)
					require.Equal(t, "transport error", reason)
					require.Empty(t, drainAll(failures), "the limit reached OnError") // reported before OnDisconnect
					require.Equal(t, []string{"socketio: message too large"}, drainAll(warns))
				case err := <-failures:
					t.Fatalf("OnError: %v", err)
				case <-time.After(3 * time.Second):
					t.Fatal("neither event nor size-limit disconnect delivered")
				}
			})
		}
	}
}

func drainAll[T any](ch chan T) (got []T) {
	for len(ch) > 0 {
		got = append(got, <-ch)
	}
	return got
}

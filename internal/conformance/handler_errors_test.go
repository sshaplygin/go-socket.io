//go:build conformance

package conformance

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/stretchr/testify/require"
)

// TestNodeHandlerErrorsAndPanics: a Node client receives the error results of
// untyped Go handlers as {code, message} ACK objects and a fixed text for
// recovered panics, while the server's OnError receives every error and panic.
func TestNodeHandlerErrorsAndPanics(t *testing.T) {
	for _, transport := range []string{"polling", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			server := socketio.NewServer()
			failure := errors.New("bad thing")
			returned, panics := make(chan error, 16), make(chan *socketio.PanicError, 16)
			report := func(_ socketio.Conn, err error) {
				var panicked *socketio.PanicError
				switch {
				case errors.As(err, &panicked):
					panics <- panicked
				case errors.Is(err, failure):
					returned <- err
				default:
					t.Logf("OnError: %v", err)
				}
			}
			ns := server.Of("/chat")
			ns.OnError(report)
			ns.OnConnect(func(c socketio.Conn) error {
				c.Use(func(event string, _ []interface{}) error {
					if event == "guarded" {
						panic("secret middleware value")
					}
					return nil
				})
				return nil
			})
			ns.OnEvent("fail", func(socketio.Conn) error { return failure })
			ns.OnEvent("ok", func(socketio.Conn) error { return nil })
			ns.OnEvent("pair", func(_ socketio.Conn, fail bool) (string, error) {
				if fail {
					return "", failure
				}
				return "done", nil
			})
			ns.OnEvent("guarded", func(socketio.Conn) {})
			ns.OnEvent("boom", func(socketio.Conn) { panic("secret event value") })
			require.NoError(t, socketio.NewAckEvent[int, int]("typed").Handle(ns,
				func(context.Context, socketio.Conn, int) (int, error) { panic("secret typed value") }))
			server.Of("/boom").OnError(report)
			server.Of("/boom").OnConnect(func(socketio.Conn) error { panic("secret connect value") })

			served := make(chan error, 1)
			go func() { served <- server.Serve() }()
			httpServer := httptest.NewServer(server)
			t.Cleanup(func() {
				closeErr := server.Close()
				httpServer.Close()
				require.NoError(t, closeErr)
				require.NoError(t, <-served)
			})

			runNode(t, "testdata/node/handler-errors.js", httpServer.URL, transport)

			for i := 0; i < 2; i++ { // "fail" and pair(true)
				select {
				case <-returned:
				case <-time.After(5 * time.Second):
					t.Fatalf("OnError received %d of 2 returned errors", i)
				}
			}
			values := map[interface{}]bool{}
			for i := 0; i < 4; i++ {
				select {
				case panicked := <-panics:
					require.NotEmpty(t, panicked.Stack)
					values[panicked.Value] = true
				case <-time.After(5 * time.Second):
					t.Fatalf("OnError received %d of 4 panics", i)
				}
			}
			require.Equal(t, map[interface{}]bool{"secret typed value": true, "secret middleware value": true,
				"secret connect value": true, "secret event value": true}, values)
		})
	}
}

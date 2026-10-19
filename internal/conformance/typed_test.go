//go:build conformance

package conformance

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/require"
)

type typedNodePayload struct {
	Number int64
	Binary parser.Buffer
	Nested map[string]parser.Buffer
}

func TestTypedNodeAcknowledgements(t *testing.T) {
	for _, transport := range []string{"polling", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			server := socketio.NewServer()
			ns := server.Of("/chat")
			ns.OnConnect(func(socketio.Conn) error { return nil })
			echo := socketio.NewAckEvent[typedNodePayload, typedNodePayload]("typed-echo")
			require.NoError(t, echo.Handle(ns, func(_ context.Context, _ socketio.Conn, value typedNodePayload) (typedNodePayload, error) {
				return value, nil
			}))
			rejected := socketio.NewAckEvent[string, string]("typed-rejected")
			require.NoError(t, rejected.Handle(ns, func(context.Context, socketio.Conn, string) (string, error) {
				return "", &socketio.RemoteError{Code: "refused", Message: "no"}
			}))
			question := socketio.NewAckEvent[typedNodePayload, typedNodePayload]("typed-question")
			done := make(chan error, 1)
			ns.OnEvent("typed-start", func(c socketio.Conn) {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					want := typedNodePayload{Number: 42, Binary: parser.Buffer{Data: []byte{0, 255}}, Nested: map[string]parser.Buffer{"bytes": {Data: []byte{1, 254}}}}
					got, err := question.EmitWithAck(ctx, c, want)
					if err == nil && (got.Number != 42 || string(got.Binary.Data) != string(want.Binary.Data) || string(got.Nested["bytes"].Data) != string(want.Nested["bytes"].Data)) {
						err = fmt.Errorf("unexpected typed Node ACK: %+v", got)
					}
					done <- err
					c.Emit("typed-finished")
				}()
			})
			served := make(chan error, 1)
			go func() { served <- server.Serve() }()
			httpServer := httptest.NewServer(server)
			t.Cleanup(func() {
				closeErr := server.Close()
				httpServer.Close()
				require.NoError(t, closeErr)
				require.NoError(t, <-served)
			})
			runNode(t, "testdata/node/typed.js", httpServer.URL, transport)
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("typed Node request did not finish")
			}
		})
	}
}

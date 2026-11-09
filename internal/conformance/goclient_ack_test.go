//go:build conformance

package conformance

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// TestGoClientAnswersUndecodableArguments: the reference server emits an event
// with an ACK callback to a Go client whose untyped handler cannot decode the
// arguments. The client answers with the error ACK, so the callback is called
// with {code, message}; the handler does not run and the connection stays usable.
func TestGoClientAnswersUndecodableArguments(t *testing.T) {
	for _, name := range []string{"polling", "websocket"} {
		t.Run(name, func(t *testing.T) {
			tr := transport.Transport(websocket.Default)
			if name == "polling" {
				tr = polling.Default
			}
			cl, err := client.New(goClientReference(t), client.WithPath("/client-engine"), client.WithTransports(tr))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			ran := make(chan struct{}, 1)
			failures := make(chan error, 1)
			cl.OnError(func(_ client.Conn, err error) { failures <- err })
			cl.OnEvent("num", func(_ client.Conn, _ int) { ran <- struct{}{} })
			require.NoError(t, cl.Connect())
			got := make(chan map[string]interface{}, 1)
			cl.Emit("ask-mismatch", func(value map[string]interface{}) { got <- value })
			value := goClientReceive(t, got, "ACK callback of the Node server")
			require.Equal(t, "invalid_payload", value["code"])
			require.Contains(t, value["message"], "cannot unmarshal string into Go value of type int")
			require.Error(t, goClientReceive(t, failures, "client error handler"))
			require.Empty(t, ran, "the handler must not run")
			echoed := make(chan string, 1)
			cl.Emit("echo", "still open", func(v string) { echoed <- v })
			require.Equal(t, "still open", goClientReceive(t, echoed, "ACK after the mismatch"))
		})
	}
}

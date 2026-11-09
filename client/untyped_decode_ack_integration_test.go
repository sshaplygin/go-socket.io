package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/parser"
)

// TestServerAckCallbackAnsweredWhenClientArgumentsDoNotDecode: a server emit with
// an ACK callback to a Go client whose untyped handler cannot decode the payload
// gets the error ACK instead of waiting for a reply that never comes.
func TestServerAckCallbackAnsweredWhenClientArgumentsDoNotDecode(t *testing.T) {
	conns := make(chan socketio.Conn, 1)
	ts := newTestServer(t, func(srv *socketio.Server) {
		srv.OnConnect("/", func(c socketio.Conn) error { conns <- c; return nil })
	})
	cl, err := client.New(ts.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	ran := make(chan string, 4)
	failures := make(chan error, 4)
	cl.OnError(func(_ client.Conn, err error) { failures <- err })
	cl.OnEvent("num", func(_ client.Conn, n int) string { ran <- "num"; return "num-ok" })
	require.NoError(t, cl.Connect())
	server := recv(t, conns, "server OnConnect")

	t.Run("callback receives the error ACK", func(t *testing.T) {
		got := make(chan map[string]interface{}, 1)
		server.Emit("num", "bad", func(v map[string]interface{}) { got <- v })
		select {
		case v := <-got:
			require.Equal(t, "invalid_payload", v["code"])
			require.Contains(t, v["message"], "cannot unmarshal string into Go value of type int")
		case <-time.After(2 * time.Second):
			t.Fatal("the ACK callback was not called")
		}
		require.Error(t, recv(t, failures, "client error handler"))
	})
	t.Run("binary attachment where a number is expected", func(t *testing.T) {
		got := make(chan map[string]interface{}, 1)
		server.Emit("num", parser.Buffer{Data: []byte("bytes")}, func(v map[string]interface{}) { got <- v })
		select {
		case v := <-got:
			require.Equal(t, "invalid_payload", v["code"])
		case <-time.After(2 * time.Second):
			t.Fatal("the ACK callback was not called")
		}
		require.Error(t, recv(t, failures, "client error handler"))
	})
	t.Run("typed emit returns the remote error", func(t *testing.T) {
		ev := socketio.NewAckEvent[string, string]("num")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := ev.EmitWithAck(ctx, server, "bad")
		var remote *socketio.RemoteError
		require.ErrorAs(t, err, &remote, "the wait must end with the peer's error, not a deadline")
		require.Equal(t, "invalid_payload", remote.Code)
		require.Error(t, recv(t, failures, "client error handler"))
	})
	t.Run("the connection stays open and decodable arguments are answered", func(t *testing.T) {
		got := make(chan string, 1)
		server.Emit("num", 3, func(v string) { got <- v })
		require.Equal(t, "num-ok", recv(t, got, "ACK"))
		require.Equal(t, "num", recv(t, ran, "handler"), "only the decodable event ran the handler")
		require.Empty(t, ran)
	})
}

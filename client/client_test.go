package client_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
)

func TestClientRejectsEmptyAddr(t *testing.T) {
	_, err := client.New("")
	require.ErrorIs(t, err, client.ErrEmptyAddr)
	_, err = client.New("http://[::1")
	require.Error(t, err)
}

func TestClientPackageRoundTrip(t *testing.T) {
	disconnected := make(chan string, 1)
	ts := newTestServer(t, func(s *socketio.Server) {
		s.OnConnect("/", func(socketio.Conn) error { return nil })
		s.OnEvent("/", "echo", func(_ socketio.Conn, value string) string { return value })
		s.OnDisconnect("/", func(_ socketio.Conn, reason string) { disconnected <- reason })
	})
	cl, err := client.New(ts.URL)
	require.NoError(t, err)
	cl.OnConnect(func(client.Conn) error { return nil })
	require.NoError(t, cl.Connect())
	replies := make(chan string, 1)
	cl.Emit("echo", "wrapper", func(value string) { replies <- value })
	require.Equal(t, "wrapper", recv(t, replies, "wrapper event ACK"))
	require.NoError(t, cl.Close())
	require.NotEmpty(t, recv(t, disconnected, "server OnDisconnect after wrapper Close"))
}

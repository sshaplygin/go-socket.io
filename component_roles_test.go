package socketio

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
)

func TestClientPackageRoundTrip(t *testing.T) {
	disconnected := make(chan string, 1)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnEvent("/", "echo", func(_ Conn, value string) string { return value })
		s.OnDisconnect("/", func(_ Conn, reason string) { disconnected <- reason })
	})
	cl, err := client.New(ts.URL, nil)
	require.NoError(t, err)
	cl.OnConnect(func(client.Conn) error { return nil })
	require.NoError(t, cl.Connect())
	replies := make(chan string, 1)
	cl.Emit("echo", "wrapper", func(value string) { replies <- value })
	require.Equal(t, "wrapper", recv(t, replies, "wrapper event ACK"))
	require.NoError(t, cl.Close())
	require.NotEmpty(t, recv(t, disconnected, "server OnDisconnect after wrapper Close"))
}

func TestRoleSpecificHandlers(t *testing.T) {
	serverConns := make(chan Conn, 1)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(c Conn) error { serverConns <- c; return nil })
		s.OnEvent("/", "root-handler", func(_ Conn, v string) string { return "root:" + v })
		s.OnEvent("/", "leaf-handler", func(_ Conn, v string) string { return "leaf:" + v })
	})
	cl, err := client.New(ts.URL, nil)
	require.NoError(t, err)
	cl.OnConnect(func(client.Conn) error { return nil })
	cl.OnEvent("root-handler", func(_ client.Conn, v string) string { return "root:" + v })
	cl.OnEvent("leaf-handler", func(_ client.Conn, v string) string { return "leaf:" + v })
	require.NoError(t, cl.Connect())
	t.Cleanup(func() { require.NoError(t, cl.Close()) })
	serverConn := recv(t, serverConns, "server alias handler")
	for _, emit := range []func(string, ...interface{}){cl.Emit, serverConn.Emit} {
		for _, kind := range []string{"root", "leaf"} {
			replies := make(chan string, 1)
			emit(kind+"-handler", "alias", func(value string) { replies <- value })
			require.Equal(t, kind+":alias", recv(t, replies, "ACK through alias handler"))
		}
	}
}

func TestClientErrorIdentity(t *testing.T) {
	_, err := client.New("", nil)
	require.ErrorIs(t, err, client.ErrEmptyAddr)
	for _, pair := range [][2]error{{ErrWriteBufferFull, client.ErrWriteBufferFull}, {client.ErrWriteBufferFull, ErrWriteBufferFull}} {
		wrapped := fmt.Errorf("wrapped: %w", pair[0])
		require.True(t, errors.Is(wrapped, pair[0]))
		require.False(t, errors.Is(wrapped, pair[1]))
	}
}
func TestEventHandlerRejectsClientConn(t *testing.T) {
	s := NewServer(nil)
	t.Cleanup(func() { _ = s.Close() })
	require.Panics(t, func() { s.OnEvent("/", "wrong-role", func(client.Conn) {}) })
}

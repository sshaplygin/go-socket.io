package socketio

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
)

func TestDeprecatedClientRoundTrip(t *testing.T) {
	disconnected := make(chan string, 1)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnEvent("/", "echo", func(_ Conn, value string) string { return value })
		s.OnDisconnect("/", func(_ Conn, reason string) { disconnected <- reason })
	})
	cl, err := NewClient(ts.URL, nil)
	require.NoError(t, err)
	cl.OnConnect(func(Conn) error { return nil })
	require.NoError(t, cl.Connect())
	replies := make(chan string, 1)
	cl.Emit("echo", "wrapper", func(value string) { replies <- value })
	require.Equal(t, "wrapper", recv(t, replies, "wrapper event ACK"))
	require.NoError(t, cl.Close())
	require.NotEmpty(t, recv(t, disconnected, "server OnDisconnect after wrapper Close"))
}

func TestConnAliasHandlers(t *testing.T) {
	serverConns := make(chan client.Conn, 1)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(c client.Conn) error { serverConns <- c; return nil })
		s.OnEvent("/", "root-handler", func(_ Conn, v string) string { return "root:" + v })
		s.OnEvent("/", "leaf-handler", func(_ client.Conn, v string) string { return "leaf:" + v })
	})
	cl, err := client.NewClient(ts.URL, nil)
	require.NoError(t, err)
	cl.OnConnect(func(Conn) error { return nil })
	cl.OnEvent("root-handler", func(_ Conn, v string) string { return "root:" + v })
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
	_, rootErr := NewClient("", nil)
	_, leafErr := client.NewClient("", nil)
	for _, err := range []error{rootErr, leafErr} {
		for _, target := range []error{ErrEmptyAddr, EmptyAddrErr, client.ErrEmptyAddr} {
			require.True(t, errors.Is(err, target))
		}
	}
	for _, pair := range [][2]error{{ErrWriteBufferFull, client.ErrWriteBufferFull}, {ErrServerOnly, client.ErrServerOnly}} {
		require.ErrorIs(t, fmt.Errorf("wrapped: %w", pair[0]), pair[1])
	}
}

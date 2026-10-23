package socketio

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnErrorPanicIsLogged(t *testing.T) {
	rec, _ := captureLogs(t, nil)
	server := NewServer(WithLogger(slog.New(rec)))
	t.Cleanup(func() { _ = server.Close() })
	refused := errors.New("connect refused")
	server.OnConnect("/", func(Conn) error { return refused })
	called := false
	server.OnError("/", func(c Conn, err error) {
		if !errors.Is(err, refused) {
			return // Ignore transport cleanup after the rejected connection.
		}
		require.Nil(t, c)
		require.ErrorIs(t, err, refused)
		called = true
		panic("error callback failed")
	})
	server.serveConn(newFakeConn(t))
	require.True(t, called)
	records := only(rec.since(0), "socketio: error handler panic")
	require.Len(t, records, 1)
	require.Equal(t, "WARN", records[0]["level"])
	require.Equal(t, "/", records[0]["nsp"])
	require.Equal(t, "error callback failed", records[0]["err"])
}

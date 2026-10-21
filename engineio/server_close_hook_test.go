package engineio_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

func TestServerCloseFromErrorHookDuringShutdown(t *testing.T) {
	var server *engineio.Server
	hookReturned := make(chan error, 1)
	server = engineio.NewServer(engineio.WithHooks(&engineio.Hooks{
		Error: func(engineio.Conn, error) { hookReturned <- server.Close() },
	}))
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	defer func() { _ = ws.Close() }()
	_, _, err = ws.ReadMessage() // OPEN
	require.NoError(t, err)
	conn, err := server.Accept()
	require.NoError(t, err)

	// Flush a non-final frame, then truncate the message. Reading its body
	// records a transport error that the subsequent shutdown reports to Error.
	writer, err := ws.NextWriter(websocket.TextMessage)
	require.NoError(t, err)
	_, err = writer.Write([]byte("4" + strings.Repeat("x", 8192)))
	require.NoError(t, err)
	_, reader, err := conn.NextReader()
	require.NoError(t, err)
	require.NoError(t, ws.UnderlyingConn().Close())
	_, err = io.ReadAll(reader)
	require.Error(t, err)
	_ = reader.Close()

	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	select {
	case err := <-hookReturned:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Error hook blocked reentering Server.Close during shutdown")
	}
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Server.Close did not finish after Error returned")
	}
	_, err = server.Accept()
	require.ErrorIs(t, err, io.EOF)
	require.NoError(t, server.Close())
}

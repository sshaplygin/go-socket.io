package engineio_test

import (
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

func TestServerCloseShutsDownSessionsConcurrently(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	server := engineio.NewServer(engineio.WithHooks(&engineio.Hooks{
		Error: func(engineio.Conn, error) { close(entered); <-release },
	}))
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	t.Cleanup(func() { unblock(); _ = server.Close() })
	dial := func() (*websocket.Conn, engineio.Conn) {
		t.Helper()
		ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+"/?EIO=3&transport=websocket", nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = ws.Close() })
		require.NoError(t, ws.SetReadDeadline(time.Now().Add(time.Second)))
		_, _, err = ws.ReadMessage()
		require.NoError(t, err)
		conn, err := server.Accept()
		require.NoError(t, err)
		return ws, conn
	}
	ws, conn := dial()

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
	var clients []*websocket.Conn
	for i := 0; i < 16; i++ {
		client, _ := dial()
		clients = append(clients, client)
	}
	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not enter the failing session's Error hook")
	}
	for i, client := range clients {
		require.NoError(t, client.SetReadDeadline(time.Now().Add(time.Second)))
		_, _, err := client.ReadMessage()
		require.Error(t, err, "client %d remained open", i)
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatalf("client %d was not shut down while the Error hook was blocked: %v", i, err)
		}
	}
	unblock()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after releasing the Error hook")
	}
}

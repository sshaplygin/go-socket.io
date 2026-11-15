package socketio

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/parser"
)

// socket.io-client 1.0–1.3 under Node opens the websocket without b64 and sends
// a binary attachment as a base64 text message. The event reaches its handler,
// and the server's binary ACK is written as a binary message.
func TestBinaryEventInBase64WebsocketMessage(t *testing.T) {
	got := make(chan []byte, 1)
	s := NewServer()
	s.OnEvent("/", "bin", func(_ Conn, v *parser.Buffer) *parser.Buffer {
		got <- v.Data
		return v
	})
	go func() { _ = s.Serve() }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
	for k := 0; k < 2; k++ { // Engine.IO OPEN and Socket.IO CONNECT
		_, _, err = c.ReadMessage()
		require.NoError(t, err)
	}

	// The frames socket.io-client 1.3.7 writes for emit('bin', Buffer.from([1, 2, 3, 4, 5]), ack).
	require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte(`451-0["bin",{"_placeholder":true,"num":0}]`)))
	require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("b4AQIDBAU=")))
	require.Equal(t, []byte{1, 2, 3, 4, 5}, recv(t, got, "binary event"))

	typ, ack, err := c.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, gorilla.TextMessage, typ)
	require.Equal(t, `461-0[{"_placeholder":true,"num":0}]`, strings.TrimSpace(string(ack)))
	typ, attachment, err := c.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, gorilla.BinaryMessage, typ)
	require.Equal(t, []byte{4, 1, 2, 3, 4, 5}, attachment)
}

// An attachment message that does not decode ends the session.
func TestBinaryEventInvalidBase64EndsSession(t *testing.T) {
	for _, attachment := range []string{"b4AQ", "b4%%%", "b", "b9AQI="} {
		t.Run(attachment, func(t *testing.T) {
			disconnected := make(chan string, 1)
			s := NewServer()
			s.OnEvent("/", "bin", func(Conn, *parser.Buffer) { t.Error("handler ran") })
			s.OnDisconnect("/", func(_ Conn, reason string) { disconnected <- reason })
			go func() { _ = s.Serve() }()
			ts := httptest.NewServer(s)
			t.Cleanup(func() { _ = s.Close(); ts.Close() })
			c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = c.Close() })
			require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
			for k := 0; k < 2; k++ { // Engine.IO OPEN and Socket.IO CONNECT
				_, _, err = c.ReadMessage()
				require.NoError(t, err)
			}
			require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte(`451-0["bin",{"_placeholder":true,"num":0}]`)))
			require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte(attachment)))
			require.Equal(t, "transport error", recv(t, disconnected, "OnDisconnect"))
		})
	}
}

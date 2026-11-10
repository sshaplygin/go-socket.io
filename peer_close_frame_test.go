package socketio

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// A client that closes its websocket with a close frame, and sends no Socket.IO
// DISCONNECT before it, is reported to OnDisconnect as "transport close" for
// status 1000, 1001 or none (1005), as socket.io 2.5.0 reports it, and as
// "transport error" for a status that signals a failure.
func TestOnDisconnectPeerCloseFrame(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload []byte
		want    string
	}{
		{"normal closure", gorilla.FormatCloseMessage(gorilla.CloseNormalClosure, ""), "transport close"},
		{"going away", gorilla.FormatCloseMessage(gorilla.CloseGoingAway, ""), "transport close"},
		{"no status", nil, "transport close"},
		{"internal error", gorilla.FormatCloseMessage(gorilla.CloseInternalServerErr, ""), "transport error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			disconnected := make(chan string, 1)
			s := NewServer()
			s.OnConnect("/", func(Conn) error { return nil })
			s.OnDisconnect("/", func(_ Conn, reason string) { disconnected <- reason })
			s.OnError("/", func(Conn, error) {})
			go func() { _ = s.Serve() }()
			ts := httptest.NewServer(s)
			defer ts.Close()
			defer func() { _ = s.Close() }()
			c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
			require.NoError(t, err)
			defer func() { _ = c.Close() }()
			// Consume Engine.IO OPEN and Socket.IO CONNECT before the close.
			_, _, err = c.ReadMessage()
			require.NoError(t, err)
			_, data, err := c.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, "40", string(data))
			require.NoError(t, c.WriteControl(gorilla.CloseMessage, tc.payload, time.Now().Add(time.Second)))
			require.Equal(t, tc.want, recv(t, disconnected, "OnDisconnect reason"))
		})
	}
}

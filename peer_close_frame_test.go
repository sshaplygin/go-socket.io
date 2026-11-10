package socketio

import (
	"io"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

// peerCloseFrames are client close frames; the first three are orderly.
var peerCloseFrames = []struct {
	name    string
	payload []byte
	want    string
}{
	{"normal closure", gorilla.FormatCloseMessage(gorilla.CloseNormalClosure, ""), "transport close"},
	{"going away", gorilla.FormatCloseMessage(gorilla.CloseGoingAway, ""), "transport close"},
	{"no status", nil, "transport close"},
	{"internal error", gorilla.FormatCloseMessage(gorilla.CloseInternalServerErr, ""), "transport error"},
}

// A client that closes its websocket with a close frame, and sends no Socket.IO
// DISCONNECT before it, is reported to OnDisconnect as "transport close" for
// status 1000, 1001 or none (1005), as socket.io 2.5.0 reports it, and as
// "transport error" for a status that signals a failure.
func TestOnDisconnectPeerCloseFrame(t *testing.T) {
	for _, tc := range peerCloseFrames {
		t.Run(tc.name, func(t *testing.T) {
			disconnected := make(chan string, 1)
			s := NewServer()
			s.OnConnect("/", func(Conn) error { return nil })
			s.OnDisconnect("/", func(_ Conn, reason string) { disconnected <- reason })
			s.OnError("/", func(Conn, error) {})
			go func() { _ = s.Serve() }()
			ts := httptest.NewServer(s)
			defer func() { _ = s.Close(); ts.Close() }()
			c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
			require.NoError(t, err)
			defer func() { _ = c.Close() }()
			for k := 0; k < 2; k++ { // Engine.IO OPEN and Socket.IO CONNECT
				_, _, err = c.ReadMessage()
				require.NoError(t, err)
			}
			require.NoError(t, c.WriteControl(gorilla.CloseMessage, tc.payload, time.Now().Add(time.Second)))
			require.Equal(t, tc.want, recv(t, disconnected, "OnDisconnect reason"))
		})
	}
}

// The reason is the same while the server emits: an Emit whose write fails after
// the reply to the client's close frame does not make it a transport error.
func TestOnDisconnectPeerCloseFrameWhileEmitting(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("the write must run in parallel with the reader that receives the close frame")
	}
	const sessions = 200
	disconnected := make(chan string, 1)
	s := NewServer(WithWriteBufferSize(1 << 20)) // no queue overflow when the client lags
	s.OnConnect("/", func(c Conn) error {
		go func() {
			for j := 0; j < 2000 && c.EngineConn().ReadyState() == engineio.StateOpen; j++ { // no backlog
				c.Emit("tick", j)
				time.Sleep(20 * time.Microsecond)
			}
		}()
		return nil
	})
	s.OnDisconnect("/", func(_ Conn, reason string) { disconnected <- reason })
	s.OnError("/", func(Conn, error) {})
	go func() { _ = s.Serve() }()
	ts := httptest.NewServer(s)
	defer func() { _ = s.Close(); ts.Close() }()
	reasons := map[string]int{}
	for i := 0; i < sessions; i++ {
		c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
		require.NoError(t, err)
		for k := 0; k < 3; k++ { // OPEN, CONNECT and a tick: the emitter is running
			_, _, err = c.ReadMessage()
			require.NoError(t, err)
		}
		go func() { _, _ = io.Copy(io.Discard, c.UnderlyingConn()) }()
		require.NoError(t, c.WriteControl(gorilla.CloseMessage, peerCloseFrames[i%3].payload, time.Now().Add(time.Second)))
		reasons[recv(t, disconnected, "OnDisconnect reason")]++
		_ = c.Close()
	}
	require.Equal(t, map[string]int{"transport close": sessions}, reasons)
}

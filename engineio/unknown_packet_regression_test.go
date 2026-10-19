package engineio_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
)

// Covers G26
func TestReviewG26UnknownPacketClosesSession(t *testing.T) {
	s := engineio.NewServer()
	ts := httptest.NewServer(s)
	defer ts.Close()
	defer func() { _ = s.Close() }()
	result := make(chan error, 1)
	go func() {
		c, err := s.Accept()
		if err == nil {
			_, r, e := c.NextReader()
			err = e
			if r != nil {
				_ = r.Close()
			}
		}
		result <- err
	}()
	ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	defer func() { _ = ws.Close() }()
	_, _, err = ws.ReadMessage()
	require.NoError(t, err)
	require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("9")))
	require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("4alive")))
	select {
	case err := <-result:
		require.Error(t, err, "invalid type 9 was skipped and following MESSAGE delivered")
	case <-time.After(time.Second):
		t.Fatal("session did not handle input")
	}
}

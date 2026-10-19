package engineio_test

import (
	gorilla "github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Covers G1
func TestDuplicateWebsocketSessionRequestReturns(t *testing.T) {
	s := engineio.NewServer()
	defer func() { _ = s.Close() }()
	ts := httptest.NewServer(s)
	defer func() { _ = ts.Close() }()
	defer func() { _ = s.Close() }()
	accepted := make(chan engineio.Conn, 1)
	go func() {
		c, err := s.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	defer func() { _ = ws.Close() }()
	_, _, err = ws.ReadMessage()
	require.NoError(t, err)
	var c engineio.Conn
	select {
	case c = <-accepted:
	case <-time.After(time.Second):
		t.Fatal("no session")
	}
	client := &http.Client{Timeout: 250 * time.Millisecond}
	resp, err := client.Get(ts.URL + "/?EIO=3&transport=websocket&sid=" + c.ID())
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	_, resp, err = gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket&sid="+c.ID(), nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, engineio.StateOpen, c.ReadyState())
}

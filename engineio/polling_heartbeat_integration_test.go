package engineio_test

import (
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPingPostReleasesBeforePongPoll(t *testing.T) {
	s, release := newHeldOpenServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()
	defer func() { _ = s.Close() }()
	defer release()
	accepted := make(chan engineio.Conn, 1)
	go func() {
		c, e := s.Accept()
		if e == nil {
			accepted <- c
			_, _, _ = c.NextReader()
		}
	}()
	resp, err := http.Get(ts.URL + "/?EIO=3&transport=polling&b64=1")
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	// Accept returns once the session is open; Clients() may not list it yet.
	release()
	var sid string
	select {
	case c := <-accepted:
		sid = c.ID()
	case <-time.After(5 * time.Second):
		t.Fatal("session was not accepted after the handshake")
	}
	require.NotEmpty(t, sid)
	url := ts.URL + "/?EIO=3&transport=polling&b64=1&sid=" + sid
	client := &http.Client{Timeout: time.Second}
	resp, err = client.Post(url, "text/plain;charset=UTF-8", strings.NewReader("6:2probe"))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
	resp, err = client.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "6:3probe", string(body))
}

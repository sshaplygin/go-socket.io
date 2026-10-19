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

// Covers: G10
func TestPingPostReleasesBeforePongPoll(t *testing.T) {
	s := engineio.NewServer()
	ts := httptest.NewServer(s)
	defer ts.Close()
	defer s.Close()
	go func() {
		c, e := s.Accept()
		if e == nil {
			_, _, _ = c.NextReader()
		}
	}()
	resp, err := http.Get(ts.URL + "/?EIO=3&transport=polling&b64=1")
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	resp.Body.Close()
	var sid string
	for id := range s.Clients() {
		sid = id
	}
	require.NotEmpty(t, sid)
	url := ts.URL + "/?EIO=3&transport=polling&b64=1&sid=" + sid
	client := &http.Client{Timeout: time.Second}
	resp, err = client.Post(url, "text/plain;charset=UTF-8", strings.NewReader("6:2probe"))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
	resp, err = client.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "6:3probe", string(body))
}

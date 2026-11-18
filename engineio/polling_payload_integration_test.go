package engineio_test

import (
	"bytes"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEmptyBinaryPostKeepsSession(t *testing.T) {
	s, release := newHeldOpenServer(t)
	ts := httptest.NewServer(s)
	defer ts.Close()
	defer func() { _ = s.Close() }()
	defer release()
	done := make(chan string, 1)
	accepted := make(chan engineio.Conn, 1)
	go func() {
		c, err := s.Accept()
		if err == nil {
			accepted <- c
			_, r, readErr := c.NextReader()
			if readErr != nil {
				done <- readErr.Error()
				return
			}
			body, readErr := io.ReadAll(r)
			_ = r.Close()
			if readErr != nil {
				done <- readErr.Error()
				return
			}
			done <- string(body)
		}
	}()
	resp, err := http.Get(ts.URL + "/?EIO=3&transport=polling")
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	require.NoError(t, err)
	// Accept returns once the handshake has finished.
	release()
	var sid string
	select {
	case c := <-accepted:
		sid = c.ID()
	case <-time.After(5 * time.Second):
		t.Fatal("session was not accepted after the handshake")
	}
	require.NotEmpty(t, sid)
	resp, err = http.Post(ts.URL+"/?EIO=3&transport=polling&sid="+sid, "application/octet-stream", strings.NewReader(""))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode, "reference treats empty binary payload as no packets")
	require.NoError(t, resp.Body.Close())
	resp, err = http.Post(ts.URL+"/?EIO=3&transport=polling&sid="+sid, "application/octet-stream", bytes.NewReader([]byte{0, 2, 255, '4', 'x'}))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	select {
	case body := <-done:
		require.Equal(t, "x", body)
	case <-time.After(time.Second):
		t.Fatal("session did not deliver the message after an empty POST")
	}
}

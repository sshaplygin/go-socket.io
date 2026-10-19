package engineio_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
)

// Covers G27
func TestReviewG27EmptyBinaryPostKeepsSession(t *testing.T) {
	s := engineio.NewServer()
	ts := httptest.NewServer(s)
	defer ts.Close()
	defer s.Close()
	done := make(chan string, 1)
	go func() {
		c, err := s.Accept()
		if err == nil {
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
	var sid string
	for id := range s.Clients() {
		sid = id
	}
	require.NotEmpty(t, sid)
	resp, err = http.Post(ts.URL+"/?EIO=3&transport=polling&sid="+sid, "application/octet-stream", strings.NewReader(""))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "reference treats empty binary payload as no packets")
	require.NoError(t, resp.Body.Close())
	resp, err = http.Post(ts.URL+"/?EIO=3&transport=polling&sid="+sid, "application/octet-stream", bytes.NewReader([]byte{0, 2, 255, '4', 'x'}))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	select {
	case body := <-done:
		require.Equal(t, "x", body)
	case <-time.After(time.Second):
		t.Fatal("session did not deliver the message after an empty POST")
	}
}

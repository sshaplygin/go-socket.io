package engineio_test

import (
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
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := s.Accept()
		if err == nil {
			_, r, _ := c.NextReader()
			if r != nil {
				_ = r.Close()
			}
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
	select {
	case <-done:
		t.Error("empty POST ended session reader")
	case <-time.After(100 * time.Millisecond):
	}
}

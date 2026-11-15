package engineio_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

type overlapResponse struct {
	status int
	body   string
	err    error
}

func overlapRequest(method, url string, body io.Reader) overlapResponse {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return overlapResponse{err: err}
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return overlapResponse{err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	return overlapResponse{status: resp.StatusCode, body: string(data), err: err}
}

// overlapAsync runs the request on its own goroutine; a request the server holds
// fails the test through awaitOverlap instead of hanging it.
func overlapAsync(method, url string, body io.Reader) <-chan overlapResponse {
	done := make(chan overlapResponse, 1)
	go func() { done <- overlapRequest(method, url, body) }()
	return done
}

func awaitOverlap(t *testing.T, done <-chan overlapResponse, what string) overlapResponse {
	t.Helper()
	select {
	case r := <-done:
		require.NoError(t, r.err, what)
		return r
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not finish", what)
		return overlapResponse{}
	}
}

// openOverlapSession opens a text polling session and returns its URL and the
// accepted server connection.
func openOverlapSession(t *testing.T) (string, engineio.Conn) {
	t.Helper()
	s := engineio.NewServer()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	open := overlapRequest(http.MethodGet, ts.URL+"/?EIO=3&transport=polling&b64=1", nil)
	require.NoError(t, open.err)
	require.Equal(t, http.StatusOK, open.status, open.body)
	var size int
	_, err := fmt.Sscanf(open.body, "%d:", &size)
	require.NoError(t, err, "%q", open.body)
	packet := open.body[strings.IndexByte(open.body, ':')+1:][:size]
	require.True(t, strings.HasPrefix(packet, "0"), "%q", open.body)
	var handshake struct {
		SID string `json:"sid"`
	}
	require.NoError(t, json.Unmarshal([]byte(packet[1:]), &handshake))
	c, err := s.Accept()
	require.NoError(t, err)
	return ts.URL + "/?EIO=3&transport=polling&b64=1&sid=" + handshake.SID, c
}

// TestPollingOverlaps checks that a polling request overlapping one of the same
// kind is refused without disturbing the first: a second GET while the first is
// being written gets 500, a second POST while the first is being read gets 400,
// both with "overlap", and the first request completes.
func TestPollingOverlaps(t *testing.T) {
	t.Run("poll", func(t *testing.T) {
		poll, c := openOverlapSession(t)
		first := overlapAsync(http.MethodGet, poll, nil)
		w, err := c.NextWriter(eioparser.String)
		require.NoError(t, err) // the first GET has acquired its writer
		_, err = w.Write([]byte("first"))
		require.NoError(t, err)
		second := awaitOverlap(t, overlapAsync(http.MethodGet, poll, nil), "the overlapping GET")
		require.Equal(t, http.StatusInternalServerError, second.status)
		require.Contains(t, second.body, "overlap")
		require.NoError(t, w.Close())
		result := awaitOverlap(t, first, "the first GET")
		require.Equal(t, http.StatusOK, result.status)
		require.Equal(t, "6:4first", result.body)
	})
	t.Run("data", func(t *testing.T) {
		poll, c := openOverlapSession(t)
		body, feed := io.Pipe()
		t.Cleanup(func() { _ = feed.Close(); _ = body.Close() })
		first := overlapAsync(http.MethodPost, poll, body)
		_, err := feed.Write([]byte("5:4"))
		require.NoError(t, err)
		_, r, err := c.NextReader()
		require.NoError(t, err) // the first POST is being read: its packet header has arrived
		second := awaitOverlap(t, overlapAsync(http.MethodPost, poll, strings.NewReader("2:4x")), "the overlapping POST")
		require.Equal(t, http.StatusBadRequest, second.status)
		require.Contains(t, second.body, "overlap")
		_, err = feed.Write([]byte("data"))
		require.NoError(t, err)
		require.NoError(t, feed.Close())
		data, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		require.Equal(t, "data", string(data))
		result := awaitOverlap(t, first, "the first POST")
		require.Equal(t, http.StatusOK, result.status)
		require.Equal(t, "ok", result.body)
	})
}

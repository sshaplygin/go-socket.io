package polling

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/stretchr/testify/require"
)

func TestResponseBufferCacheRetentionAndReuse(t *testing.T) {
	cache := newResponseBufferCache()
	large := make([]byte, 512*1024)
	long := bytes.NewBuffer(cache.take())
	_, err := long.Write(large)
	require.NoError(t, err)
	want := &long.Bytes()[0]
	cache.put(long.Bytes())
	short := bytes.NewBuffer(cache.take())
	require.True(t, want == &short.Bytes()[:cap(short.Bytes())][0], "the large echo buffer should be reused")
	_, err = short.WriteString("short")
	require.NoError(t, err)
	require.Equal(t, "short", short.String(), "the earlier response must not leak into the next")
	cache.put(short.Bytes())

	cache.put(make([]byte, maxResponseBufferSize+1))
	require.Len(t, cache.free, 1, "buffers larger than 2 MiB must be dropped")

	var concurrent [16][]byte
	for i := range concurrent {
		concurrent[i] = make([]byte, 1024+i)
	}
	var wg sync.WaitGroup
	for _, b := range concurrent {
		wg.Add(1)
		go func(b []byte) { defer wg.Done(); cache.put(b) }(b)
	}
	wg.Wait()
	require.Len(t, cache.free, maxRetainedResponseBuffers)
	for i := 0; i < maxRetainedResponseBuffers; i++ {
		b := <-cache.free
		require.LessOrEqual(t, cap(b), maxResponseBufferSize)
		require.Len(t, b, 0)
	}
}

func TestPollingGETShorterAfterLongerHasNoStaleBytes(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost/?b64=1", nil)
	c := newServerConn(Default, req)
	defer func() { require.NoError(t, c.Close()) }()
	require.NoError(t, c.SetWriteDeadline(time.Now().Add(10*time.Second)))
	for _, body := range []string{strings.Repeat("L", 512*1024), "short"} {
		got, err := serveOneBufferedPacket(c, req, body)
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("%d:4%s", len(body)+1, body), got)
	}
}

func TestPollingGETLargeResponseDoesNotRetainOversizedBuffer(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost/?b64=1", nil)
	c := newServerConn(Default, req)
	defer func() { require.NoError(t, c.Close()) }()
	require.NoError(t, c.SetWriteDeadline(time.Now().Add(10*time.Second)))
	body := strings.Repeat("L", maxResponseBufferSize+1)
	got, err := serveOneBufferedPacket(c, req, body)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%d:4%s", len(body)+1, body), got)
	requireBoundedResponseBuffers(t)
}

func TestPollingGETConcurrentSessionsReuseBuffers(t *testing.T) {
	const sessions, polls = 16, 8
	var wg sync.WaitGroup
	errs := make(chan error, sessions)
	for session := 0; session < sessions; session++ {
		wg.Add(1)
		go func(session int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "http://localhost/?b64=1", nil)
			c := newServerConn(Default, req)
			defer c.Close()
			if err := c.SetWriteDeadline(time.Now().Add(15 * time.Second)); err != nil {
				errs <- err
				return
			}
			for poll := 0; poll < polls; poll++ {
				body := fmt.Sprintf("session=%d poll=%d %s", session, poll, strings.Repeat(string(rune('a'+session)), 2048+poll))
				got, err := serveOneBufferedPacket(c, req, body)
				if err != nil {
					errs <- err
					return
				}
				want := fmt.Sprintf("%d:4%s", len(body)+1, body)
				if got != want {
					errs <- fmt.Errorf("session %d poll %d: response mismatch", session, poll)
					return
				}
			}
		}(session)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	requireBoundedResponseBuffers(t)
}

func requireBoundedResponseBuffers(t *testing.T) {
	t.Helper()
	n := len(responseBuffers.free)
	require.LessOrEqual(t, n, maxRetainedResponseBuffers)
	retained := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		b := <-responseBuffers.free
		retained = append(retained, b)
		require.LessOrEqual(t, cap(b), maxResponseBufferSize)
	}
	for _, b := range retained {
		responseBuffers.put(b)
	}
}

func serveOneBufferedPacket(c *serverConn, req *http.Request, body string) (string, error) {
	written := make(chan error, 1)
	go func() {
		w, err := c.NextWriter(eioparser.String, eioparser.MESSAGE)
		if err == nil {
			_, err = io.WriteString(w, body)
			if err == nil {
				err = w.Close()
			}
		}
		written <- err
	}()
	response := httptest.NewRecorder()
	c.ServeHTTP(response, req)
	if err := <-written; err != nil {
		return "", err
	}
	if response.Code != 200 {
		return "", fmt.Errorf("unexpected GET status: %d: %s", response.Code, response.Body.String())
	}
	return response.Body.String(), nil
}

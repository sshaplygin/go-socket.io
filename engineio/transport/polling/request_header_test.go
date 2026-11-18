package polling

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// recordedRequest is the part of a client request that a header test checks.
type recordedRequest struct {
	method string
	sid    string
	header http.Header
}

// headerRecorder is an Engine.IO polling endpoint that records every request.
// It answers the handshake with OPEN, holds polls open until the client
// cancels them, and accepts POSTs.
type headerRecorder struct {
	open []byte

	mu       sync.Mutex
	requests []recordedRequest
	changed  chan struct{}
}

func newHeaderRecorder(t *testing.T) *headerRecorder {
	t.Helper()
	cp := eioparser.ConnParameters{
		PingInterval: time.Minute,
		PingTimeout:  time.Minute,
		SID:          "sid1",
		Upgrades:     []string{},
	}
	var buf bytes.Buffer
	_, err := cp.WriteTo(&buf)
	require.NoError(t, err)
	return &headerRecorder{
		open:    []byte(fmt.Sprintf("%d:0%s", buf.Len()+1, buf.String())),
		changed: make(chan struct{}, 1),
	}
}

func (h *headerRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("sid")
	h.mu.Lock()
	h.requests = append(h.requests, recordedRequest{method: r.Method, sid: sid, header: r.Header.Clone()})
	h.mu.Unlock()
	select {
	case h.changed <- struct{}{}:
	default:
	}

	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	switch {
	case r.Method == http.MethodGet && sid == "":
		_, _ = w.Write(h.open)
	case r.Method == http.MethodGet:
		<-r.Context().Done()
	default:
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte("ok"))
	}
}

// waitFor waits until the recorded requests satisfy done and returns them.
func (h *headerRecorder) waitFor(t *testing.T, done func([]recordedRequest) bool) []recordedRequest {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		h.mu.Lock()
		requests := append([]recordedRequest(nil), h.requests...)
		h.mu.Unlock()
		if done(requests) {
			return requests
		}
		select {
		case <-h.changed:
		case <-deadline:
			t.Fatalf("timed out; requests so far: %+v", requests)
		}
	}
}

func countRequests(requests []recordedRequest, method string, poll bool) int {
	n := 0
	for _, r := range requests {
		if r.method == method && (r.sid != "") == poll {
			n++
		}
	}
	return n
}

// dialRecorder dials a polling client connection to the recorder, opens it and
// sends one message, so that the handshake, a poll and a POST are all sent.
func dialRecorder(t *testing.T, h *headerRecorder, query string, header http.Header) []recordedRequest {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL + "/?" + query)
	require.NoError(t, err)

	httpClient := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(httpClient.CloseIdleConnections)
	cc, err := dial(httpClient, u, header)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cc.Close()) })
	_, err = cc.Open()
	require.NoError(t, err)

	w, err := cc.NextWriter(eioparser.String, eioparser.MESSAGE)
	require.NoError(t, err)
	_, err = w.Write([]byte("hello"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	return h.waitFor(t, func(requests []recordedRequest) bool {
		return countRequests(requests, http.MethodGet, true) > 0 && countRequests(requests, http.MethodPost, true) > 0
	})
}

// TestDialContentTypeOnlyOnPost checks that the polling client sends
// Content-Type only with the POST that carries a payload, as engine.io-client
// does. A GET with a Content-Type that is not CORS-safelisted makes a browser
// send a preflight, and the server answers the handshake preflight with 400.
func TestDialContentTypeOnlyOnPost(t *testing.T) {
	for _, tc := range []struct {
		name, query, post string
	}{
		{"binary", "EIO=3&transport=polling", "application/octet-stream"},
		{"b64", "EIO=3&transport=polling&b64=1", "text/plain;charset=UTF-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := dialRecorder(t, newHeaderRecorder(t), tc.query, nil)
			for _, r := range requests {
				switch r.method {
				case http.MethodGet:
					require.Empty(t, r.header.Values("Content-Type"), "GET (sid %q)", r.sid)
				case http.MethodPost:
					require.Equal(t, []string{tc.post}, r.header.Values("Content-Type"))
				default:
					t.Fatalf("unexpected %s request", r.method)
				}
			}
		})
	}
}

// TestDialContentTypeKeepsCallerHeader checks that the POST Content-Type does
// not leak into the header map the caller passed to Dial.
func TestDialContentTypeKeepsCallerHeader(t *testing.T) {
	header := http.Header{"X-Test": {"1"}}
	requests := dialRecorder(t, newHeaderRecorder(t), "EIO=3&transport=polling", header)
	require.Equal(t, http.Header{"X-Test": {"1"}}, header)
	for _, r := range requests {
		require.Equal(t, "1", r.header.Get("X-Test"), "%s (sid %q)", r.method, r.sid)
	}
}

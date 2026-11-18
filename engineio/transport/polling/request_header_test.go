package polling

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
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
// It answers the handshake with OPEN, answers the first answerPolls polls with
// NOOP, holds later polls open until the client cancels them, and accepts
// POSTs.
type headerRecorder struct {
	open        []byte
	answerPolls int

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
		h.mu.Lock()
		answer := h.answerPolls > 0
		h.answerPolls--
		h.mu.Unlock()
		if answer {
			_, _ = w.Write([]byte("1:6"))
			return
		}
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

// keyDeletingRoundTripper deletes a header key from each request it sends, as
// Go's Fetch round trip does with its js.fetch:* options under js/wasm.
type keyDeletingRoundTripper struct {
	key  string
	next http.RoundTripper
}

func (rt keyDeletingRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Del(rt.key)
	return rt.next.RoundTrip(r)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// exchangeRepeatedly opens a polling connection to srv through httpClient and
// makes it send the handshake, three POSTs and two polls: the recorder answers
// the first poll with NOOP, which the test reads, so a second poll follows.
func exchangeRepeatedly(t *testing.T, h *headerRecorder, srv *httptest.Server, httpClient *http.Client, query string, header http.Header) []recordedRequest {
	t.Helper()
	u, err := url.Parse(srv.URL + "/?" + query)
	require.NoError(t, err)
	t.Cleanup(httpClient.CloseIdleConnections)
	cc, err := dial(httpClient, u, header)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cc.Close()) })
	_, err = cc.Open()
	require.NoError(t, err)

	_, pt, r, err := cc.NextReader()
	require.NoError(t, err)
	require.Equal(t, eioparser.NOOP, pt)
	require.NoError(t, r.Close())
	for i := 0; i < 3; i++ {
		w, err := cc.NextWriter(eioparser.String, eioparser.MESSAGE)
		require.NoError(t, err)
		_, err = w.Write([]byte("hello"))
		require.NoError(t, err)
		require.NoError(t, w.Close())
		require.NoError(t, cc.Flush())
	}
	return h.waitFor(t, func(requests []recordedRequest) bool {
		return countRequests(requests, http.MethodGet, true) > 1 && countRequests(requests, http.MethodPost, true) > 2
	})
}

var pollingModes = []struct{ name, query string }{
	{"binary", "EIO=3&transport=polling"},
	{"b64", "EIO=3&transport=polling&b64=1"},
}

// TestDialRequestHeaderPerRequest checks that every polling request gets its
// own copy of the request header: a RoundTripper that removes a key from one
// request must not remove it from the later ones.
func TestDialRequestHeaderPerRequest(t *testing.T) {
	const key = "X-Option"
	for _, mode := range pollingModes {
		t.Run(mode.name, func(t *testing.T) {
			h := newHeaderRecorder(t)
			h.answerPolls = 1
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Record whether the client still had the key when it sent the
				// request: the RoundTripper copies it to X-Seen before deleting it.
				r.Header.Set(key, r.Header.Get("X-Seen"))
				h.ServeHTTP(w, r)
			}))
			t.Cleanup(srv.Close)
			httpClient := &http.Client{Timeout: 5 * time.Second, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				r.Header.Set("X-Seen", r.Header.Get(key))
				return keyDeletingRoundTripper{key: key, next: http.DefaultTransport}.RoundTrip(r)
			})}

			requests := exchangeRepeatedly(t, h, srv, httpClient, mode.query, http.Header{key: {"on"}})
			for _, r := range requests {
				require.Equal(t, "on", r.header.Get(key), "%s (sid %q)", r.method, r.sid)
			}
		})
	}
}

// TestDialCookieJarHeaderPerRequest checks that a cookie jar adds its cookie
// once to each request: http.Client adds jar cookies to the request it is
// given, so a header shared across requests grew by one cookie per request.
func TestDialCookieJarHeaderPerRequest(t *testing.T) {
	for _, mode := range pollingModes {
		t.Run(mode.name, func(t *testing.T) {
			h := newHeaderRecorder(t)
			h.answerPolls = 1
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("sid") == "" {
					http.SetCookie(w, &http.Cookie{Name: "io", Value: "sid1"})
				}
				h.ServeHTTP(w, r)
			}))
			t.Cleanup(srv.Close)
			jar, err := cookiejar.New(nil)
			require.NoError(t, err)

			requests := exchangeRepeatedly(t, h, srv, &http.Client{Timeout: 5 * time.Second, Jar: jar}, mode.query, nil)
			for _, r := range requests {
				if r.sid == "" {
					continue
				}
				require.Equal(t, []string{"io=sid1"}, r.header.Values("Cookie"), "%s", r.method)
			}
		})
	}
}

// TestDialCopiesRequestHeader checks that Dial keeps no reference to the
// header it is given: a change to the caller's values after Dial does not
// reach the requests.
func TestDialCopiesRequestHeader(t *testing.T) {
	h := newHeaderRecorder(t)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL + "/?EIO=3&transport=polling")
	require.NoError(t, err)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(httpClient.CloseIdleConnections)
	header := http.Header{"X-Test": {"1"}}
	cc, err := dial(httpClient, u, header)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cc.Close()) })
	header["X-Test"][0] = "changed"
	header.Set("X-Other", "added")
	_, err = cc.Open()
	require.NoError(t, err)

	requests := h.waitFor(t, func(requests []recordedRequest) bool { return len(requests) > 0 })
	require.Equal(t, "1", requests[0].header.Get("X-Test"))
	require.Empty(t, requests[0].header.Get("X-Other"))
}

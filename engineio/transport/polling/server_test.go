package polling

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// TestServerGetV4Body checks the wire form of a poll response: text/plain, a binary
// packet as base64 behind 'b', and no JSONP wrapper for the removed j parameter.
func TestServerGetV4Body(t *testing.T) {
	var scValue atomic.Value
	conn := make(chan transport.Conn, 1)

	handler := func(w http.ResponseWriter, r *http.Request) {
		c := scValue.Load()
		if c == nil {
			co, err := Default.Accept(w, r)
			require.NoError(t, err)
			scValue.Store(co)
			c = co
			conn <- co
		}
		c.(http.Handler).ServeHTTP(w, r)
	}
	httpSvr := httptest.NewServer(http.HandlerFunc(handler))
	defer httpSvr.Close()

	go func() {
		sc := <-conn
		for _, p := range []struct {
			ft   frame.Type
			data string
		}{{frame.Binary, "hello"}, {frame.String, "world"}} {
			w, err := sc.NextWriter(p.ft, packet.MESSAGE)
			if !assert.NoError(t, err) {
				return
			}
			_, _ = w.Write([]byte(p.data))
			assert.NoError(t, w.Close())
		}
	}()

	for _, want := range []string{"baGVsbG8=", "4world"} {
		// b64 and j are not v4 parameters: both are ignored.
		resp, err := http.Get(httpSvr.URL + "?b64=1&j=0")
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "text/plain; charset=UTF-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, want, string(body))
	}
}

// serverPost sends one POST to conn and returns the recorded response.
func serverPost(conn *serverConn, contentType string, body io.Reader, length int64) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/?transport=polling", body)
	req.ContentLength = length
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	conn.ServeHTTP(rec, req)
	return rec
}

func TestServerPost(t *testing.T) {
	const text = "text/plain;charset=UTF-8"
	tests := []struct {
		name        string
		contentType string
		body        string
		chunked     bool
		status      int
		packets     []string // delivered to NextReader
	}{
		{"one packet", text, "4hi", false, http.StatusOK, []string{"hi"}},
		{"batch", text, "4a\x1e4b\x1e4c", false, http.StatusOK, []string{"a", "b", "c"}},
		{"base64 binary", text, "bAQID", false, http.StatusOK, []string{"\x01\x02\x03"}},
		{"exactly the limit", text, "4abcdefg", false, http.StatusOK, []string{"abcdefg"}},
		{"over the limit by length", text, "4abcdefgh", false, http.StatusRequestEntityTooLarge, nil},
		{"over the limit, chunked", text, "4abcdefgh", true, http.StatusRequestEntityTooLarge, nil},
		{"empty", text, "", false, http.StatusBadRequest, nil},
		{"empty record", text, "4a\x1e\x1e4b", false, http.StatusBadRequest, nil},
		{"v3 length prefix", text, "18:4a", false, http.StatusBadRequest, nil},
		{"octet-stream is v3 only", "application/octet-stream", "4a", false, http.StatusBadRequest, nil},
		{"no content type", "", "4a", false, http.StatusBadRequest, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := newServerConn(&Transport{MaxPayload: 8}, httptest.NewRequest(http.MethodGet, "/", nil))
			defer func() { _ = conn.Close() }()
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))

			var got []string
			done := make(chan struct{})
			go func() {
				defer close(done)
				for range tc.packets {
					_, _, r, err := conn.NextReader()
					if !assert.NoError(t, err) {
						return
					}
					b, _ := io.ReadAll(r)
					got = append(got, string(b))
					assert.NoError(t, r.Close())
				}
			}()

			length := int64(len(tc.body))
			var body io.Reader = strings.NewReader(tc.body)
			if tc.chunked {
				length, body = -1, io.MultiReader(body) // hides the length from the handler
			}
			rec := serverPost(conn, tc.contentType, body, length)
			<-done

			assert.Equal(t, tc.status, rec.Code)
			assert.Equal(t, tc.packets, got)
			if tc.status == http.StatusOK {
				assert.Equal(t, "ok", rec.Body.String())
			}
		})
	}
}

// TestServerPostTooLargeKeepsSession: a 413 delivers nothing and does not end the
// session, while a malformed body does, as the batch it carried is lost.
func TestServerPostTooLargeKeepsSession(t *testing.T) {
	conn := newServerConn(&Transport{MaxPayload: 4}, httptest.NewRequest(http.MethodGet, "/", nil))
	defer func() { _ = conn.Close() }()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	const text = "text/plain;charset=UTF-8"

	rec := serverPost(conn, text, strings.NewReader("4toolong"), -1)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)

	go serverPost(conn, text, strings.NewReader("4ok"), 3)
	_, _, r, err := conn.NextReader()
	require.NoError(t, err)
	require.NoError(t, r.Close())

	rec = serverPost(conn, text, strings.NewReader("9"), 1)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	_, _, _, err = conn.NextReader()
	require.Error(t, err)
}

func TestServerInvalidMethod(t *testing.T) {
	conn := newServerConn(Default, httptest.NewRequest(http.MethodGet, "/", nil))
	defer func() { _ = conn.Close() }()
	rec := httptest.NewRecorder()
	conn.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

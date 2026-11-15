package polling

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A poll that fails because a deadline of the connection expired, which the session
// reports as "ping timeout", is answered HTTP 200 with a CLOSE packet in the
// connection's framing, as engine.io 3.6.2 answers the pending poll of a session that
// timed out. A poll that fails for another reason, such as a close without an expired
// deadline, keeps its plain-text HTTP 500.
func TestPollFailureAnswer(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fail        func(c *serverConn)
		ctx         func() (context.Context, context.CancelFunc) // the poll's context; nil keeps the default
		status      int
		text, octet string // bodies for b64=1 and for the binary framing
	}{
		{name: "write deadline expired", status: http.StatusOK, text: "1:1", octet: "\x00\x01\xff1",
			fail: func(c *serverConn) { _ = c.SetWriteDeadline(time.Now().Add(-time.Second)) }},
		{name: "read deadline expired before close", status: http.StatusOK, text: "1:1", octet: "\x00\x01\xff1",
			fail: func(c *serverConn) {
				// The session's reader times out, stores the error and aborts the connection.
				_ = c.payload.Store("read", errTimeout)
				_ = c.Abort()
			}},
		{name: "request body read timed out", status: http.StatusOK, text: "1:1", octet: "\x00\x01\xff1",
			fail: func(c *serverConn) {
				_ = c.payload.Store("read", &requestError{"request body", timeoutError{}})
				_ = c.Close()
			}},
		{name: "request context deadline exceeded", status: http.StatusOK, text: "1:1", octet: "\x00\x01\xff1",
			fail: func(*serverConn) {},
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Unix(1, 0))
			}},
		{name: "request context canceled", status: http.StatusInternalServerError, text: "write: context canceled\n", octet: "write: context canceled\n",
			fail: func(*serverConn) {},
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			}},
		{name: "closed", status: http.StatusInternalServerError, text: "EOF\n", octet: "EOF\n",
			fail: func(c *serverConn) { _ = c.Abort() }},
		{name: "transport error", status: http.StatusInternalServerError, text: "write: broken pipe\n", octet: "write: broken pipe\n",
			fail: func(c *serverConn) {
				_ = c.payload.Store("write", errString("broken pipe"))
				_ = c.Close()
			}},
	} {
		for _, b64 := range []bool{true, false} {
			name := tc.name + "/binary"
			query, want, contentType := "/", tc.octet, "application/octet-stream"
			if b64 {
				name, query, want, contentType = tc.name+"/b64", "/?b64=1", tc.text, "text/plain; charset=UTF-8"
			}
			if tc.status != http.StatusOK {
				contentType = "text/plain; charset=utf-8"
			}
			t.Run(name, func(t *testing.T) {
				c := newServerConn(&Transport{Compression: true, CompressionThreshold: 1}, httptest.NewRequest(http.MethodGet, query, nil))
				tc.fail(c)
				req := httptest.NewRequest(http.MethodGet, query, nil)
				if tc.ctx != nil {
					ctx, cancel := tc.ctx()
					defer cancel()
					req = req.WithContext(ctx)
				}
				req.Header.Set("Accept-Encoding", "gzip")
				rec := httptest.NewRecorder()
				c.ServeHTTP(rec, req)
				require.Equal(t, tc.status, rec.Code)
				require.Equal(t, contentType, rec.Header().Get("Content-Type"))
				require.Empty(t, rec.Header().Get("Content-Encoding"))
				require.Equal(t, want, rec.Body.String())
			})
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

package polling

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
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

// A poll whose response the session writer has started keeps that response when a
// deadline then expires: the packets already written are delivered, not replaced by
// a lone CLOSE.
func TestPollStartedResponseOutranksCloseAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		packet      string
	}{
		{name: "b64", query: "/?b64=1", packet: "6:4hello"},
		{name: "binary", query: "/", packet: "\x00\x06\xff4hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newServerConn(Default, httptest.NewRequest(http.MethodGet, tc.query, nil))
			rec := httptest.NewRecorder()
			served := make(chan struct{})
			go func() {
				defer close(served)
				c.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.query, nil))
			}()

			// The session writer takes the poll's writer and writes one packet.
			w, err := c.payload.AcquirePayloadWriter()
			require.NoError(t, err)
			_, err = w.Write([]byte(tc.packet))
			require.NoError(t, err)
			// Before the packet's write is released, a request body read times out:
			// its error is stored and the connection closed.
			_ = c.payload.Store("read", &requestError{"request body", timeoutError{}})
			_ = c.Close()
			<-served

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, tc.packet, rec.Body.String())
		})
	}
}

// stalledBody is a POST body that delivers the start of a packet and then blocks,
// as a network read does, until an expired read deadline is applied to its request.
type stalledBody struct {
	*httptest.ResponseRecorder
	once    sync.Once
	sent    bool
	stalled chan struct{} // closed when a read blocks
	expired chan struct{} // closed by an expired read deadline
}

func newStalledBody() *stalledBody {
	return &stalledBody{ResponseRecorder: httptest.NewRecorder(), stalled: make(chan struct{}), expired: make(chan struct{})}
}

func (b *stalledBody) SetReadDeadline(t time.Time) error {
	if !t.IsZero() && !t.After(time.Now()) {
		b.once.Do(func() { close(b.expired) })
	}
	return nil
}

func (b *stalledBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, "10:4"), nil
	}
	select {
	case <-b.stalled:
	default:
		close(b.stalled)
	}
	<-b.expired
	return 0, os.ErrDeadlineExceeded
}

func (b *stalledBody) Close() error { return nil }

// A POST body read that the close of the connection interrupts is not an expired
// session deadline. Close and Abort expire the read before they close the payload,
// so its error can be stored first; a poll pending at that close then keeps its
// HTTP 500. The same read interrupted by the session's read deadline is a timeout,
// and the poll is answered with CLOSE.
func TestPollAnswerAfterInterruptedBodyRead(t *testing.T) {
	for _, tc := range []struct {
		name       string
		interrupt  func(c *serverConn)
		status     int
		post, poll string
	}{
		{name: "close", status: http.StatusInternalServerError,
			post: "read: request body: use of closed network connection\n",
			poll: "read: request body: use of closed network connection\n",
			interrupt: func(c *serverConn) {
				// The first step of Close and Abort. The test finishes the close
				// after the POST has failed, which is the order under test.
				c.ioMu.Lock()
				c.ioClosed = true
				c.reading.expire()
				c.ioMu.Unlock()
			}},
		{name: "read deadline", status: http.StatusOK,
			post: "read: request body: i/o timeout\n", poll: "1:1",
			interrupt: func(c *serverConn) {
				// Only the tracked body read follows the expired deadline, so the
				// stored error is that read's, not the payload's own deadline error.
				c.ioMu.Lock()
				c.reading.follow(time.Now().Add(-time.Second))
				c.ioMu.Unlock()
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newServerConn(Default, httptest.NewRequest(http.MethodGet, "/?b64=1", nil))
			read := make(chan struct{})
			go func() {
				defer close(read)
				for {
					_, _, r, err := c.NextReader()
					if err != nil {
						return
					}
					_, _ = io.Copy(io.Discard, r)
					_ = r.Close()
				}
			}()

			body := newStalledBody()
			posted := make(chan struct{})
			go func() {
				defer close(posted)
				c.ServeHTTP(body, httptest.NewRequest(http.MethodPost, "/?b64=1", body))
			}()
			<-body.stalled
			tc.interrupt(c)
			<-posted
			require.Equal(t, http.StatusBadRequest, body.Code)
			require.Equal(t, tc.post, body.Body.String())

			_ = c.Abort()
			<-read
			rec := httptest.NewRecorder()
			c.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?b64=1", nil))
			require.Equal(t, tc.status, rec.Code)
			require.Equal(t, tc.poll, rec.Body.String())
		})
	}
}

type errString string

func (e errString) Error() string { return string(e) }

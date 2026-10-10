package polling

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio/payload"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
)

// newBigResponseServer answers the open request with params and every poll with
// "4" and size bytes, the way this repo's server does: it applies no limit to a
// response. It counts the polls.
func newBigResponseServer(t *testing.T, params transport.ConnParameters, size int) (*httptest.Server, *atomic.Int32) {
	var polls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		switch {
		case r.URL.Query().Get("sid") == "":
			var open strings.Builder
			_, _ = params.WriteTo(&open)
			_, _ = io.WriteString(w, "0"+open.String())
		case r.Method == http.MethodGet:
			polls.Add(1)
			_, _ = io.WriteString(w, "4"+strings.Repeat("a", size))
		}
	}))
	t.Cleanup(s.Close)
	return s, &polls
}

func dialBig(t *testing.T, s *httptest.Server, tr *Transport) (*clientConn, transport.ConnParameters, error) {
	u, err := url.Parse(s.URL)
	require.NoError(t, err)
	conn, err := tr.Dial(u, nil)
	require.NoError(t, err)
	params, err := conn.(*clientConn).Open()
	return conn.(*clientConn), params, err
}

// TestClientPollResponseOverLimitFails: a poll response over Transport.MaxPayload
// ends the session with the read error instead of leaving the reader blocked.
func TestClientPollResponseOverLimitFails(t *testing.T) {
	s, polls := newBigResponseServer(t, transport.ConnParameters{SID: "sid", PingInterval: time.Second, PingTimeout: time.Second}, 1000)
	conn, _, err := dialBig(t, s, &Transport{MaxPayload: 150})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	done := make(chan error, 1)
	go func() {
		_, _, _, err := conn.NextReader()
		done <- err
	}()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, payload.ErrTooLarge)
	case <-time.After(5 * time.Second):
		t.Fatalf("NextReader blocked after an oversized poll response (polls=%d)", polls.Load())
	}
}

// TestClientOpenResponseOverLimitFails: Open returns the error when the open
// response is over Transport.MaxPayload.
func TestClientOpenResponseOverLimitFails(t *testing.T) {
	s, _ := newBigResponseServer(t, transport.ConnParameters{SID: strings.Repeat("s", 300), PingInterval: time.Second, PingTimeout: time.Second}, 0)
	done := make(chan error, 1)
	go func() {
		conn, _, err := dialBig(t, s, &Transport{MaxPayload: 150})
		_ = conn.Close()
		done <- err
	}()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, payload.ErrTooLarge)
	case <-time.After(5 * time.Second):
		t.Fatal("Open blocked after an oversized open response")
	}
}

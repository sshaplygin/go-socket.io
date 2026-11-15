package engineio_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

type pollResult struct {
	status      int
	contentType string
	body        []byte
	err         error
}

// A session that ends with "ping timeout" while a poll is pending answers that
// poll with HTTP 200 and a CLOSE packet, as engine.io 3.6.2 does (its pending poll
// receives "1:1" at the timeout), so the client sees an orderly close instead of a
// failed request. The packet uses the connection's payload framing. A later poll is
// answered HTTP 400 with code 1, as in the reference.
func TestPendingPollAtPingTimeoutReceivesClose(t *testing.T) {
	for _, tc := range []struct {
		name        string
		query       string
		contentType string
		body        []byte
	}{
		{name: "text payload", query: "&b64=1", contentType: "text/plain; charset=UTF-8", body: []byte("1:1")},
		{name: "binary payload", contentType: "application/octet-stream", body: []byte{0x00, 0x01, 0xff, '1'}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The poll must be pending before the heartbeat window expires. A window that
			// passed before the poll arrived says nothing; retry with a longer one, so the
			// result does not depend on how fast a loaded machine starts the request.
			for window := 200 * time.Millisecond; !pendingPollAtPingTimeout(t, tc.query, window, tc.contentType, tc.body); window *= 4 {
				require.Less(t, window, 10*time.Second, "the poll was never pending before the ping timeout")
			}
		})
	}
}

// pendingPollAtPingTimeout runs one session with a heartbeat window (ping interval
// plus ping timeout) and checks the answer of a poll left pending until the window
// expires. It returns false when the session ended before the poll was pending.
func pendingPollAtPingTimeout(t *testing.T, query string, window time.Duration, contentType string, want []byte) bool {
	t.Helper()
	s := engineio.NewServer(engineio.WithPingInterval(window/2), engineio.WithPingTimeout(window/2))
	ts := httptest.NewServer(s)
	defer func() { _ = s.Close(); ts.Close() }()
	client := &http.Client{Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()

	resp, err := client.Get(ts.URL + "/?EIO=3&transport=polling" + query)
	require.NoError(t, err)
	open, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	m := sidPattern.FindSubmatch(open)
	require.NotNil(t, m, "no sid in handshake response %q", open)
	url := ts.URL + "/?EIO=3&transport=polling" + query + "&sid=" + string(m[1])

	conn := acceptOne(t, s)
	done := readUntilEnd(conn)
	polled := make(chan pollResult, 1)
	go func() {
		resp, err := client.Get(url)
		if err != nil {
			polled <- pollResult{err: err}
			return
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		polled <- pollResult{status: resp.StatusCode, contentType: resp.Header.Get("Content-Type"), body: body, err: err}
	}()

	writable := conn.(interface{ Writable() bool })
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	var ended error
	for ended == nil && !writable.Writable() {
		select {
		case ended = <-done:
		case <-tick.C:
		}
	}
	if ended != nil {
		<-polled
		return false
	}

	select {
	case ended = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end")
	}
	var closeErr *engineio.CloseError
	require.ErrorAs(t, ended, &closeErr)
	require.Equal(t, engineio.ReasonPingTimeout, closeErr.Reason)

	var got pollResult
	select {
	case got = <-polled:
	case <-time.After(10 * time.Second):
		t.Fatal("the pending poll was not answered")
	}
	require.NoError(t, got.err)
	require.Equal(t, http.StatusOK, got.status, "body %q", got.body)
	require.Equal(t, contentType, got.contentType)
	require.Equal(t, want, got.body)

	resp, err = client.Get(url)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.JSONEq(t, `{"code":1,"message":"Session ID unknown"}`, string(body))
	return true
}

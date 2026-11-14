package engineio_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
)

// openPollingParams performs a polling handshake against url, with header, and returns
// the response and the parameters of its OPEN packet.
func openPollingParams(t *testing.T, url string, header http.Header) (*http.Response, transport.ConnParameters) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url+"/?EIO=4&transport=polling", nil)
	require.NoError(t, err)
	if header != nil {
		req.Header = header
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.True(t, bytes.HasPrefix(body, []byte("0")), "open packet expected, got %q", body)
	params, err := transport.ReadConnParameters(bytes.NewReader(body[1:]))
	require.NoError(t, err)
	return resp, params
}

// postSize POSTs a MESSAGE of size wire bytes to the polling session sid and returns the
// status of the answer.
func postSize(t *testing.T, url, sid string, size int) int {
	t.Helper()
	body := strings.NewReader("4" + strings.Repeat("a", size-1))
	req, err := http.NewRequest(http.MethodPost, url+"/?EIO=4&transport=polling&sid="+sid, body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain; charset=UTF-8")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode
}

// requireTooBig sends a MESSAGE of size data bytes, its type byte included, on c and
// requires that the server closes the connection with status 1009.
func requireTooBig(t *testing.T, c transport.Conn, size int) {
	t.Helper()
	writePacket(t, c, packet.MESSAGE, strings.Repeat("a", size-1))
	require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
	var err error
	for err == nil {
		var r io.ReadCloser
		if _, _, r, err = c.NextReader(); err == nil {
			_ = r.Close()
		}
	}
	var closed wsutil.ClosedError
	require.ErrorAs(t, err, &closed)
	require.Equal(t, ws.StatusMessageTooBig, closed.Code)
}

// TestServerKeepsTransportCopies: NewServer works on private copies of its polling and
// websocket transports, so later changes to the configured structs, and to
// polling.Default.CheckOrigin, which the polling copy resolved, do not reach the server.
// Not parallel: it sets polling.Default.CheckOrigin.
func TestServerKeepsTransportCopies(t *testing.T) {
	pt, wt := &polling.Transport{MaxPayload: 300}, &websocket.Transport{MaxPayload: 300}
	f := newGuardFixture(t, &engineio.Options{Transports: []transport.Transport{pt, wt}})
	pt.MaxPayload, wt.MaxPayload = 5000, 5000
	prev := polling.Default.CheckOrigin
	polling.Default.CheckOrigin = func(*http.Request) bool { return true }
	t.Cleanup(func() { polling.Default.CheckOrigin = prev })

	resp, params := openPollingParams(t, f.url, http.Header{"Origin": {"http://example.com"}})
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
	polled := f.accept(t)
	go func() { _ = readAll(polled) }()
	require.Equal(t, http.StatusRequestEntityTooLarge, postSize(t, f.url, params.SID, 301))

	c, err := f.dial(t, "", nil)
	require.NoError(t, err)
	opened := f.accept(t)
	go func() { _ = readAll(opened) }()
	requireTooBig(t, c, 301)
}

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

// TestServerResolvesCheckOriginFallback: a polling transport without CheckOrigin uses
// polling.Default.CheckOrigin as it was at NewServer, so the CORS header it allowed is
// still sent after polling.Default.CheckOrigin is cleared, here while the handshake runs,
// so that -race reports a read of polling.Default at request time. Not parallel: it sets
// polling.Default.CheckOrigin.
func TestServerResolvesCheckOriginFallback(t *testing.T) {
	prev := polling.Default.CheckOrigin
	t.Cleanup(func() { polling.Default.CheckOrigin = prev })
	polling.Default.CheckOrigin = func(*http.Request) bool { return true }
	f := newGuardFixture(t, &engineio.Options{Transports: []transport.Transport{&polling.Transport{}}})
	cleared := make(chan struct{})
	go func() { polling.Default.CheckOrigin = nil; close(cleared) }()
	resp, _ := openPollingParams(t, f.url, http.Header{"Origin": {"http://example.com"}})
	<-cleared
	require.Equal(t, "http://example.com", resp.Header.Get("Access-Control-Allow-Origin"))
}

// TestOptionsMaxPayload: a positive Options.MaxPayload is the limit the server advertises
// in OPEN and enforces over the limits of its transports: a polling POST over it is
// answered 413, and a websocket message over it closes the connection with status 1009,
// on a session opened on websocket and after an upgrade. Zero or a negative value keeps
// the transport limits.
func TestOptionsMaxPayload(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		option, polling, message int
	}{
		{"options", 300, 300, 300},
		{"zero", 0, 5000, 6000},
		{"negative", -1, 5000, 6000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGuardFixture(t, &engineio.Options{MaxPayload: tc.option, Transports: []transport.Transport{
				&polling.Transport{MaxPayload: 5000}, &websocket.Transport{MaxPayload: 6000},
			}})
			_, params := openPollingParams(t, f.url, nil)
			require.Equal(t, tc.polling, params.MaxPayload)
			polled := f.accept(t)
			go func() { _ = readAll(polled) }()
			require.Equal(t, http.StatusRequestEntityTooLarge, postSize(t, f.url, params.SID, tc.polling+1))
			require.Equal(t, http.StatusOK, postSize(t, f.url, params.SID, tc.polling))

			opened, err := f.dial(t, "", nil)
			require.NoError(t, err)
			openedConn := f.accept(t)
			go func() { _ = readAll(openedConn) }()
			requireTooBig(t, opened, tc.message+1)

			dialRaw(t, polling.Default, f.url)
			conn := f.accept(t)
			go func() { _ = readAll(conn) }()
			upgraded, err := f.dial(t, conn.ID(), nil)
			require.NoError(t, err)
			upgradeTo(t, upgraded, conn)
			requireTooBig(t, upgraded, tc.message+1)
		})
	}
}

// TestMaxPayloadNormalize: Normalize rejects a negative MaxPayload and returns valid
// options unchanged. NewServer treats a negative value as zero, which
// TestOptionsMaxPayload checks.
func TestMaxPayloadNormalize(t *testing.T) {
	_, err := engineio.Options{MaxPayload: -1}.Normalize()
	require.Error(t, err)
	valid := engineio.Options{MaxPayload: 1}
	got, err := valid.Normalize()
	require.NoError(t, err)
	require.Equal(t, valid, got)
}

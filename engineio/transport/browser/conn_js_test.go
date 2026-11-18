//go:build js && wasm

package browser

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

func fixtureURL(t *testing.T, name, path string) *url.URL {
	t.Helper()
	base := os.Getenv(name)
	require.NotEmpty(t, base, "%s is empty: run make test-browser", name)
	u, err := url.Parse(base + path)
	require.NoError(t, err)
	return u
}

func read(t *testing.T, c transport.Conn) (eioparser.FrameType, eioparser.PacketType, string) {
	t.Helper()
	ft, pt, r, err := c.NextReader()
	require.NoError(t, err)
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return ft, pt, string(data)
}

func write(t *testing.T, c transport.Conn, ft eioparser.FrameType, data string) {
	t.Helper()
	w, err := c.NextWriter(ft, eioparser.MESSAGE)
	require.NoError(t, err)
	_, err = io.WriteString(w, data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

// dialEngine opens an Engine.IO session of the fixture and reads OPEN and the CONNECT of /.
func dialEngine(t *testing.T) *conn {
	t.Helper()
	u := fixtureURL(t, "SOCKETIO_BROWSER_URL", "/socket.io/?EIO=3")
	host := u.Host
	c, err := (&Transport{HandshakeTimeout: 5 * time.Second}).Dial(u, http.Header{"X-Not-Sent": {"1"}})
	require.NoError(t, err)
	require.Equal(t, "ws", u.Scheme)
	require.Equal(t, *u, c.URL())
	require.Equal(t, "websocket", u.Query().Get("transport"))
	require.Equal(t, http.Header{}, c.RemoteHeader())
	require.Equal(t, host, c.RemoteAddr().String())
	require.Equal(t, "tcp", c.LocalAddr().Network())
	ft, pt, _ := read(t, c)
	require.Equal(t, []any{eioparser.String, eioparser.OPEN}, []any{ft, pt})
	ft, pt, data := read(t, c)
	require.Equal(t, []any{eioparser.String, eioparser.MESSAGE, "0"}, []any{ft, pt, data})
	return c.(*conn)
}

func requireClosed(t *testing.T, c *conn) {
	t.Helper()
	require.Zero(t, liveFuncs.Load(), "js.Funcs released")
	require.Contains(t, []int{2, 3}, c.ws.Get("readyState").Int(), "CLOSING or CLOSED")
	_, err := c.NextWriter(eioparser.String, eioparser.PING)
	require.ErrorIs(t, err, net.ErrClosed)
	require.False(t, c.Writable())
}

func TestTextAndBinaryFrames(t *testing.T) {
	c := dialEngine(t)
	// A BINARY_EVENT with an ACK id: its text frame, then the attachment as a binary frame.
	write(t, c, eioparser.String, `51-0["binary",{"_placeholder":true,"num":0}]`)
	write(t, c, eioparser.Binary, "\x00\x01\xff")
	// The server emits the event back, then acknowledges it, each with the attachment.
	for _, want := range []eioparser.FrameType{eioparser.String, eioparser.Binary, eioparser.String, eioparser.Binary} {
		ft, pt, data := read(t, c)
		require.Equal(t, []any{want, eioparser.MESSAGE}, []any{ft, pt})
		if ft == eioparser.Binary {
			require.Equal(t, "\x00\x01\xff", data)
		}
	}
	require.NoError(t, c.Close())
	_, _, _, err := c.NextReader()
	require.ErrorIs(t, err, net.ErrClosed)
	requireClosed(t, c)
	require.NoError(t, c.Close())
}

func TestPeerCloseAfterQueuedMessages(t *testing.T) {
	c := dialEngine(t)
	write(t, c, eioparser.String, `2["echo","before"]`)
	write(t, c, eioparser.String, `2["close-engine"]`)
	_, _, data := read(t, c)
	require.Equal(t, `2["echo","before"]`, strings.TrimSpace(data), "messages that precede the close are read first")
	for {
		_, _, r, err := c.NextReader()
		if err != nil {
			var closeErr *websocket.CloseError
			require.ErrorAs(t, err, &closeErr)
			require.Equal(t, websocket.CloseNormalClosure, closeErr.Code)
			break
		}
		require.NoError(t, r.Close())
	}
	_, err := c.NextWriter(eioparser.String, eioparser.PING)
	require.IsType(t, &websocket.CloseError{}, err)
	require.NoError(t, c.Close())
	require.Zero(t, liveFuncs.Load())
}

func TestReadLimit(t *testing.T) {
	c := dialEngine(t)
	c.SetReadLimit(16)
	write(t, c, eioparser.String, `2["echo","`+strings.Repeat("x", 32)+`"]`)
	_, _, _, err := c.NextReader()
	var tooLarge *http.MaxBytesError
	require.ErrorAs(t, err, &tooLarge)
	require.EqualValues(t, 16, tooLarge.Limit)
	require.ErrorIs(t, err, websocket.ErrReadLimit)
	require.ErrorIs(t, c.ReadLimitError(), websocket.ErrReadLimit)
	require.False(t, c.Writable())
	require.NoError(t, c.Abort())
	require.Zero(t, liveFuncs.Load())
	require.NoError(t, c.Close(), "Close after Abort")
}

func TestDeadlinesAndInvalidCalls(t *testing.T) {
	c := dialEngine(t)
	require.True(t, c.Writable())
	require.Nil(t, c.ReadLimitError())
	require.NoError(t, c.SetReadDeadline(time.Now().Add(50*time.Millisecond)))
	_, _, _, err := c.NextReader()
	var netErr net.Error
	require.ErrorAs(t, err, &netErr)
	require.True(t, netErr.Timeout())
	require.NoError(t, c.SetWriteDeadline(time.Now().Add(-time.Second)))
	_, err = c.NextWriter(eioparser.String, eioparser.PING)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	_, err = frameWriter{c}.NextWriter(eioparser.FrameType(7))
	require.ErrorIs(t, err, transport.ErrInvalidFrame)
	require.Error(t, c.call("close", 1001), "the browser rejects close code 1001")
	require.NoError(t, c.Close())
	requireClosed(t, c)
}

func TestDialFailures(t *testing.T) {
	webSocket := js.Global().Get("WebSocket")
	for name, tc := range map[string]struct {
		transport Transport
		url       *url.URL
		before    func()
		timeout   bool
	}{
		"closed listener": {Transport{HandshakeTimeout: 2 * time.Second}, fixtureURL(t, "SOCKETIO_BROWSER_CLOSED_URL", "/socket.io/?EIO=3"), nil, false},
		"silent server":   {Transport{HandshakeTimeout: 300 * time.Millisecond}, fixtureURL(t, "SOCKETIO_BROWSER_URL", "/silent/?EIO=3"), nil, true},
		"subprotocol not selected": {Transport{Subprotocols: []string{"chat"}, HandshakeTimeout: 2 * time.Second},
			fixtureURL(t, "SOCKETIO_BROWSER_URL", "/socket.io/?EIO=3"), nil, false},
		"invalid URL":  {Transport{}, &url.URL{Scheme: "ftp", Host: "127.0.0.1"}, nil, false},
		"no WebSocket": {Transport{}, fixtureURL(t, "SOCKETIO_BROWSER_URL", "/socket.io/"), func() { js.Global().Set("WebSocket", js.Undefined()) }, false},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.before != nil {
				tc.before()
				t.Cleanup(func() { js.Global().Set("WebSocket", webSocket) })
			}
			start := time.Now()
			c, err := tc.transport.Dial(tc.url, nil)
			require.Error(t, err)
			require.Nil(t, c)
			require.Equal(t, tc.timeout, errors.Is(err, os.ErrDeadlineExceeded), "%v", err)
			require.Less(t, time.Since(start), 2*time.Second)
			if tc.timeout {
				require.GreaterOrEqual(t, time.Since(start), tc.transport.HandshakeTimeout)
			}
			require.Zero(t, liveFuncs.Load(), "js.Funcs released")
		})
	}
}

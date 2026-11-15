package engineio

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// The server options decide compression and the session cookie: the corresponding
// fields of a transport passed to WithTransports are replaced on the server's copy, and
// the caller's transport is not modified.
func TestServerOptionsOverrideTransportFields(t *testing.T) {
	mine := &http.Cookie{Name: "mine", Path: "/mine"}
	poll := &polling.Transport{Compression: false, CompressionThreshold: 1, Cookie: mine}
	ws := &websocket.Transport{EnableCompression: true, CompressionThreshold: 1}
	s, ts := compressionServer(t, []Option{WithTransports(poll, ws)})

	cl := &http.Client{Timeout: 2 * time.Second}
	defer cl.CloseIdleConnections()
	resp, err := cl.Get(ts.URL + "/?EIO=3&transport=polling&b64=1")
	require.NoError(t, err)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	_, open, ok := strings.Cut(string(data), ":0")
	require.True(t, ok)
	var params eioparser.ConnParameters
	require.NoError(t, json.Unmarshal([]byte(open), &params))
	// The cookie is the server's default, not Transport.Cookie.
	require.Equal(t, (&http.Cookie{Name: "io", Value: params.SID, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode}).String(), resp.Header.Get("Set-Cookie"))

	c := compressionAccept(t, s)
	for _, tc := range []struct {
		size     int
		encoding string
	}{{100, ""}, {4096, "gzip"}} { // the server's threshold of 1024, compression on
		done := parityWrite(c, strings.Repeat("x", tc.size))
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/?EIO=3&transport=polling&b64=1&sid="+params.SID, nil)
		require.NoError(t, err)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := cl.Do(req)
		require.NoError(t, err)
		require.Equal(t, tc.encoding, resp.Header.Get("Content-Encoding"), "size %d", tc.size)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.NoError(t, <-done)
	}

	// WithPerMessageDeflate is off by default, so Transport.EnableCompression is not offered.
	peer := compressionDial(t, ts.URL)
	require.NotContains(t, peer.response.Header.Get("Sec-WebSocket-Extensions"), "permessage-deflate")

	require.Equal(t, &polling.Transport{Compression: false, CompressionThreshold: 1, Cookie: mine}, poll)
	require.Equal(t, &http.Cookie{Name: "mine", Path: "/mine"}, mine)
	require.Equal(t, &websocket.Transport{EnableCompression: true, CompressionThreshold: 1}, ws)
}

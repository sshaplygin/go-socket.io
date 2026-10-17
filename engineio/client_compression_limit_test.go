package engineio

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
)

type limitedCompressionTransport struct{ *websocket.Transport }

func (t limitedCompressionTransport) Dial(u *url.URL, h http.Header) (transport.Conn, error) {
	c, err := t.Transport.Dial(u, h)
	if err == nil {
		c.(interface{ SetReadLimit(int64) }).SetReadLimit(1024)
	}
	return c, err
}

// Covers E8
// Covers C11
func TestEngineClientCompressedCloseLimit(t *testing.T) {
	ws := &websocket.Transport{EnableCompression: true, CompressionThreshold: 1}
	type acceptedConn struct {
		conn transport.Conn
		err  error
	}
	accepted := make(chan acceptedConn, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := ws.Accept(w, r)
		if err == nil {
			var writer io.WriteCloser
			writer, err = c.NextWriter(frame.String, packet.OPEN)
			if err == nil {
				params := transport.ConnParameters{SID: "compressed-client", PingInterval: time.Hour, PingTimeout: time.Second}
				_, err = params.WriteTo(writer)
				if closeErr := writer.Close(); err == nil {
					err = closeErr
				}
			}
		}
		accepted <- acceptedConn{c, err}
	}))
	defer ts.Close()
	dialer := Dialer{Transports: []transport.Transport{limitedCompressionTransport{ws}}}
	c, err := dialer.Dial(ts.URL, nil)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	var peer transport.Conn
	select {
	case result := <-accepted:
		require.NoError(t, result.err)
		peer = result.conn
	case <-time.After(time.Second):
		t.Fatal("compressed client not accepted")
	}
	defer func() { _ = peer.Close() }()
	writer, err := peer.NextWriter(frame.String, packet.CLOSE)
	require.NoError(t, err)
	_, err = io.WriteString(writer, strings.Repeat("a", 100_000))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	_, reader, err := c.NextReader()
	require.Nil(t, reader)
	var closed *CloseError
	require.ErrorAs(t, err, &closed)
	require.Equal(t, ReasonTransportError, closed.Reason)
	require.ErrorIs(t, closed.Err, gorilla.ErrReadLimit)
}

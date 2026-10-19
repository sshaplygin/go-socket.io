package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

type limitedCompressionTransport struct{ *websocket.Transport }

func (t limitedCompressionTransport) Dial(u *url.URL, h http.Header) (transport.Conn, error) {
	c, err := t.Transport.Dial(u, h)
	if err == nil {
		c.(interface{ SetReadLimit(int64) }).SetReadLimit(1024)
	}
	return c, err
}

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
			writer, err = c.NextWriter(eioparser.String, eioparser.OPEN)
			if err == nil {
				params := eioparser.ConnParameters{SID: "compressed-client", PingInterval: time.Hour, PingTimeout: time.Second}
				_, err = params.WriteTo(writer)
				if closeErr := writer.Close(); err == nil {
					err = closeErr
				}
			}
		}
		accepted <- acceptedConn{c, err}
	}))
	defer ts.Close()
	dialer := Dialer{MaxMessageSize: 1024, Transports: []transport.Transport{limitedCompressionTransport{ws}}}
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
	writer, err := peer.NextWriter(eioparser.String, eioparser.CLOSE)
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

func TestEngineClientCompressedOpenLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pt      eioparser.PacketType
		invalid bool
	}{{"oversized OPEN", eioparser.OPEN, false}, {"preserve JSON error", eioparser.OPEN, true}, {"preserve packet error", eioparser.MESSAGE, false}} {
		t.Run(tc.name, func(t *testing.T) {
			ws := &websocket.Transport{EnableCompression: true, CompressionThreshold: 1}
			type serverResult struct {
				conn transport.Conn
				err  error
			}
			accepted := make(chan serverResult, 1)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := ws.Accept(w, r)
				if err == nil {
					var writer io.WriteCloser
					writer, err = c.NextWriter(eioparser.String, tc.pt)
					if err == nil {
						if tc.invalid {
							_, err = io.WriteString(writer, "!")
						} else {
							params := eioparser.ConnParameters{SID: "oversized-open", PingInterval: time.Hour, PingTimeout: time.Second}
							_, err = params.WriteTo(writer)
						}
						if err == nil {
							_, err = io.WriteString(writer, strings.Repeat(" ", 100_000))
						}
						if closeErr := writer.Close(); err == nil {
							err = closeErr
						}
					}
				}
				accepted <- serverResult{c, err}
			}))
			defer ts.Close()
			dialer := Dialer{MaxMessageSize: 1024, Transports: []transport.Transport{limitedCompressionTransport{ws}}}
			c, err := dialer.Dial(ts.URL, nil)
			if c != nil {
				defer func() { _ = c.Close() }()
			}
			select {
			case result := <-accepted:
				require.NoError(t, result.err)
				defer func() { _ = result.conn.Close() }()
			case <-time.After(time.Second):
				t.Fatal("no OPEN peer")
			}
			require.Error(t, err)
			require.Nil(t, c, "Dial must reject an invalid OPEN instead of returning a failed connection")
			switch {
			case tc.invalid:
				var syntax *json.SyntaxError
				require.ErrorAs(t, err, &syntax)
				require.NotErrorIs(t, err, gorilla.ErrReadLimit)
			case tc.pt != eioparser.OPEN:
				require.EqualError(t, err, "invalid open")
			default:
				require.ErrorIs(t, err, gorilla.ErrReadLimit)
			}
		})
	}
}

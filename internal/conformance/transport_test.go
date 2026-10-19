//go:build conformance

package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

var rawHTTP = &http.Client{Timeout: 5 * time.Second}

type response struct {
	status int
	header http.Header
	body   []byte
	err    error
}

func request(method, url string, body io.Reader) response {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return response{err: err}
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	}
	r, err := rawHTTP.Do(req)
	if err != nil {
		return response{err: err}
	}
	defer func() { _ = r.Body.Close() }()
	b, err := io.ReadAll(r.Body)
	return response{status: r.StatusCode, header: r.Header, body: b, err: err}
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(6 * time.Second):
		t.Fatal("timed out waiting for transport operation")
		var zero T
		return zero
	}
}

func engineServer(t *testing.T) (*engineio.Server, *httptest.Server) {
	t.Helper()
	s := engineio.NewServer()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	return s, ts
}

func textPackets(t *testing.T, body []byte) []string {
	t.Helper()
	var packets []string
	for len(body) != 0 {
		var size, offset int
		_, err := fmt.Sscanf(string(body), "%d:", &size)
		require.NoError(t, err, "%q", body)
		offset = bytes.IndexByte(body, ':') + 1
		require.Positive(t, offset)
		require.Positive(t, size)
		require.LessOrEqual(t, offset+size, len(body))
		packets = append(packets, string(body[offset:offset+size]))
		body = body[offset+size:]
	}
	return packets
}

func openPolling(t *testing.T, base string) string {
	return openPollingMode(t, base, false)
}

func pollPackets(t *testing.T, body []byte, binary bool) []string {
	t.Helper()
	if !binary {
		return textPackets(t, body)
	}
	var packets []string
	for len(body) > 0 {
		require.Equal(t, byte(0), body[0], "text packet in a binary payload")
		end := bytes.IndexByte(body, 255)
		require.Greater(t, end, 1)
		size := 0
		for _, digit := range body[1:end] {
			require.LessOrEqual(t, digit, byte(9))
			size = size*10 + int(digit)
		}
		require.Positive(t, size)
		body = body[end+1:]
		require.LessOrEqual(t, size, len(body))
		packets = append(packets, string(body[:size]))
		body = body[size:]
	}
	return packets
}

func openPollingMode(t *testing.T, base string, binary bool) string {
	t.Helper()
	url := base + "/?EIO=3&transport=polling"
	if !binary {
		url += "&b64=1"
	}
	r := request(http.MethodGet, url, nil)
	require.NoError(t, r.err)
	require.Equal(t, http.StatusOK, r.status, "%s", r.body)
	packets := pollPackets(t, r.body, binary)
	require.NotEmpty(t, packets)
	require.True(t, strings.HasPrefix(packets[0], "0"))
	var open struct {
		SID string `json:"sid"`
	}
	require.NoError(t, json.Unmarshal([]byte(packets[0][1:]), &open))
	require.NotEmpty(t, open.SID)
	return url + "&sid=" + open.SID
}

// Covers P9
func TestPollingOptions(t *testing.T) {
	s, ts := engineServer(t)
	poll := openPolling(t, ts.URL)
	c, err := s.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	r := request(http.MethodOptions, poll, nil)
	require.NoError(t, r.err)
	require.Equal(t, http.StatusOK, r.status)
	require.Equal(t, "Content-Type", r.header.Get("Access-Control-Allow-Headers"))
	require.Empty(t, r.body)
}

// Covers P15
func TestWebsocketFraming(t *testing.T) {
	s, ts := engineServer(t)
	ws, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(5*time.Second)))
	typ, body, err := ws.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, typ)
	require.True(t, bytes.HasPrefix(body, []byte("0{")), "%q", body)
	c, err := s.Accept()
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	for _, message := range []struct {
		frame eioparser.FrameType
		ws    int
		body  []byte
		wire  []byte
	}{
		{eioparser.String, websocket.TextMessage, []byte("hello"), []byte("4hello")},
		{eioparser.Binary, websocket.BinaryMessage, []byte{0, 255, 1}, []byte{4, 0, 255, 1}},
	} {
		// Two writes produce two distinct messages, with no polling length prefix
		// or base64 transformation; binary data follows the numeric packet type.
		for i := 0; i < 2; i++ {
			w, err := c.NextWriter(message.frame)
			require.NoError(t, err)
			_, err = w.Write(message.body)
			require.NoError(t, err)
			require.NoError(t, w.Close())
		}
		for i := 0; i < 2; i++ {
			typ, body, err = ws.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, message.ws, typ)
			require.Equal(t, message.wire, body)
		}
		require.NoError(t, ws.WriteMessage(message.ws, message.wire))
		ft, r, err := c.NextReader()
		require.NoError(t, err)
		data, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		require.Equal(t, message.frame, ft)
		require.Equal(t, message.body, data)
	}
}

// Covers P17
func TestPollingGracefulClose(t *testing.T) {
	for _, binary := range []bool{false, true} {
		for _, mode := range []string{"pending poll", "next poll", "buffered writer"} {
			t.Run(fmt.Sprintf("binary=%v/%s", binary, mode), func(t *testing.T) {
				s, ts := engineServer(t)
				poll := openPollingMode(t, ts.URL, binary)
				c, err := s.Accept()
				require.NoError(t, err)
				polled := make(chan response, 1)
				closeReceived := false
				if mode == "buffered writer" {
					go func() { polled <- request(http.MethodGet, poll, nil) }()
					w, err := c.NextWriter(eioparser.String)
					require.NoError(t, err)
					_, err = w.Write([]byte("buffered"))
					require.NoError(t, err)
					require.NoError(t, c.Close(), "close starts while the application writer is still open")
					require.Equal(t, engineio.StateClosing, c.ReadyState())
					require.NoError(t, w.Close())
					r := receive(t, polled)
					require.NoError(t, r.err)
					require.Equal(t, http.StatusOK, r.status)
					packets := pollPackets(t, r.body, binary)
					require.NotEmpty(t, packets)
					require.Equal(t, "4buffered", packets[0])
					if len(packets) > 1 {
						require.Equal(t, []string{"4buffered", "1"}, packets)
						closeReceived = true
					}
				} else if mode == "pending poll" {
					go func() { polled <- request(http.MethodGet, poll, nil) }()
					require.Eventually(t, func() bool { return c.(interface{ Writable() bool }).Writable() }, time.Second, time.Millisecond)
				}
				require.NoError(t, c.Close())
				_, err = c.NextWriter(eioparser.String)
				require.Error(t, err)
				if !closeReceived && mode != "pending poll" {
					require.Equal(t, engineio.StateClosing, c.ReadyState())
					require.Contains(t, s.Clients(), c.ID(), "closing SID remains routable")
					go func() { polled <- request(http.MethodGet, poll, nil) }()
				}
				if !closeReceived {
					r := receive(t, polled)
					require.NoError(t, r.err)
					require.Equal(t, http.StatusOK, r.status, "%s", r.body)
					require.Equal(t, []string{"1"}, pollPackets(t, r.body, binary))
				}
				require.Eventually(t, func() bool { return c.ReadyState() == engineio.StateClosed }, time.Second, time.Millisecond)
			})
		}
	}
}

// Covers P18
func TestPollingOverlaps(t *testing.T) {
	t.Run("poll", func(t *testing.T) {
		s, ts := engineServer(t)
		poll := openPolling(t, ts.URL)
		c, err := s.Accept()
		require.NoError(t, err)
		first := make(chan response, 1)
		go func() { first <- request(http.MethodGet, poll, nil) }()
		w, err := c.NextWriter(eioparser.String)
		require.NoError(t, err) // the first FlushOut has acquired its writer
		_, err = w.Write([]byte("first"))
		require.NoError(t, err)
		r := request(http.MethodGet, poll, nil)
		require.NoError(t, r.err)
		require.Equal(t, http.StatusInternalServerError, r.status)
		require.Contains(t, string(r.body), "overlap")
		require.NoError(t, w.Close())
		r = receive(t, first)
		require.NoError(t, r.err)
		require.Equal(t, http.StatusOK, r.status)
		require.Equal(t, []string{"4first"}, textPackets(t, r.body))
	})
	t.Run("data", func(t *testing.T) {
		s, ts := engineServer(t)
		poll := openPolling(t, ts.URL)
		c, err := s.Accept()
		require.NoError(t, err)
		body, feed := io.Pipe()
		t.Cleanup(func() { _ = feed.Close(); _ = body.Close() })
		first := make(chan response, 1)
		go func() { first <- request(http.MethodPost, poll, body) }()
		_, err = feed.Write([]byte("5:4"))
		require.NoError(t, err)
		_, r, err := c.NextReader()
		require.NoError(t, err) // the first FeedIn has supplied the packet header
		second := request(http.MethodPost, poll, strings.NewReader("2:4x"))
		require.NoError(t, second.err)
		require.Equal(t, http.StatusBadRequest, second.status)
		require.Contains(t, string(second.body), "overlap")
		_, err = feed.Write([]byte("data"))
		require.NoError(t, err)
		require.NoError(t, feed.Close())
		data, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		require.Equal(t, "data", string(data))
		result := receive(t, first)
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.status)
		require.Equal(t, "ok", string(result.body))
	})
}

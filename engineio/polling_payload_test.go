package engineio

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// nodeBinaryPayload splits a binary polling payload the way decodePayloadAsBinary of
// engine.io-parser 2.2.1 does: one type byte (0 string, 1 binary), the length as one
// byte per decimal digit, 0xff, then that many bytes.
func nodeBinaryPayload(t *testing.T, b []byte) [][]byte {
	t.Helper()
	var out [][]byte
	for len(b) > 0 {
		end := bytes.IndexByte(b, 0xff)
		require.Greater(t, end, 0, "no length terminator in %v", b)
		n := 0
		for _, d := range b[1:end] {
			require.LessOrEqual(t, d, byte(9), "length digit in %v", b)
			n = n*10 + int(d)
		}
		b = b[end+1:]
		require.LessOrEqual(t, n, len(b), "length %d beyond the payload %q", n, b)
		out = append(out, b[:n])
		b = b[n:]
	}
	return out
}

func TestPollingBinaryPayloadNonASCII(t *testing.T) {
	must := require.New(t)
	should := assert.New(t)

	svr := NewServer()
	defer func() { must.NoError(svr.Close()) }()
	httpSvr := httptest.NewServer(svr)
	defer httpSvr.Close()

	resp, err := http.Get(httpSvr.URL + "/?EIO=3&transport=polling")
	must.NoError(err)
	body, err := io.ReadAll(resp.Body)
	must.NoError(err)
	must.NoError(resp.Body.Close())
	pkts := nodeBinaryPayload(t, body)
	must.Len(pkts, 1)
	must.Equal(byte('0'), pkts[0][0], "OPEN packet expected: %q", pkts[0])
	var open struct {
		SID string `json:"sid"`
	}
	must.NoError(json.Unmarshal(pkts[0][1:], &open))
	poll := httpSvr.URL + "/?EIO=3&transport=polling&sid=" + open.SID

	conn, err := svr.Accept()
	must.NoError(err)
	defer func() { _ = conn.Close() }()

	// Client to server: engine.io-parser 2.2.1 encodePayloadAsBinary of the messages
	// "€👍a" and "ok": the length counts UTF-8 bytes (9), not UTF-16 units (5).
	in := []byte{0x00, 0x09, 0xff, '4', 0xe2, 0x82, 0xac, 0xf0, 0x9f, 0x91, 0x8d, 'a', 0x00, 0x03, 0xff, '4', 'o', 'k'}
	posted := make(chan string, 1)
	go func() {
		resp, err := http.Post(poll, "application/octet-stream", bytes.NewReader(in))
		if err != nil {
			posted <- err.Error()
			return
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		posted <- strconv.Itoa(resp.StatusCode) + " " + string(b)
	}()
	for _, want := range []string{"€👍a", "ok"} {
		ft, r, err := conn.NextReader()
		must.NoError(err)
		should.Equal(eioparser.String, ft)
		b, err := io.ReadAll(r)
		must.NoError(err)
		must.NoError(r.Close())
		should.Equal(want, string(b))
	}
	should.Equal("200 ok", <-posted)

	// Server to client: the bytes engine.io-parser 2.2.1 produces for the message
	// "hello 你好" in a binary payload; the length is 13 bytes, not 9 UTF-16 units.
	go func() {
		w, err := conn.NextWriter(eioparser.String)
		if err == nil {
			_, _ = w.Write([]byte("hello 你好"))
			_ = w.Close()
		}
	}()
	resp, err = http.Get(poll)
	must.NoError(err)
	body, err = io.ReadAll(resp.Body)
	must.NoError(err)
	must.NoError(resp.Body.Close())
	want := append([]byte{0x00, 0x01, 0x03, 0xff, '4'}, "hello 你好"...)
	should.Equal(want, body)
}

package socketio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// maxRecordValue is the bound the logger package puts on a string or error value.
const maxRecordValue = 1024

// TestPeerFloodLogsNoWarn sends, on one websocket connection of a server with OnError
// registered on "/", CONNECT packets for namespaces the server does not have, among them
// names of 1024, 1025 and 100,001 bytes, and ERROR packets with a 100,000-byte message to
// an unknown namespace and to "/". The server answers each name of at most 1024 bytes
// with ERROR "Invalid namespace" for that namespace, as socket.io 2.5.0 does, repeating
// the name as received even where the reference rewrites it, answers nothing for a
// longer one, logs every packet at DEBUG only, calls OnError of "/" for
// the ERROR packet sent to "/" only, and keeps every attribute value within the
// logger's bound.
func TestPeerFloodLogsNoWarn(t *testing.T) {
	rec, opts := captureLogs(t, nil)
	srv := NewServer(WithEngineOptions(opts...), WithLogger(slog.New(rec)))
	var rootErrors atomic.Int32
	srv.OnConnect("/", func(Conn) error { return nil })
	srv.OnError("/", func(Conn, error) { rootErrors.Add(1) })
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	d := engineclient.Dialer{Transports: []transport.Transport{websocket.Default}}
	conn, err := d.Dial(ts.URL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	c := &rawClient{t: t, conn: conn}
	require.Equal(t, "0", c.read(), "root namespace CONNECT")

	const n = 200
	for i := 0; i < n; i++ {
		c.send(fmt.Sprintf("0/bogus%d,", i))
		require.Equal(t, fmt.Sprintf("4/bogus%d,\"Invalid namespace\"\n", i), c.read())
	}
	c.send("0/bogus?x=1,")
	require.Equal(t, "4/bogus,\"Invalid namespace\"\n", c.read(), "the query is not echoed")
	edge := "/" + strings.Repeat("b", maxEchoedNamespace-1) // the bound counts the "/"
	c.send("0" + edge + ",")
	require.Equal(t, clip("4"+edge+",\"Invalid namespace\"\n"), clip(c.read()), "a 1024-byte name is answered")
	c.send("0" + edge + "b,")
	long := strings.Repeat("e", 100_000)
	c.send("0/" + long + ",")
	c.send("0/after,")
	assert.Equal(t, "4/after,\"Invalid namespace\"\n", clip(c.read()), "the 1025-byte and oversized names are not answered")
	// socket.io 2.5.0 rewrites these names as a URL path; they are repeated as received.
	verbatim := []string{"/a b", "/a\tb", "/a\rb", "/a\nb", "/a ", "/a\n", "/a#b", `/a\b`}
	for _, name := range verbatim {
		c.send("0" + name + ",")
		assert.Equal(t, "4"+name+",\"Invalid namespace\"\n", c.read())
	}
	c.send("4/nope,\"" + long + "\"")
	c.send(`4"` + long + `"`)

	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { return len(only(rec.since(0), "engineio: session close")) == 1 },
		waitFor, time.Millisecond, "the session close record")
	recs := rec.since(0)
	for _, m := range loud(recs, "") {
		assert.Fail(t, "a record above DEBUG", "%s %s nsp=%s err=%s", m["level"], m["msg"], clip(m["nsp"]), clip(m["err"]))
	}
	require.Equal(t, n+5+len(verbatim), len(only(recs, "socketio: connect without handler")))
	for _, m := range recs {
		for key, value := range m {
			assert.LessOrEqual(t, len(value), maxRecordValue, "%s of %q", key, m["msg"])
		}
	}
	require.Equal(t, "/"+long[:maxRecordValue-4]+"...", only(recs, "socketio: connect without handler")[n+3]["nsp"])
	unhandled := only(recs, "socketio: unhandled error")
	require.Equal(t, 1, len(unhandled), "the ERROR packet for /nope reaches no OnError")
	require.Equal(t, "/nope", unhandled[0]["nsp"])
	require.Equal(t, long[:maxRecordValue-3]+"...", unhandled[0]["err"])
	require.Equal(t, int32(1), rootErrors.Load(), "only the ERROR packet for / reaches OnError")
}

// TestUnknownNamespaceOverflowOverPolling sends, in one POST of a polling session that
// never polls again, more CONNECTs to namespaces the server does not have than the
// outbound queue holds ERROR replies. The reply that overflows the queue is the root
// namespace's: OnError of "/" receives ErrWriteBufferFull once when it is registered,
// and the overflow is logged at DEBUG otherwise. No record is above DEBUG.
func TestUnknownNamespaceOverflowOverPolling(t *testing.T) {
	for _, withOnError := range []bool{true, false} {
		t.Run(fmt.Sprintf("OnError=%v", withOnError), func(t *testing.T) {
			rec, opts := captureLogs(t, nil)
			srv := NewServer(WithEngineOptions(opts...), WithLogger(slog.New(rec)))
			rootErrors := make(chan error, 8)
			srv.OnConnect("/", func(Conn) error { return nil })
			if withOnError {
				srv.OnError("/", func(c Conn, err error) {
					assert.Equal(t, "/", c.Namespace())
					rootErrors <- err
				})
			}
			go func() { _ = srv.Serve() }()
			t.Cleanup(func() { _ = srv.Close() })
			ts := httptest.NewServer(srv)
			t.Cleanup(ts.Close)

			resp, err := http.Get(ts.URL + "/?EIO=3&transport=polling")
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			require.NoError(t, err)
			var params struct {
				SID string `json:"sid"`
			}
			require.NoError(t, json.Unmarshal(body[bytes.IndexByte(body, '{'):bytes.LastIndexByte(body, '}')+1], &params))

			var payload strings.Builder
			for i := 0; i < defaultWriteBufferSize+100; i++ {
				packet := fmt.Sprintf("40/z%d,", i)
				fmt.Fprintf(&payload, "%d:%s", len(packet), packet)
			}
			resp, err = http.Post(ts.URL+"/?EIO=3&transport=polling&sid="+params.SID, "text/plain;charset=UTF-8",
				strings.NewReader(payload.String()))
			require.NoError(t, err)
			_ = resp.Body.Close()

			overflow := func() []map[string]string {
				var out []map[string]string
				for _, m := range only(rec.since(0), "socketio: unhandled error") {
					if m["err"] == ErrWriteBufferFull.Error() {
						out = append(out, m)
					}
				}
				return out
			}
			if withOnError {
				require.ErrorIs(t, recv(t, rootErrors, "the overflow report"), ErrWriteBufferFull)
			} else {
				require.Eventually(t, func() bool { return len(overflow()) == 1 }, waitFor, time.Millisecond,
					"the overflow record")
				require.Equal(t, "DEBUG", overflow()[0]["level"])
			}
			require.Eventually(t, func() bool { return len(only(rec.since(0), "engineio: session close")) == 1 },
				waitFor, time.Millisecond, "the session close record")
			require.Empty(t, drain(rootErrors), "a second OnError call")
			for _, m := range loud(rec.since(0), "") {
				assert.Fail(t, "a record above DEBUG", "%s %s nsp=%s err=%s", m["level"], m["msg"], m["nsp"], m["err"])
			}
			if withOnError {
				require.Empty(t, overflow(), "the overflow reached OnError and was logged")
			}
		})
	}
}

// clip returns s, or its first 64 bytes and its length if it is longer.
func clip(s string) string {
	if len(s) <= 64 {
		return s
	}
	return fmt.Sprintf("%s... (%d bytes)", s[:64], len(s))
}

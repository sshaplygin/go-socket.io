package socketio

import (
	"fmt"
	"log/slog"
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
// registered on "/", CONNECT packets for namespaces the server does not have, one with a
// 100,000-byte name, and ERROR packets with a 100,000-byte message. The server answers
// each ordinary name with ERROR "Invalid namespace" for that namespace, as socket.io 2.5.0
// does, answers nothing for the oversized name, logs every packet at DEBUG only, calls no
// OnError, and keeps every attribute value within the logger's bound.
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
	long := strings.Repeat("e", 100_000)
	c.send("0/" + long + ",")
	c.send("0/after,")
	assert.Equal(t, "4/after,\"Invalid namespace\"\n", clip(c.read()), "the oversized name is not answered")
	c.send("4/nope,\"" + long + "\"")
	c.send(`4"` + long + `"`)

	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { return len(only(rec.since(0), "engineio: session close")) == 1 },
		waitFor, time.Millisecond, "the session close record")
	recs := rec.since(0)
	for _, m := range loud(recs, "") {
		assert.Fail(t, "a record above DEBUG", "%s %s nsp=%s err=%s", m["level"], m["msg"], clip(m["nsp"]), clip(m["err"]))
	}
	require.Equal(t, n+3, len(only(recs, "socketio: connect without handler")))
	for _, m := range recs {
		for key, value := range m {
			assert.LessOrEqual(t, len(value), maxRecordValue, "%s of %q", key, m["msg"])
		}
	}
	require.Equal(t, "/"+long[:maxRecordValue-4]+"...", only(recs, "socketio: connect without handler")[n+1]["nsp"])
	unhandled := only(recs, "socketio: unhandled error")
	require.Equal(t, 1, len(unhandled), "the ERROR packet for /nope reaches no OnError")
	require.Equal(t, "/nope", unhandled[0]["nsp"])
	require.Equal(t, long[:maxRecordValue-3]+"...", unhandled[0]["err"])
	require.Equal(t, int32(1), rootErrors.Load(), "only the ERROR packet for / reaches OnError")
}

// clip returns s, or its first 64 bytes and its length if it is longer.
func clip(s string) string {
	if len(s) <= 64 {
		return s
	}
	return fmt.Sprintf("%s... (%d bytes)", s[:64], len(s))
}

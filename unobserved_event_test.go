package socketio

import (
	"encoding/json"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/parser"
)

// unobservedPayload is the 999,006-byte event 2["e",0,0,…] of 499,500 arguments.
var unobservedPayload = `2["e",` + strings.Repeat("0,", 499_499) + `0]`

// newDecodeConnection returns a connection whose root namespace has no listeners
// and reads frames: the first one as text, the others as binary attachments.
func newDecodeConnection(frames ...string) (*connection, *namespaceConn) {
	data := make([][]byte, len(frames))
	for i, frame := range frames {
		data[i] = []byte(frame)
	}
	c := &connection{
		handlers:   newNamespaceRegistry(),
		namespaces: newNamespaces(),
		decoder:    parser.NewDecoder(&fakeReader{data: data}),
	}
	c.handlers.Set(rootNamespace, newNamespaceHandler())
	nc := newNamespaceConn(c, aliasRootNamespace, nil)
	c.namespaces.Set(rootNamespace, nc)
	return c, nc
}

// handleEvent reads and handles one packet and reports the bytes allocated.
func handleEvent(t *testing.T, c *connection) uint64 {
	t.Helper()
	var header parser.Header
	var event string
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	require.NoError(t, c.decoder.DecodeHeader(&header, &event))
	err := eventPacketHandler(c, event, header)
	runtime.ReadMemStats(&after)
	require.NoError(t, err)
	return after.TotalAlloc - before.TotalAlloc
}

func TestUnobservedEventArgumentsAreNotDecoded(t *testing.T) {
	require.Len(t, unobservedPayload, 999_006)
	// The smallest of three runs excludes allocations of unrelated goroutines.
	least := uint64(math.MaxUint64)
	for i := 0; i < 3; i++ {
		c, _ := newDecodeConnection(unobservedPayload)
		least = min(least, handleEvent(t, c))
	}
	// DecodeHeader alone allocates about 70,000,000 B to read and split the array.
	require.Less(t, least, uint64(100_000_000), "bytes allocated by an event without listeners or middleware")
}

func TestUnobservedEventReachesMiddlewareDecoded(t *testing.T) {
	c, nc := newDecodeConnection(`51-["unobserved",1,"two",{"_placeholder":true,"num":0}]`, "three")
	var got []interface{}
	nc.Use(func(event string, args []interface{}) error {
		got = append([]interface{}{event}, args...)
		return nil
	})
	handleEvent(t, c)
	require.Equal(t, []interface{}{"unobserved", float64(1), "two", &parser.Buffer{Data: []byte("three")}}, got)
}

// requireNoReport fails if OnError has received an error. OnError runs on the
// dispatcher, so it has returned for every packet before a later reply arrives.
func requireNoReport(t *testing.T, errs <-chan error) {
	t.Helper()
	select {
	case err := <-errs:
		t.Fatalf("OnError: %v", err)
	default:
	}
}

// TestUnobservedEventObservableBehavior pins what a peer and OnError observe for an
// event without listeners: no ACK, consumed attachments and an open connection.
func TestUnobservedEventObservableBehavior(t *testing.T) {
	errs := make(chan error, 8)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnError("/", func(_ Conn, err error) { errs <- err })
		s.OnEvent("/", "alive", func(Conn) string { return "alive" })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	attach := func(data string) {
		w, err := c.conn.NextWriter(eioparser.Binary)
		require.NoError(t, err)
		_, err = w.Write([]byte(data))
		require.NoError(t, err)
		require.NoError(t, w.Close())
	}
	c.send(`21["unobserved",1]`)
	c.send(`51-2["unobserved",{"_placeholder":true,"num":0}]`)
	attach("bytes")
	c.send(`51-["unobserved",{"_placeholder":true,"num":0}]`)
	attach("bytes")
	c.send(`23["alive"]`)
	require.Equal(t, "33[\"alive\"]\n", c.read(), "an ACK for an unobserved event or an unconsumed attachment came first")
	requireNoReport(t, errs)

	// A text frame in place of an attachment is reported and ends the connection.
	c.send(`51-4["unobserved",{"_placeholder":true,"num":0}]`)
	c.send(`2["alive"]`)
	require.EqualError(t, recv(t, errs, "OnError"), "buffer packet should be binary")
	require.True(t, c.readClosed())
}

// TestUnobservedEventOutOfRangeNumber: an argument value that is valid JSON but
// does not decode into interface{} is reported only when a listener decodes it.
func TestUnobservedEventOutOfRangeNumber(t *testing.T) {
	errs := make(chan error, 8)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnError("/", func(_ Conn, err error) { errs <- err })
		s.OnEvent("/", "listened", func(Conn, interface{}) { t.Error("listener ran") })
		s.OnEvent("/", "alive", func(Conn) string { return "alive" })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send(`2["e",1e400]`)
	c.send(`21["alive"]`)
	require.Equal(t, "31[\"alive\"]\n", c.read())
	requireNoReport(t, errs)
	c.send(`2["listened",1e400]`)
	var typeErr *json.UnmarshalTypeError
	require.ErrorAs(t, recv(t, errs, "OnError"), &typeErr)
	require.Equal(t, "number 1e400", typeErr.Value)
	c.send(`22["alive"]`)
	require.Equal(t, "32[\"alive\"]\n", c.read())
}

func BenchmarkUnobservedEvent(b *testing.B) {
	for _, bc := range []struct{ name, payload string }{
		{"args", unobservedPayload},
		{"nested", `2["unobserved",[` + strings.Repeat("0,", 499_990) + `0]]`},
	} {
		b.Run(bc.name, func(b *testing.B) {
			b.SetBytes(int64(len(bc.payload)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				c, _ := newDecodeConnection(bc.payload)
				b.StartTimer()
				var header parser.Header
				var event string
				if err := c.decoder.DecodeHeader(&header, &event); err != nil {
					b.Fatal(err)
				}
				if err := eventPacketHandler(c, event, header); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

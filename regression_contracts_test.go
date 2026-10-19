package socketio

import (
	"errors"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/require"
	"testing"
)

// Covers: G20
func TestVariadicPayloadAndAckCallbacks(t *testing.T) {
	acked := make(chan []string, 1)
	_, url := defectServer(t, func(s *Server) {
		s.OnEvent("/", "sum", func(_ Conn, prefix string, values ...int) string {
			require.Equal(t, []int{1, 2, 3}, values)
			return prefix
		})
		s.OnEvent("/", "ask", func(c Conn) { c.Emit("question", func(values ...string) { acked <- values }) })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send(`21["sum","result",1,2,3]`)
	require.Equal(t, "31[\"result\"]\n", c.read())
	c.send(`2["ask"]`)
	require.Equal(t, "21[\"question\"]\n", c.read())
	c.send(`31["a","b"]`)
	require.Equal(t, []string{"a", "b"}, recv(t, acked, "variadic ACK"))
}

// Covers: G9, G19, G21
func TestIgnoredBinaryEventsConsumeTheirAttachments(t *testing.T) {
	_, url := defectServer(t, func(s *Server) { s.OnEvent("/", "alive", func(Conn) string { return "alive" }) })
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	for _, packet := range []string{
		`51-/missing,["unknown",{"_placeholder":true,"num":0}]`,
		`51-["unknown",{"_placeholder":true,"num":0}]`,
		`61-99[{"_placeholder":true,"num":0}]`,
		`51-["disconnecting",{"_placeholder":true,"num":0}]`,
	} {
		c.send(packet)
		w, err := c.conn.NextWriter(eioparser.Binary)
		require.NoError(t, err)
		_, err = w.Write([]byte("attachment"))
		require.NoError(t, err)
		require.NoError(t, w.Close())
		c.send(`27["alive"]`)
		require.Equal(t, "37[\"alive\"]\n", c.read())
	}
}

// Covers: G28
func TestReservedEmitCallsLocalListenersOnce(t *testing.T) {
	seen := make(chan string, 4)
	_, url := defectServer(t, func(s *Server) {
		s.Of("/").On("disconnecting", func(c Conn, reason string) { seen <- reason; c.Emit("disconnecting", "recursive") })
		s.OnEvent("/", "trigger", func(c Conn) { c.Emit("disconnecting", "local"); c.Emit("marker", c.Connected()) })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send(`2["trigger"]`)
	require.Equal(t, "2[\"marker\",true]\n", c.read())
	require.Equal(t, "local", recv(t, seen, "local reserved listener"))
	require.Empty(t, seen)
}

// Covers: G6
func TestEmitSnapshotsBinaryAndNestedPayload(t *testing.T) {
	_, c, fc, _ := connWithOptions(t, 'S', nil)
	fc.hold.Store(true)
	c.Emit("barrier")
	recv(t, fc.held, "held writer")
	payload := map[string]interface{}{"items": []interface{}{map[string]string{"value": "before"}}}
	c.Emit("nested", payload)
	payload["items"].([]interface{})[0].(map[string]string)["value"] = "after"
	// Binary payload preparation also owns bytes when pending behind another frame.
	binary := parser.Buffer{Data: []byte("before")}
	c.Emit("binary", binary)
	copy(binary.Data, "after!")
	fc.hold.Store(false)
	close(fc.release)
	recv(t, fc.out, "barrier")
	require.Contains(t, recv(t, fc.out, "nested"), `"before"`)
	require.Contains(t, recv(t, fc.out, "binary header"), `_placeholder`)
	require.Equal(t, "before", recv(t, fc.out, "binary attachment"))
}

// Covers: G4, G18
func TestRejectedAdmissionDuringCloseDoesNotConnect(t *testing.T) {
	entered, release := make(chan Conn, 1), make(chan struct{})
	s := NewServer()
	fc := newFakeConn(t)
	s.OnConnect("/", func(c Conn) error { entered <- c; <-release; return errors.New("reject") })
	done := make(chan struct{})
	go func() { s.serveConn(fc); close(done) }()
	c := recv(t, entered, "admission")
	require.NoError(t, c.Close())
	close(release)
	recv(t, done, "startup completed")
	require.Empty(t, drain(fc.out), "rejected admission must not send CONNECT or DISCONNECT")
	require.NoError(t, s.Close())
}

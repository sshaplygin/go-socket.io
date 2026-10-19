package socketio

import (
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/require"
	"testing"
)

// startRoom serves `members` connections in room "r" and returns the frames each receives.
func startRoom(t *testing.T, members int) (*Server, []<-chan string) {
	t.Helper()
	p := start(t, 'S', hooks{connect: func(c Conn) error { c.Join("r"); return nil }})
	outs := []<-chan string{p.fc.out}
	for i := 1; i < members; i++ {
		fc := newFakeConn(t)
		p.srv.serveConn(fc)
		require.Equal(t, "0", recv(t, fc.out, "the CONNECT of a member"))
		closeAtEnd(t, fc, serverCompletion(p.srv, recv(t, p.conns, "OnConnect of a member").EngineConn().ID()))
		outs = append(outs, fc.out)
	}
	return p.srv, outs
}

// A *parser.Buffer and plain JSON values in the args of a broadcast (to the room and to the
// namespace, alternately) reach the encoder of every member on its own write goroutine. Under
// -race neither the encoders nor the broadcast path may write to them, and every member must
// get the frames that a single connection gets.
func TestBroadcastSharedArgs(t *testing.T) {
	const members, rounds = 4, 20
	mk := func() []interface{} {
		return []interface{}{&parser.Buffer{Data: []byte{1, 2, 3}}, map[string]interface{}{"k": []interface{}{1, "two"}}}
	}

	srv, outs := startRoom(t, members)
	args := mk()
	for i := 0; i < rounds; i++ {
		if i%2 == 0 {
			srv.BroadcastToRoom("/", "r", "bin", args...)
		} else {
			srv.BroadcastToNamespace("/", "bin", args...)
		}
	}

	for m, out := range outs {
		for i := 0; i < rounds; i++ {
			require.Equal(t, "51-[\"bin\",{\"_placeholder\":true,\"num\":0},{\"k\":[1,\"two\"]}]\n", recv(t, out, "the text frame"), "member %d", m)
			require.Equal(t, "\x01\x02\x03", recv(t, out, "the binary frame"), "member %d", m)
		}
	}
	require.Equal(t, mk(), args, "the broadcast wrote to its arguments")
}

// Covers G6
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

// Covers G6
func TestRegressionG6EmitSnapshotsMap(t *testing.T) {
	_, c, fc, _ := connWithOptions(t, 'S', nil)
	fc.hold.Store(true)
	c.Emit("barrier")
	recv(t, fc.held, "writer blocked on previous packet")
	value := map[string]string{"value": "before"}
	c.Emit("snapshot", value)
	value["value"] = "after"
	fc.hold.Store(false)
	close(fc.release)
	recv(t, fc.out, "barrier packet")
	require.Contains(t, recv(t, fc.out, "snapshot packet"), `"value":"before"`)
}

// Covers B1
func TestEmitNil(t *testing.T) {
	_, url := defectServer(t, func(s *Server) {
		s.OnEvent("/", "get-nil", func(c Conn) { c.Emit("nil", nil); c.Emit("pair", nil, "tail") })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send(`2["get-nil"]`)
	require.Equal(t, "2[\"nil\",null]\n", c.read())
	require.Equal(t, "2[\"pair\",null,\"tail\"]\n", c.read())
}

package socketio

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/parser"
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
		closeAtEnd(t, fc, recv(t, p.conns, "OnConnect of a member").(*namespaceConn).conn)
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

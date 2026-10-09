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

// One *parser.Buffer in the args of a broadcast (to the room and to the namespace, alternately) reaches the encoder of every member of the
// room, each on its own write goroutine. Under -race the encoders must not write to it, and
// every member must get the bytes that a single connection gets.
func TestBroadcastSharedBuffer(t *testing.T) {
	const members, rounds = 4, 20

	srv, outs := startRoom(t, members)
	payload := []byte{1, 2, 3}
	shared := &parser.Buffer{Data: payload}
	for i := 0; i < rounds; i++ {
		if i%2 == 0 {
			srv.BroadcastToRoom("/", "r", "bin", shared)
		} else {
			srv.BroadcastToNamespace("/", "bin", shared)
		}
	}

	for m, out := range outs {
		for i := 0; i < rounds; i++ {
			require.Equal(t, "51-[\"bin\",{\"_placeholder\":true,\"num\":0}]\n", recv(t, out, "the text frame"), "member %d", m)
			require.Equal(t, string(payload), recv(t, out, "the binary frame"), "member %d", m)
		}
	}
	require.Equal(t, parser.Buffer{Data: []byte{1, 2, 3}}, *shared, "the broadcast wrote to its argument")
}

// The same for arguments without a Buffer: a broadcast must not write to them either.
func TestBroadcastSharedArgs(t *testing.T) {
	const members, rounds = 4, 20

	mk := func() []interface{} {
		return []interface{}{map[string]interface{}{"k": []interface{}{1, "two"}}, []string{"x", "y"}}
	}
	srv, outs := startRoom(t, members)
	shared := mk()
	for i := 0; i < rounds; i++ {
		srv.BroadcastToRoom("/", "r", "json", shared...)
	}

	for m, out := range outs {
		for i := 0; i < rounds; i++ {
			require.Equal(t, "2[\"json\",{\"k\":[1,\"two\"]},[\"x\",\"y\"]]\n", recv(t, out, "the text frame"), "member %d", m)
		}
	}
	require.Equal(t, mk(), shared, "the broadcast wrote to its arguments")
}

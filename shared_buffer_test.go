package socketio

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/parser"
)

// One *parser.Buffer in the args of a broadcast reaches the encoder of every member of the
// room, each on its own write goroutine. Under -race the encoders must not write to it, and
// every member must get the bytes that a single connection gets.
func TestBroadcastSharedBuffer(t *testing.T) {
	const members, rounds = 4, 20

	p := start(t, 'S', hooks{connect: func(c Conn) error { c.Join("r"); return nil }})
	outs := []<-chan string{p.fc.out}
	for i := 1; i < members; i++ {
		fc := newFakeConn(t)
		p.srv.serveConn(fc)
		require.Equal(t, "0", recv(t, fc.out, "the CONNECT of a member"))
		recv(t, p.conns, "OnConnect of a member")
		outs = append(outs, fc.out)
	}

	payload := []byte{1, 2, 3}
	shared := &parser.Buffer{Data: payload}
	for i := 0; i < rounds; i++ {
		p.srv.BroadcastToRoom("/", "r", "bin", shared)
	}

	for m, out := range outs {
		for i := 0; i < rounds; i++ {
			require.Equal(t, "51-[\"bin\",{\"_placeholder\":true,\"num\":0}]\n", recv(t, out, "the text frame"), "member %d", m)
			require.Equal(t, string(payload), recv(t, out, "the binary frame"), "member %d", m)
		}
	}
}

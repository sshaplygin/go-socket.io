package client

import (
	"testing"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/stretchr/testify/require"
)

func TestConnectFromServerDisconnectStartsNewLifecycle(t *testing.T) {
	p := start(t, 'C', hooks{})
	next := newFakeConn(t)
	dialed := make(chan struct{}, 1)
	p.cl.dial = func(string) (engineclient.Conn, error) { dialed <- struct{}{}; return next, nil }
	result := make(chan error, 1)
	p.cl.OnDisconnect(func(_ Conn, reason string) {
		// Only the server DISCONNECT starts a new lifecycle; cleanup must not.
		if reason == "io server disconnect" {
			result <- p.cl.Connect()
		}
	})
	p.send(t, "1")
	require.NoError(t, recv(t, result, "explicit Connect in OnDisconnect"))
	replacement := p.conn()
	closeAtEnd(t, next, replacement)
	recv(t, p.fc.closed, "old engine close")
	recv(t, dialed, "replacement dial")
	next.reads <- "0"
	recv(t, p.conns, "replacement CONNECT")

	require.NoError(t, p.cl.Close())
	recv(t, next.closed, "replacement engine close")
	recv(t, replacement.readStopped, "replacement reader shutdown")
	recv(t, replacement.writeStopped, "replacement writer shutdown")
	recv(t, replacement.errorStopped, "replacement error worker shutdown")
	require.Empty(t, dialed, "cleanup must not start another lifecycle")
}

func TestLateAckCannotOutliveNamespace(t *testing.T) {
	acks := make(chan AckFunc, 1)
	p := start(t, 'C', hooks{events: map[string]interface{}{
		"ask":     func(_ Conn, ack AckFunc) { acks <- ack },
		"barrier": func(Conn) string { return "ready" },
	}}, "/chat")
	p.send(t, `2/chat,0["ask"]`)
	ack := recv(t, acks, "deferred ACK")
	p.send(t, "1/chat,")
	require.Equal(t, "/chat", recv(t, p.discs, "namespace disconnect"))
	ack("stale")
	p.send(t, `27["barrier"]`)
	require.Equal(t, "37[\"ready\"]\n", recv(t, p.fc.out, "only live namespace ACK"))
}

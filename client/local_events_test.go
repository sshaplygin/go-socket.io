package client

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/internal/event"
)

// localEvents are the names that socket.io-client 2.5.0 and 1.7.4 keep local
// (lib/socket.js events), plus disconnecting, a lifecycle name of the server role.
var localEvents = []string{
	"connect", "connect_error", "connect_timeout", "connecting",
	"disconnect", "disconnecting", "error",
	"reconnect", "reconnect_attempt", "reconnect_error", "reconnect_failed", "reconnecting",
	"ping", "pong",
}

type localCall struct {
	conn Conn
	arg  string
}

// listenLocal registers a listener of every local name on the Socket of nsp.
func listenLocal(p *peer, nsp string, calls chan<- localCall) {
	for _, name := range localEvents {
		p.cl.Socket(nsp).On(name, func(c Conn, arg string) { calls <- localCall{c, arg} })
	}
}

func TestClientLocalEventsStayLocal(t *testing.T) {
	p := start(t, 'C', hooks{}, "/chat")
	chat := p.extra["/chat"]
	calls := make(chan localCall, 16)
	listenLocal(p, "/", calls)
	listenLocal(p, "/chat", calls)
	sends := []struct {
		name string
		conn Conn
		emit func(event string, args ...interface{})
	}{
		{"Client.Emit", p.nc, p.cl.Emit},
		{"Socket.Emit", p.nc, p.cl.Socket("/").Emit},
		{"Conn.Emit", p.nc, p.nc.Emit},
		{"Conn.Volatile.Emit", p.nc, p.nc.Volatile().Emit},
		{"Conn.Compress.Emit", p.nc, p.nc.Compress(false).Emit},
		{"Conn.Volatile.Compress.Emit", p.nc, p.nc.Volatile().Compress(false).Emit},
		{"namespace Socket.Emit", chat, p.cl.Socket("/chat").Emit},
		{"namespace Conn.Emit", chat, chat.Emit},
		{"namespace Conn.Volatile.Emit", chat, chat.Volatile().Emit},
	}
	for _, send := range sends {
		for _, name := range localEvents {
			send.emit(name, send.name+" "+name)
			call := recv(t, calls, send.name+" "+name+" local listener")
			require.Same(t, send.conn, call.conn, "%s %s", send.name, name)
			require.Equal(t, send.name+" "+name, call.arg)
		}
	}
	require.Empty(t, calls)
	expectBeforeBarrier(t, p)
}

func TestClientLocalEventsKeepAcknowledgementsLocal(t *testing.T) {
	p := start(t, 'C', hooks{})
	acks := make(chan string, 1)
	p.cl.Socket("/").On("error", func(_ Conn, arg string, ack func(string)) { ack(arg) })
	for _, emit := range []func(string, ...interface{}){p.nc.Emit, p.cl.Socket("/").Emit, p.nc.Volatile().Emit} {
		emit("error", "boom", func(value string) { acks <- value })
		require.Equal(t, "boom", recv(t, acks, "acknowledgement passed to the local listener"))
	}
	// No local emit took an ACK ID: the first one, 1, goes to this event.
	p.nc.Emit("barrier", func() {})
	require.Equal(t, "21[\"barrier\"]\n", recv(t, p.fc.out, "barrier"))
}

func TestClientLocalEventsAreNotBufferedBeforeConnect(t *testing.T) {
	p := newPeer(t, 'C', hooks{}, "/chat")
	calls := make(chan localCall, 4)
	listenLocal(p, "/", calls)
	listenLocal(p, "/chat", calls)
	for _, nsp := range []string{"/", "/chat"} {
		for _, name := range localEvents {
			p.cl.Socket(nsp).Emit(name, "early")
		}
		p.cl.Socket(nsp).Emit("queued", nsp)
	}
	p.connect(t)
	require.Equal(t, ev("queued", "/"), recv(t, p.fc.out, "buffered root event"))
	require.Equal(t, "2/chat,[\"queued\",\"/chat\"]\n", recv(t, p.fc.out, "buffered namespace event"))
	expectBeforeBarrier(t, p)
	require.Empty(t, calls, "a Socket without a namespace connection has no Conn for its listeners")
}

func TestClientLocalEventListenerCannotRecurse(t *testing.T) {
	p := start(t, 'C', hooks{})
	calls := make(chan string, 4)
	p.cl.Socket("/").On("disconnecting", func(c Conn, reason string) {
		calls <- reason
		c.Emit("disconnecting", "recursive")
	})
	p.nc.Emit("disconnecting", "local")
	require.Equal(t, "local", recv(t, calls, "local listener"))
	require.Empty(t, calls)
	require.True(t, p.nc.Connected())
	expectBeforeBarrier(t, p)
}

func TestClientLocalEventListenerErrorReachesOnError(t *testing.T) {
	p := start(t, 'C', hooks{})
	p.cl.Socket("/").On("error", func(Conn, int) {})
	p.nc.Emit("error", "not a number")
	require.Error(t, recv(t, p.errs, "listener error").err)
	expectBeforeBarrier(t, p)
}

func TestClientTypedSendsRejectLocalEvents(t *testing.T) {
	p := start(t, 'C', hooks{})
	nc := p.nc.(*namespaceConn)
	socket := p.cl.Socket("/")
	ctx := context.Background()
	for _, name := range localEvents {
		require.ErrorIs(t, nc.SendEvent(ctx, name, event.ValueOf(1)), event.ErrInvalidEventName, name)
		require.ErrorIs(t, socket.SendEvent(ctx, name, event.ValueOf(1)), event.ErrInvalidEventName, name)
	}
	expectBeforeBarrier(t, p)
}

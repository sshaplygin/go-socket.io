package socketio

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// A handler serves one namespace, but it runs on the reader of its client's Engine.IO
// connection. While a /chat handler blocks, an event of the same client's root namespace
// is not dispatched, and the ping timeout that follows ends both namespaces.
func TestBlockingHandlerStallsOtherNamespaces(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			var rootRan atomic.Bool
			var once sync.Once
			started, release := make(chan struct{}), make(chan struct{})
			chatReady, chatReason := make(chan struct{}, 1), make(chan string, 1)
			p := newHeartbeatPair(t, tr, 100*time.Millisecond, 300*time.Millisecond, func(p *heartbeatPair) {
				p.srv.OnConnect("/chat", func(Conn) error { return nil })
				p.srv.OnEvent("/chat", "work", func(Conn) {
					close(started)
					<-release
				})
				p.srv.OnEvent("/", "probe", func(Conn) { rootRan.Store(true) })
				chat := p.cl.Socket("/chat")
				chat.OnConnect(func(client.Conn) error { chatReady <- struct{}{}; return nil })
				chat.OnDisconnect(func(_ client.Conn, reason string) { chatReason <- reason })
			})
			t.Cleanup(func() { once.Do(func() { close(release) }) }) // before the server closes
			recv(t, p.conns, "the root OnConnect")
			recv(t, chatReady, "the /chat OnConnect")
			p.cl.Socket("/chat").Emit("work")
			recv(t, started, "the /chat handler to start")
			p.cl.Emit("probe")
			// The client gives up only after it has waited longer than the heartbeat window
			// for a PONG, and the root event was sent before that wait began.
			require.Equal(t, "ping timeout", recv(t, p.clientReason, "the root OnDisconnect of the client"))
			require.Equal(t, "ping timeout", recv(t, chatReason, "the /chat OnDisconnect of the client"))
			require.False(t, rootRan.Load(), "the root handler ran while the /chat handler blocked")
			once.Do(func() { close(release) })
			recv(t, p.serverReasons, "the server's OnDisconnect")
			require.Empty(t, drain(p.errs), "OnError was called")
		})
	}
}

// The Go client reads all its namespaces on one reader as well: while a /chat callback
// blocks, a root event sent to the same client is not dispatched and no PONG is read.
func TestBlockingClientHandlerStallsOtherNamespaces(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			var blocked blockedClient
			var rootRan atomic.Bool
			var once sync.Once
			started, release, chats := make(chan struct{}), make(chan struct{}), make(chan Conn, 1)
			p := newHeartbeatPair(t, tr, 100*time.Millisecond, 300*time.Millisecond, func(p *heartbeatPair) {
				p.srv.OnConnect("/chat", func(c Conn) error { chats <- c; return nil })
				blocked.observe(p.cl)
				p.cl.OnEvent("probe", func(client.Conn) { rootRan.Store(true) })
				p.cl.Socket("/chat").OnEvent("work", func(client.Conn) {
					blocked.block()
					close(started)
					<-release
				})
			})
			t.Cleanup(func() { once.Do(func() { close(release) }) }) // before the client closes
			root := recv(t, p.conns, "the root OnConnect")
			recv(t, chats, "the /chat OnConnect").Emit("work")
			recv(t, started, "the /chat callback to start")
			root.Emit("probe")
			blocked.pastWindow(t, p)
			require.False(t, rootRan.Load(), "the root callback ran while the /chat callback blocked")
			require.Zero(t, blocked.pongs.Load(), "the client read a PONG while its callback ran")
			once.Do(func() { close(release) })
			require.Empty(t, drain(p.errs), "OnError was called")
		})
	}
}

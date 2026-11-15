package client_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// rejoinServer serves / and /chat; "kick" disconnects the sender's namespace.
func rejoinServer(t *testing.T) (url string, connects chan socketio.Conn, received chan string) {
	connects, received = make(chan socketio.Conn, 8), make(chan string, 8)
	ts := newTestServer(t, func(srv *socketio.Server) {
		for _, nsp := range []string{"/", "/chat"} {
			srv.OnConnect(nsp, func(c socketio.Conn) error { connects <- c; return nil })
			srv.OnEvent(nsp, "kick", func(c socketio.Conn) { c.Disconnect() })
			srv.OnEvent(nsp, "echo", func(c socketio.Conn, value string) string {
				received <- c.Namespace() + ":" + value
				return value
			})
		}
	}, steadyPing...)
	return ts.URL, connects, received
}

func TestSocketConnectRejoinsNamespace(t *testing.T) {
	for _, tr := range []struct {
		name      string
		transport transport.Transport
	}{{"polling", polling.Default}, {"websocket", websocket.Default}} {
		for _, by := range []string{"server", "client"} {
			t.Run(tr.name+"/"+by, func(t *testing.T) {
				url, serverConnects, received := rejoinServer(t)
				cl, err := client.New(url, client.WithTransports(tr.transport))
				require.NoError(t, err)
				t.Cleanup(func() { _ = cl.Close() })
				connects, disconnects := make(chan string, 8), make(chan string, 8)
				root, chat := cl.Socket("/"), cl.Socket("/chat")
				for _, s := range []*client.Socket{root, chat} {
					s.OnConnect(func(c client.Conn) error { connects <- c.Namespace(); return nil })
					s.OnDisconnect(func(c client.Conn, reason string) { disconnects <- c.Namespace() + ":" + reason })
				}
				require.NoError(t, cl.Connect())
				require.ElementsMatch(t, []string{"/", "/chat"}, []string{recv(t, connects, "first OnConnect"), recv(t, connects, "second OnConnect")})
				first := recv(t, serverConnects, "first server OnConnect")
				recv(t, serverConnects, "second server OnConnect")
				engineID := first.EngineConn().ID()

				if by == "server" {
					chat.Emit("kick")
					require.Equal(t, "/chat:io server disconnect", recv(t, disconnects, "server namespace disconnect"))
				} else {
					require.NoError(t, chat.Close())
					require.Equal(t, "/chat:io client disconnect", recv(t, disconnects, "Socket.Close"))
				}
				require.False(t, chat.Connected())
				// Client.Connect has nothing to do while the Engine.IO connection is open.
				require.NoError(t, cl.Connect())
				require.False(t, chat.Connected())

				acks := make(chan string, 4)
				chat.Emit("echo", "while closed", func(value string) { acks <- value })
				require.NoError(t, chat.Connect())
				require.Equal(t, "/chat", recv(t, connects, "OnConnect after Socket.Connect"))
				rejoined := recv(t, serverConnects, "server OnConnect after Socket.Connect")
				require.Equal(t, "/chat", rejoined.Namespace())
				require.Equal(t, engineID, rejoined.EngineConn().ID(), "the namespace rejoins the open Engine.IO connection")
				require.True(t, chat.Connected())
				require.Equal(t, "while closed", recv(t, acks, "ACK of the event buffered while closed"))
				require.Equal(t, "/chat:while closed", recv(t, received, "event buffered while closed"))

				require.NoError(t, chat.Connect(), "Connect on a connected Socket")
				chat.Emit("echo", "after", func(value string) { acks <- value })
				require.Equal(t, "after", recv(t, acks, "ACK after rejoin"))
				require.Equal(t, "/chat:after", recv(t, received, "event after rejoin"))
				root.Emit("echo", "root", func(value string) { acks <- value })
				require.Equal(t, "root", recv(t, acks, "root ACK"))
				require.Equal(t, "/:root", recv(t, received, "root event"))
				require.Empty(t, connects, "each join calls OnConnect once")
				require.Empty(t, serverConnects)
				require.Empty(t, disconnects)
			})
		}
	}
}

// The root namespace has no CONNECT of its own in protocol v4: the server joins it
// when the Engine.IO connection opens, so it rejoins only on a new connection.
func TestSocketConnectRootNamespace(t *testing.T) {
	t.Run("open connection", func(t *testing.T) {
		url, serverConnects, received := rejoinServer(t)
		cl, err := client.New(url)
		require.NoError(t, err)
		t.Cleanup(func() { _ = cl.Close() })
		connects, disconnects := make(chan string, 8), make(chan string, 8)
		root, chat := cl.Socket("/"), cl.Socket("/chat")
		for _, s := range []*client.Socket{root, chat} {
			s.OnConnect(func(c client.Conn) error { connects <- c.Namespace(); return nil })
			s.OnDisconnect(func(c client.Conn, reason string) { disconnects <- c.Namespace() + ":" + reason })
		}
		require.NoError(t, cl.Connect())
		recv(t, connects, "first OnConnect")
		recv(t, connects, "second OnConnect")
		recv(t, serverConnects, "first server OnConnect")
		recv(t, serverConnects, "second server OnConnect")
		root.Emit("kick")
		require.Equal(t, "/:io server disconnect", recv(t, disconnects, "root disconnect"))
		require.ErrorIs(t, root.Connect(), client.ErrRootNamespaceRejoin)
		require.False(t, root.Connected())
		acks := make(chan string, 1)
		chat.Emit("echo", "chat", func(value string) { acks <- value })
		require.Equal(t, "chat", recv(t, acks, "chat ACK"))
		require.Equal(t, "/chat:chat", recv(t, received, "chat event"))
		require.Empty(t, connects)
		require.Empty(t, serverConnects)

		// Once the last namespace closes the connection, Connect opens a new one.
		require.NoError(t, chat.Close())
		require.Equal(t, "/chat:io client disconnect", recv(t, disconnects, "chat Close"))
		require.NoError(t, root.Connect())
		require.Equal(t, "/", recv(t, connects, "root OnConnect on a new connection"))
		require.Equal(t, "/", recv(t, serverConnects, "server root OnConnect").Namespace())
		root.Emit("echo", "root", func(value string) { acks <- value })
		require.Equal(t, "root", recv(t, acks, "root ACK"))
		require.Equal(t, "/:root", recv(t, received, "root event"))
		require.False(t, chat.Connected(), "a closed namespace stays closed on the new connection")
		require.Empty(t, connects)
	})
	t.Run("only namespace", func(t *testing.T) {
		url, serverConnects, _ := rejoinServer(t)
		cl, err := client.New(url)
		require.NoError(t, err)
		t.Cleanup(func() { _ = cl.Close() })
		connects, disconnects := make(chan string, 4), make(chan string, 4)
		cl.OnConnect(func(c client.Conn) error { connects <- c.ID(); return nil })
		cl.OnDisconnect(func(c client.Conn, reason string) { disconnects <- reason })
		require.NoError(t, cl.Connect())
		first := recv(t, connects, "OnConnect")
		recv(t, serverConnects, "server OnConnect")
		cl.Emit("kick")
		require.Equal(t, "io server disconnect", recv(t, disconnects, "root disconnect"))
		root := cl.Socket("/")
		require.NoError(t, root.Connect())
		require.NotEqual(t, first, recv(t, connects, "OnConnect on a new connection"))
		recv(t, serverConnects, "server OnConnect on a new connection")
		require.True(t, root.Connected())
	})
}

// A namespace the server rejected stays joining: OnError receives the server's
// message and Connect does nothing; Close followed by Connect sends a new CONNECT.
func TestSocketConnectAfterRejection(t *testing.T) {
	accept := make(chan bool, 2)
	accept <- false
	accept <- true
	serverConnects := make(chan string, 4)
	ts := newTestServer(t, func(srv *socketio.Server) {
		srv.OnConnect("/", func(socketio.Conn) error { return nil })
		srv.OnConnect("/chat", func(c socketio.Conn) error {
			if !<-accept {
				return errors.New("not yet")
			}
			serverConnects <- c.Namespace()
			return nil
		})
		srv.OnEvent("/chat", "echo", func(_ socketio.Conn, value string) string { return value })
	}, steadyPing...)
	cl, err := client.New(ts.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	connects, errs, disconnects := make(chan string, 4), make(chan string, 4), make(chan string, 4)
	chat := cl.Socket("/chat")
	chat.OnConnect(func(c client.Conn) error { connects <- c.Namespace(); return nil })
	chat.OnError(func(_ client.Conn, err error) { errs <- err.Error() })
	chat.OnDisconnect(func(_ client.Conn, reason string) { disconnects <- reason })
	require.NoError(t, cl.Connect())
	require.Equal(t, "not yet", recv(t, errs, "rejection"))
	require.False(t, chat.Connected())
	require.NoError(t, chat.Connect())
	require.NoError(t, chat.Close())
	require.NoError(t, chat.Connect())
	require.Equal(t, "/chat", recv(t, connects, "OnConnect after the second CONNECT"))
	require.Equal(t, "/chat", recv(t, serverConnects, "server OnConnect"))
	acks := make(chan string, 1)
	chat.Emit("echo", "joined", func(value string) { acks <- value })
	require.Equal(t, "joined", recv(t, acks, "ACK"))
	require.Empty(t, connects)
	require.Empty(t, errs)
	require.Empty(t, disconnects, "a rejected namespace was never connected")
}

// A rejected root namespace looks the same: Connect succeeds, OnError receives the
// server's message with a Conn that is not connected, and no lifecycle callback runs.
func TestClientRootRejection(t *testing.T) {
	ts := newTestServer(t, func(srv *socketio.Server) {
		srv.OnConnect("/", func(socketio.Conn) error { return errors.New("rejected") })
	}, steadyPing...)
	cl, err := client.New(ts.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	events := make(chan string, 4)
	cl.OnConnect(func(client.Conn) error { events <- "connect"; return nil })
	cl.OnDisconnect(func(_ client.Conn, reason string) { events <- "disconnect " + reason })
	cl.OnError(func(c client.Conn, err error) {
		events <- fmt.Sprintf("error %v, conn %t, connected %t", err, c != nil, c != nil && c.Connected())
	})
	require.NoError(t, cl.Connect())
	require.Equal(t, "error rejected, conn true, connected false", recv(t, events, "rejection"))
	require.False(t, cl.Socket("/").Connected())
	require.NoError(t, cl.Close())
	require.Empty(t, events)
}

// After Client.Close, Socket.Connect opens a connection that joins only that namespace.
func TestSocketConnectAfterClientClose(t *testing.T) {
	url, serverConnects, received := rejoinServer(t)
	cl, err := client.New(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	connects := make(chan string, 8)
	root, chat := cl.Socket("/"), cl.Socket("/chat")
	for _, s := range []*client.Socket{root, chat} {
		s.OnConnect(func(c client.Conn) error { connects <- c.Namespace(); return nil })
	}
	require.NoError(t, cl.Connect())
	recv(t, connects, "first OnConnect")
	recv(t, connects, "second OnConnect")
	recv(t, serverConnects, "first server OnConnect")
	recv(t, serverConnects, "second server OnConnect")
	require.NoError(t, cl.Close())
	require.NoError(t, chat.Connect())
	require.Equal(t, "/chat", recv(t, connects, "chat OnConnect"))
	acks := make(chan string, 1)
	chat.Emit("echo", "chat", func(value string) { acks <- value })
	require.Equal(t, "chat", recv(t, acks, "chat ACK"))
	require.Equal(t, "/chat:chat", recv(t, received, "chat event"))
	require.False(t, root.Connected())
	require.Empty(t, connects, "the closed root Socket stays closed")
}

// A server DISCONNECT that crosses Close and Connect does not undo the rejoin:
// the client stays joined to the namespace socket the server created for it.
func TestSocketConnectAfterCrossingDisconnect(t *testing.T) {
	for _, tr := range []struct {
		name      string
		transport transport.Transport
	}{{"polling", polling.Default}, {"websocket", websocket.Default}} {
		t.Run(tr.name, func(t *testing.T) {
			url, serverConnects, received := rejoinServer(t)
			cl, err := client.New(url, client.WithTransports(tr.transport))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			connects, disconnects := make(chan string, 8), make(chan string, 8)
			root, chat := cl.Socket("/"), cl.Socket("/chat")
			for _, s := range []*client.Socket{root, chat} {
				s.OnConnect(func(c client.Conn) error { connects <- c.Namespace(); return nil })
				s.OnDisconnect(func(c client.Conn, reason string) { disconnects <- c.Namespace() + ":" + reason })
			}
			require.NoError(t, cl.Connect())
			recv(t, connects, "first OnConnect")
			recv(t, connects, "second OnConnect")
			recv(t, serverConnects, "first server OnConnect")
			recv(t, serverConnects, "second server OnConnect")

			chat.Emit("kick")
			require.NoError(t, chat.Close())
			require.NoError(t, chat.Connect())
			require.Equal(t, "/chat", recv(t, connects, "OnConnect after Socket.Connect"))
			require.Equal(t, "/chat", recv(t, serverConnects, "server OnConnect after Socket.Connect").Namespace())
			acks := make(chan string, 1)
			chat.Emit("echo", "rejoined", func(value string) { acks <- value })
			require.Equal(t, "rejoined", recv(t, acks, "ACK after the rejoin"))
			require.Equal(t, "/chat:rejoined", recv(t, received, "event after the rejoin"))
			require.True(t, chat.Connected())
			// Whether the kick crosses Close depends on timing; the result does not.
			require.Contains(t, []string{"/chat:io client disconnect", "/chat:io server disconnect"}, recv(t, disconnects, "chat disconnect"))
			require.Empty(t, disconnects)
			require.Empty(t, connects)
			require.Empty(t, serverConnects)
		})
	}
}

// Replies to the CONNECTs of earlier rounds arrive while later rounds close and
// reopen the Socket. Whichever reply joins it, its events reach the server.
func TestSocketConnectAfterCloseConnectRounds(t *testing.T) {
	for _, tr := range []struct {
		name      string
		transport transport.Transport
	}{{"polling", polling.Default}, {"websocket", websocket.Default}} {
		t.Run(tr.name, func(t *testing.T) {
			received := make(chan string, 2)
			ts := newTestServer(t, func(srv *socketio.Server) {
				srv.OnConnect("/", func(socketio.Conn) error { return nil })
				srv.OnConnect("/chat", func(socketio.Conn) error { return nil })
				srv.OnEvent("/chat", "echo", func(c socketio.Conn, value string) string {
					received <- c.Namespace() + ":" + value
					return value
				})
			}, steadyPing...)
			cl, err := client.New(ts.URL, client.WithTransports(tr.transport))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			cl.Socket("/")
			chat := cl.Socket("/chat")
			require.NoError(t, cl.Connect())
			for i := 0; i < 100; i++ {
				require.NoError(t, chat.Close())
				require.NoError(t, chat.Connect())
			}
			acks := make(chan string, 1)
			for _, value := range []string{"first", "second"} {
				chat.Emit("echo", value, func(value string) { acks <- value })
				require.Equal(t, value, recv(t, acks, "ACK of "+value))
				require.Equal(t, "/chat:"+value, recv(t, received, value))
				require.True(t, chat.Connected())
			}
		})
	}
}

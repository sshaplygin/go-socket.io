package client

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
)

// rejoinPeer is a Client on a fake engine with the root and /chat namespaces connected.
type rejoinPeer struct {
	cl          *manager
	fc          *fakeConn
	root, chat  *Socket
	connects    chan string
	disconnects chan string
}

func newRejoinPeer(t *testing.T, opts ...Option) *rejoinPeer {
	t.Helper()
	cl, err := newTestManager(t, "http://unused/", opts...)
	require.NoError(t, err)
	p := &rejoinPeer{cl: cl, fc: newFakeConn(t), connects: make(chan string, 8), disconnects: make(chan string, 8)}
	cl.dial = func(string) (engineclient.Conn, error) { return p.fc, nil }
	p.root, p.chat = cl.Socket("/"), cl.Socket("/chat")
	for _, s := range []*Socket{p.root, p.chat} {
		s.OnConnect(func(c Conn) error { p.connects <- c.Namespace(); return nil })
		s.OnDisconnect(func(c Conn, reason string) { p.disconnects <- c.Namespace() + ":" + reason })
	}
	require.NoError(t, cl.Connect())
	require.Equal(t, "0/chat,", recv(t, p.fc.out, "chat CONNECT"))
	p.fc.reads <- "0"
	p.fc.reads <- "0/chat,"
	require.ElementsMatch(t, []string{"/", "/chat"}, []string{recv(t, p.connects, "root OnConnect"), recv(t, p.connects, "chat OnConnect")})
	return p
}

// written returns the packets written before a root event sent as a barrier:
// the writer keeps CONNECT and DISCONNECT in order with application events.
func (p *rejoinPeer) written(t *testing.T) []string {
	t.Helper()
	p.root.Emit("barrier")
	var got []string
	for {
		frame := strings.TrimSuffix(recv(t, p.fc.out, "barrier"), "\n")
		if frame == `2["barrier"]` {
			return got
		}
		got = append(got, frame)
	}
}

// read feeds the reader a frame and returns once the reader has handled it:
// frames are handled in order, and a root event follows this one.
func (p *rejoinPeer) read(t *testing.T, frame string) {
	t.Helper()
	handled := make(chan struct{}, 1)
	off := p.root.On("handled", func(Conn) { handled <- struct{}{} })
	defer off()
	p.fc.reads <- frame
	p.fc.reads <- `2["handled"]`
	recv(t, handled, "reader handled "+frame)
}

func TestSocketConnectConcurrentCallsSendOneConnect(t *testing.T) {
	for _, by := range []string{"server", "client"} {
		t.Run(by, func(t *testing.T) {
			p := newRejoinPeer(t)
			if by == "server" {
				p.fc.reads <- "1/chat,"
				require.Equal(t, "/chat:io server disconnect", recv(t, p.disconnects, "server disconnect"))
			} else {
				require.NoError(t, p.chat.Close())
				require.Equal(t, "/chat:io client disconnect", recv(t, p.disconnects, "Socket.Close"))
			}
			p.chat.Emit("pending", 1)
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); require.NoError(t, p.chat.Connect()) }()
			}
			wg.Wait()
			want := []string{"0/chat,"}
			if by == "client" {
				want = []string{"1/chat,", "0/chat,"}
			}
			require.Equal(t, want, p.written(t))
			p.fc.reads <- "0/chat,"
			require.Equal(t, "/chat", recv(t, p.connects, "OnConnect after Connect"))
			require.Equal(t, `2/chat,["pending",1]`, strings.TrimSuffix(recv(t, p.fc.out, "buffered event"), "\n"))
			require.True(t, p.chat.Connected())
			require.NoError(t, p.chat.Connect())
			require.Empty(t, p.written(t), "Connect on a connected Socket sends nothing")
			require.Empty(t, p.connects)
		})
	}
}

func TestSocketConnectRootOnOpenConnection(t *testing.T) {
	for _, by := range []string{"server", "client"} {
		t.Run(by, func(t *testing.T) {
			p := newRejoinPeer(t)
			if by == "server" {
				p.fc.reads <- "1"
				require.Equal(t, "/:io server disconnect", recv(t, p.disconnects, "server disconnect"))
			} else {
				require.NoError(t, p.root.Close())
				require.Equal(t, "/:io client disconnect", recv(t, p.disconnects, "Socket.Close"))
			}
			require.ErrorIs(t, p.root.Connect(), ErrRootNamespaceRejoin)
			p.chat.Emit("barrier")
			got := strings.TrimSuffix(recv(t, p.fc.out, "chat barrier"), "\n")
			if by == "client" {
				require.Equal(t, "1", got)
				got = strings.TrimSuffix(recv(t, p.fc.out, "chat barrier"), "\n")
			}
			require.Equal(t, `2/chat,["barrier"]`, got, "no root CONNECT")
			require.False(t, p.root.Connected())
			p.cl.state.mu.Lock()
			active := p.root.active
			p.cl.state.mu.Unlock()
			require.False(t, active, "a refused Connect changes nothing")
		})
	}
}

// While the Client is reconnecting, Connect does not dial: the namespace joins
// the connection the reconnection restores.
func TestSocketConnectWhileReconnecting(t *testing.T) {
	p := newRejoinPeer(t, WithReconnectionDelay(time.Millisecond), WithRandomizationFactor(-1))
	p.fc.reads <- "1/chat,"
	require.Equal(t, "/chat:io server disconnect", recv(t, p.disconnects, "server disconnect"))
	next := newFakeConn(t)
	dials := make(chan struct{}, 4)
	release := make(chan struct{})
	p.cl.dial = func(string) (engineclient.Conn, error) {
		dials <- struct{}{}
		<-release
		return next, nil
	}
	reconnects := make(chan int, 2)
	p.cl.OnReconnect(func(n int) { reconnects <- n })
	close(p.fc.peerGone)
	require.True(t, strings.HasPrefix(recv(t, p.disconnects, "engine end"), "/:"))
	recv(t, dials, "reconnection dial")
	require.NoError(t, p.chat.Connect())
	require.Empty(t, dials, "Connect joins the reconnection in progress")
	close(release)
	require.Equal(t, 1, recv(t, reconnects, "OnReconnect"))
	require.Equal(t, "0/chat,", recv(t, next.out, "chat CONNECT on the restored connection"))
	next.reads <- "0"
	next.reads <- "0/chat,"
	require.ElementsMatch(t, []string{"/", "/chat"}, []string{recv(t, p.connects, "root OnConnect"), recv(t, p.connects, "chat OnConnect")})
	require.Empty(t, dials)
}

// Without a connection, Connect dials and returns the dial's error, which is
// not retried automatically.
func TestSocketConnectDials(t *testing.T) {
	cl, err := newTestManager(t, "http://unused/", WithReconnectionDelay(time.Millisecond), WithRandomizationFactor(-1))
	require.NoError(t, err)
	fc := newFakeConn(t)
	failed := errors.New("down")
	dials := 0
	cl.dial = func(string) (engineclient.Conn, error) {
		dials++
		if dials == 1 {
			return nil, failed
		}
		return fc, nil
	}
	chat := cl.Socket("/chat")
	require.ErrorIs(t, chat.Connect(), failed)
	require.NoError(t, chat.Connect())
	require.Equal(t, "0/chat,", recv(t, fc.out, "chat CONNECT"))
	require.Equal(t, 2, dials)
}

// A failed first Connect is the caller's to retry: unlike socket.io-client 2.x,
// the Client starts no reconnection for it, whatever WithReconnection says.
func TestClientConnectFailureStartsNoReconnection(t *testing.T) {
	cl, err := newTestManager(t, "http://unused/", WithReconnectionDelay(time.Millisecond), WithRandomizationFactor(-1))
	require.NoError(t, err)
	attempts := make(chan int, 4)
	cl.OnReconnectAttempt(func(n int) { attempts <- n })
	failed := errors.New("down")
	fc := newFakeConn(t)
	dials := 0
	cl.dial = func(string) (engineclient.Conn, error) {
		dials++
		if dials == 1 {
			return nil, failed
		}
		return fc, nil
	}
	require.ErrorIs(t, cl.Connect(), failed)
	cl.state.mu.Lock()
	retry := cl.state.retry
	cl.state.mu.Unlock()
	require.Nil(t, retry, "no reconnection run")
	require.NoError(t, cl.Connect())
	fc.reads <- "0"
	require.Equal(t, 2, dials)
	require.Empty(t, attempts)
}

// A DISCONNECT that the server sent before it read Close and Connect ends the
// namespace socket that Close ended. A server sends a namespace socket's CONNECT
// reply before its DISCONNECT, so the reply that follows joins the Socket.
func TestSocketConnectIgnoresCrossingDisconnect(t *testing.T) {
	p := newRejoinPeer(t)
	require.NoError(t, p.chat.Close())
	require.Equal(t, "/chat:io client disconnect", recv(t, p.disconnects, "Socket.Close"))
	require.NoError(t, p.chat.Connect())
	require.Equal(t, []string{"1/chat,", "0/chat,"}, p.written(t))
	p.fc.reads <- "1/chat,"
	p.fc.reads <- "0/chat,"
	require.Equal(t, "/chat", recv(t, p.connects, "OnConnect after the crossing DISCONNECT"))
	require.True(t, p.chat.Connected())
	require.Empty(t, p.disconnects, "the crossing DISCONNECT is not the joined namespace's")
	p.chat.Emit("after", 1)
	require.Equal(t, `2/chat,["after",1]`, strings.TrimSuffix(recv(t, p.fc.out, "event after the rejoin"), "\n"))
	p.fc.reads <- "1/chat,"
	require.Equal(t, "/chat:io server disconnect", recv(t, p.disconnects, "DISCONNECT of the joined namespace"))
	require.False(t, p.chat.Connected())
}

// Connect during Client.Close, or during the Close of the last Socket, waits for
// that close and then dials: it joins the next connection, not the closing one.
func TestSocketConnectWaitsForClose(t *testing.T) {
	for _, by := range []string{"last Socket", "Client.Close"} {
		t.Run(by, func(t *testing.T) {
			p := newRejoinPeer(t)
			if by == "last Socket" {
				require.NoError(t, p.root.Close())
				require.Equal(t, "/:io client disconnect", recv(t, p.disconnects, "root Close"))
			}
			next := newFakeConn(t)
			p.cl.dial = func(string) (engineclient.Conn, error) { return next, nil }
			closing, release, waits := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
			var once, released sync.Once
			p.cl.state.testHookClosing = func() { once.Do(func() { close(closing); <-release }) }
			unblock := func() { released.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			p.cl.state.testHookJoinWaits = func() { waits <- struct{}{} }
			closed := make(chan error, 1)
			go func() {
				if by == "last Socket" {
					closed <- p.chat.Close()
				} else {
					closed <- p.cl.Close()
				}
			}()
			recv(t, closing, "close stopped the client")
			joined := make(chan error, 1)
			go func() { joined <- p.chat.Connect() }()
			recv(t, waits, "Connect waits for the close")
			unblock()
			require.NoError(t, recv(t, closed, "close"))
			require.NoError(t, recv(t, joined, "Connect"))
			require.Equal(t, "0/chat,", recv(t, next.out, "chat CONNECT on the next connection"))
			next.reads <- "0"
			next.reads <- "0/chat,"
			require.Equal(t, "/chat", recv(t, p.connects, "chat OnConnect"))
			require.True(t, p.chat.Connected())
			require.False(t, p.root.Connected(), "the closed root Socket stays closed")
		})
	}
}

// Client.Connect during Client.Close waits for that close and dials a new
// connection that every Socket joins.
func TestClientConnectWaitsForClose(t *testing.T) {
	p := newRejoinPeer(t)
	next := newFakeConn(t)
	p.cl.dial = func(string) (engineclient.Conn, error) { return next, nil }
	closing, release, waits := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	var once, released sync.Once
	p.cl.state.testHookClosing = func() { once.Do(func() { close(closing); <-release }) }
	p.cl.state.testHookJoinWaits = func() { waits <- struct{}{} }
	unblock := func() { released.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	closed := make(chan error, 1)
	go func() { closed <- p.cl.Close() }()
	recv(t, closing, "Client.Close stopped the client")
	connected := make(chan error, 1)
	go func() { connected <- p.cl.Connect() }()
	recv(t, waits, "Client.Connect waits for the close")
	unblock()
	require.NoError(t, recv(t, closed, "Client.Close"))
	require.NoError(t, recv(t, connected, "Client.Connect"))
	require.Equal(t, "0/chat,", recv(t, next.out, "chat CONNECT on the next connection"))
	next.reads <- "0"
	next.reads <- "0/chat,"
	require.ElementsMatch(t, []string{"/", "/chat"}, []string{recv(t, p.connects, "root OnConnect"), recv(t, p.connects, "chat OnConnect")})
}

// After the Engine.IO connection ends and before its reconnection starts, Connect
// does not dial: the reconnection restores the namespace with the others.
func TestSocketConnectBeforeReconnectionStarts(t *testing.T) {
	p := newRejoinPeer(t, WithReconnectionDelay(time.Millisecond), WithRandomizationFactor(-1))
	p.fc.reads <- "1/chat,"
	require.Equal(t, "/chat:io server disconnect", recv(t, p.disconnects, "server disconnect"))
	next := newFakeConn(t)
	var dials atomic.Int32
	p.cl.dial = func(string) (engineclient.Conn, error) {
		if dials.Add(1) == 1 {
			return nil, errors.New("down")
		}
		return next, nil
	}
	attempts := make(chan int, 16)
	p.cl.OnReconnectAttempt(func(n int) {
		select {
		case attempts <- n:
		default:
		}
	})
	t.Cleanup(func() { _ = p.cl.Close() }) // before next closes
	joined := make(chan string, 1)
	// The root's OnDisconnect runs before the connection's recovery starts.
	p.root.OnDisconnect(func(Conn, string) {
		err := p.chat.Connect()
		joined <- fmt.Sprintf("err %v, dials %d", err, dials.Load())
	})
	close(p.fc.peerGone)
	require.Equal(t, "err <nil>, dials 0", recv(t, joined, "Connect"))
	require.Equal(t, 1, recv(t, attempts, "first reconnection attempt"))
	require.Equal(t, 2, recv(t, attempts, "second reconnection attempt"))
	require.Equal(t, "0/chat,", recv(t, next.out, "chat CONNECT on the restored connection"))
	next.reads <- "0"
	next.reads <- "0/chat,"
	require.ElementsMatch(t, []string{"/", "/chat"}, []string{recv(t, p.connects, "root OnConnect"), recv(t, p.connects, "chat OnConnect")})
}

// The reader registers a CONNECT reply's Conn before it binds it to the Socket.
// A Close in between unregisters that Conn and sends DISCONNECT; a Connect that
// follows must join with the reply to its own CONNECT, not with that Conn, whose
// events the connection no longer routes.
func TestSocketConnectDropsReplyClosedBeforeBinding(t *testing.T) {
	p := newRejoinPeer(t)
	p.fc.reads <- "1/chat,"
	require.Equal(t, "/chat:io server disconnect", recv(t, p.disconnects, "server disconnect"))
	require.NoError(t, p.chat.Connect())
	require.Equal(t, []string{"0/chat,"}, p.written(t))
	// The reader's first step for the reply, as clientConnectPacketHandler does it.
	m := p.cl.state
	m.mu.Lock()
	current := m.conn
	m.mu.Unlock()
	reply := newNamespaceConn(current, "/chat")
	require.True(t, current.register("/chat", reply))
	require.NoError(t, p.chat.Close())
	require.NoError(t, p.chat.Connect())
	// The reader's second step.
	require.NoError(t, m.namespaceConnected(current, "/chat", reply))
	require.False(t, p.chat.Connected(), "the reply that Close ended does not join the Socket")
	require.Empty(t, p.connects)
	require.Equal(t, []string{"1/chat,", "0/chat,"}, p.written(t))
	p.fc.reads <- "0/chat,"
	require.Equal(t, "/chat", recv(t, p.connects, "OnConnect for the second CONNECT"))
	require.True(t, p.chat.Connected())
	p.chat.Emit("after", 1)
	require.Equal(t, []string{`2/chat,["after",1]`}, p.written(t))
	require.Empty(t, p.connects)
	require.Empty(t, p.disconnects, "the join that Close ended was never announced")
}

// A CONNECT reply for a Socket that the server's DISCONNECT closed announces a
// namespace socket that the server keeps. The client tells the server to drop
// it, so that the server answers the next CONNECT.
func TestSocketConnectReleasesNamespaceJoinedWhileClosed(t *testing.T) {
	p := newRejoinPeer(t)
	p.fc.reads <- "1/chat,"
	require.Equal(t, "/chat:io server disconnect", recv(t, p.disconnects, "server disconnect"))
	require.NoError(t, p.chat.Connect())
	require.NoError(t, p.chat.Close())
	require.NoError(t, p.chat.Connect())
	require.Equal(t, []string{"0/chat,", "1/chat,", "0/chat,"}, p.written(t))
	p.fc.reads <- "0/chat," // the first CONNECT's namespace socket
	require.Equal(t, "/chat", recv(t, p.connects, "OnConnect"))
	p.fc.reads <- "1/chat," // the server disconnects it
	require.Equal(t, "/chat:io server disconnect", recv(t, p.disconnects, "server disconnect"))
	p.read(t, "0/chat,") // the second CONNECT's namespace socket
	require.Equal(t, []string{"1/chat,"}, p.written(t))
	require.False(t, p.chat.Connected())
	require.NoError(t, p.chat.Connect())
	require.Equal(t, []string{"0/chat,"}, p.written(t))
	p.fc.reads <- "0/chat,"
	require.Equal(t, "/chat", recv(t, p.connects, "OnConnect after Connect"))
	require.True(t, p.chat.Connected())

	// Close has sent DISCONNECT after the CONNECT whose reply arrives next: the
	// server drops that namespace socket without another DISCONNECT.
	require.NoError(t, p.chat.Close())
	require.Equal(t, "/chat:io client disconnect", recv(t, p.disconnects, "Socket.Close"))
	require.NoError(t, p.chat.Connect())
	require.NoError(t, p.chat.Close())
	p.read(t, "0/chat,")
	require.Equal(t, []string{"1/chat,", "0/chat,", "1/chat,"}, p.written(t))
	require.Empty(t, p.connects)
	require.Empty(t, p.disconnects)
}

// Connect on a Client that has no connection reopens only its own Socket, but
// the connection it dials is the Client's: Sockets that are not closed join it.
func TestSocketConnectDialJoinsSocketsNotClosed(t *testing.T) {
	cl, err := newTestManager(t, "http://unused/")
	require.NoError(t, err)
	fc := newFakeConn(t)
	cl.dial = func(string) (engineclient.Conn, error) { return fc, nil }
	connects := make(chan string, 8)
	root, chat, open, closed := cl.Socket("/"), cl.Socket("/chat"), cl.Socket("/open"), cl.Socket("/closed")
	for _, s := range []*Socket{root, chat, open, closed} {
		s.OnConnect(func(c Conn) error { connects <- c.Namespace(); return nil })
	}
	require.NoError(t, closed.Close())
	require.NoError(t, chat.Connect())
	require.Equal(t, "0/chat,", recv(t, fc.out, "chat CONNECT"))
	require.Equal(t, "0/open,", recv(t, fc.out, "CONNECT of the Socket that was not closed"))
	fc.reads <- "0"
	fc.reads <- "0/chat,"
	fc.reads <- "0/open,"
	require.ElementsMatch(t, []string{"/", "/chat", "/open"},
		[]string{recv(t, connects, "OnConnect"), recv(t, connects, "OnConnect"), recv(t, connects, "OnConnect")})
	root.Emit("barrier")
	require.Equal(t, `2["barrier"]`, strings.TrimSuffix(recv(t, fc.out, "barrier"), "\n"), "no CONNECT for the closed Socket")
	require.False(t, closed.Connected())
}

// An event emitted while the Socket waits for its CONNECT reply stays buffered
// through Close and is sent when the Socket joins after Connect.
func TestSocketConnectKeepsEventsBufferedBeforeClose(t *testing.T) {
	p := newRejoinPeer(t)
	p.fc.reads <- "1/chat,"
	require.Equal(t, "/chat:io server disconnect", recv(t, p.disconnects, "server disconnect"))
	require.NoError(t, p.chat.Connect())
	p.chat.Emit("early", 1)
	require.NoError(t, p.chat.Close())
	require.NoError(t, p.chat.Connect())
	require.Equal(t, []string{"0/chat,", "1/chat,", "0/chat,"}, p.written(t))
	p.fc.reads <- "0/chat,"
	require.Equal(t, "/chat", recv(t, p.connects, "OnConnect after Connect"))
	require.Equal(t, `2/chat,["early",1]`, strings.TrimSuffix(recv(t, p.fc.out, "buffered event"), "\n"))
}

// The reader of a connection that a close is draining still reads the server's
// DISCONNECT for a namespace. A Socket that Connect reopened since that close
// belongs to the connection being dialed: the DISCONNECT neither closes it nor
// cancels the dial.
func TestConnectIgnoresDisconnectReadByClosingConnection(t *testing.T) {
	for _, tc := range []struct{ name, closer, opener string }{
		{"last Socket, Socket.Connect", "socket", "socket"},
		{"Client.Close, Socket.Connect", "client", "socket"},
		{"Client.Close, Client.Connect", "client", "client"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newRejoinPeer(t)
			next := newFakeConn(t)
			dials, release := make(chan struct{}, 1), make(chan struct{})
			p.cl.dial = func(string) (engineclient.Conn, error) {
				dials <- struct{}{}
				<-release
				return next, nil
			}
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			// The held writer keeps the closing connection draining and its reader running.
			p.fc.hold.Store(true)
			if tc.closer == "socket" {
				require.NoError(t, p.root.Close())
				require.NoError(t, p.chat.Close())
			} else {
				require.NoError(t, p.cl.Close())
			}
			require.ElementsMatch(t, []string{"/:io client disconnect", "/chat:io client disconnect"},
				[]string{recv(t, p.disconnects, "first OnDisconnect"), recv(t, p.disconnects, "second OnDisconnect")})
			opened := make(chan error, 1)
			go func() {
				if tc.opener == "socket" {
					opened <- p.chat.Connect()
				} else {
					opened <- p.cl.Connect()
				}
			}()
			recv(t, dials, "dial")
			// The reader takes the second frame once it has handled the first.
			p.fc.reads <- "1/chat,"
			p.fc.reads <- "1/chat,"
			unblock()
			require.NoError(t, recv(t, opened, "Connect"))
			_ = p.fc.Close() // the next connection starts once the closed one has ended
			require.Equal(t, "0/chat,", recv(t, next.out, "chat CONNECT on the next connection"))
			next.reads <- "0"
			next.reads <- "0/chat,"
			want := []string{"/chat"}
			if tc.opener == "client" {
				want = append(want, "/")
			}
			var got []string
			for range want {
				got = append(got, recv(t, p.connects, "OnConnect"))
			}
			require.ElementsMatch(t, want, got)
			require.True(t, p.chat.Connected())
			require.Empty(t, p.disconnects)
		})
	}
}

// After a failed dial the closed connection may still be draining. A DISCONNECT
// its reader reads then does not close the Socket that the failed Connect
// reopened: the Socket joins the connection another Socket's Connect dials.
func TestConnectIgnoresDisconnectReadByClosedConnectionAfterFailedDial(t *testing.T) {
	p := newRejoinPeer(t)
	next := newFakeConn(t)
	failed := errors.New("down")
	var dials atomic.Int32
	p.cl.dial = func(string) (engineclient.Conn, error) {
		if dials.Add(1) == 1 {
			return nil, failed
		}
		return next, nil
	}
	p.fc.hold.Store(true)
	require.NoError(t, p.cl.Close())
	require.ErrorIs(t, p.chat.Connect(), failed)
	p.fc.reads <- "1/chat,"
	p.fc.reads <- "1/chat,"
	require.NoError(t, p.root.Connect())
	_ = p.fc.Close()
	require.Equal(t, "0/chat,", recv(t, next.out, "chat CONNECT on the next connection"))
	require.Equal(t, int32(2), dials.Load())
}

// Once the reconnection has given up, the Client has no connection and is not
// reconnecting: Connect dials.
func TestSocketConnectAfterReconnectionGaveUp(t *testing.T) {
	p := newRejoinPeer(t, WithReconnectionDelay(time.Millisecond), WithRandomizationFactor(-1), WithReconnectionAttempts(1))
	p.fc.reads <- "1/chat,"
	require.Equal(t, "/chat:io server disconnect", recv(t, p.disconnects, "server disconnect"))
	next := newFakeConn(t)
	var dials atomic.Int32
	p.cl.dial = func(string) (engineclient.Conn, error) {
		if dials.Add(1) == 1 {
			return nil, errors.New("down")
		}
		return next, nil
	}
	gaveUp := make(chan struct{}, 1)
	p.cl.OnReconnectFailed(func() { gaveUp <- struct{}{} })
	close(p.fc.peerGone)
	require.True(t, strings.HasPrefix(recv(t, p.disconnects, "engine end"), "/:"))
	recv(t, gaveUp, "OnReconnectFailed")
	require.Equal(t, int32(1), dials.Load())
	require.NoError(t, p.chat.Connect())
	require.Equal(t, int32(2), dials.Load(), "Connect dials")
	require.Equal(t, "0/chat,", recv(t, next.out, "chat CONNECT on the dialed connection"))
	next.reads <- "0"
	next.reads <- "0/chat,"
	require.ElementsMatch(t, []string{"/", "/chat"}, []string{recv(t, p.connects, "root OnConnect"), recv(t, p.connects, "chat OnConnect")})
}

// With WithReconnection(false) nothing restores a connection that ended: Connect
// dials, also when it runs before the end of that connection has been handled.
func TestSocketConnectAfterConnectionEndedWithoutReconnection(t *testing.T) {
	p := newRejoinPeer(t, WithReconnection(false))
	p.fc.reads <- "1/chat,"
	require.Equal(t, "/chat:io server disconnect", recv(t, p.disconnects, "server disconnect"))
	next := newFakeConn(t)
	var dials atomic.Int32
	p.cl.dial = func(string) (engineclient.Conn, error) {
		dials.Add(1)
		return next, nil
	}
	t.Cleanup(func() { _ = p.cl.Close() }) // before next closes
	joined := make(chan string, 1)
	// The root's OnDisconnect runs before the end of the connection is handled.
	p.root.OnDisconnect(func(Conn, string) {
		err := p.chat.Connect()
		joined <- fmt.Sprintf("err %v, dials %d", err, dials.Load())
	})
	close(p.fc.peerGone)
	require.Equal(t, "err <nil>, dials 1", recv(t, joined, "Connect"))
	require.Equal(t, "0/chat,", recv(t, next.out, "chat CONNECT on the dialed connection"))
	next.reads <- "0"
	next.reads <- "0/chat,"
	require.ElementsMatch(t, []string{"/", "/chat"}, []string{recv(t, p.connects, "root OnConnect"), recv(t, p.connects, "chat OnConnect")})
}

package client

import (
	"errors"
	"strings"
	"sync"
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

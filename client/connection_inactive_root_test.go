package client

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
)

func TestClientInactiveRootDuringReconnect(t *testing.T) {
	for _, mode := range []string{"server", "client"} {
		t.Run(mode, func(t *testing.T) {
			cl, err := newTestManager("http://unused/", WithReconnectionAttempts(1), WithReconnectionDelay(time.Millisecond), WithRandomizationFactor(-1))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			first, second := newFakeConn(t), newFakeConn(t)
			dials := 0
			cl.dial = func(string) (engineclient.Conn, error) {
				dials++
				if dials == 1 {
					return first, nil
				}
				if dials == 2 {
					return second, nil
				}
				return nil, errors.New("down")
			}
			root, chat := cl.Socket("/"), cl.Socket("/chat")
			ready := make(chan Conn, 4)
			root.OnConnect(func(c Conn) error { ready <- c; return nil })
			chat.OnConnect(func(c Conn) error { ready <- c; return nil })
			rootDiscs := make(chan string, 4)
			root.OnDisconnect(func(_ Conn, reason string) { rootDiscs <- reason })
			recovered := make(chan struct{}, 2)
			cl.OnReconnect(func(int) { recovered <- struct{}{} })
			require.NoError(t, cl.Connect())
			first.reads <- "0"
			first.reads <- "0/chat,"
			recv(t, ready, "root CONNECT")
			recv(t, ready, "chat CONNECT")
			if mode == "server" {
				first.reads <- "1"
				require.Equal(t, "io server disconnect", recv(t, rootDiscs, "root DISCONNECT"))
			} else {
				require.NoError(t, root.Close())
				require.Equal(t, "io client disconnect", recv(t, rootDiscs, "root Close"))
			}
			close(first.peerGone)
			recv(t, recovered, "replacement engine OPEN")
			cl.state.mu.Lock()
			current := cl.state.conn
			cl.state.mu.Unlock()
			close(second.peerGone) // no namespace CONNECT reply was received
			recv(t, current.done, "replacement engine failure")
			require.Empty(t, rootDiscs, "inactive root has no manager lifecycle subscription")
			require.False(t, root.Connected())
			require.Empty(t, root.ID())
		})
	}
}

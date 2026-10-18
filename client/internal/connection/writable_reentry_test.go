package connection

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
)

type callbackWritableClientConn struct {
	*fakeConn
	check func() bool
}

func (c *callbackWritableClientConn) Writable() bool { return c.check() }

// Covers C11
func TestClientVolatileWritableReentry(t *testing.T) {
	for _, mode := range []string{"state", "close"} {
		t.Run(mode, func(t *testing.T) {
			cl, err := newTestManager("http://unused/", WithReconnection(!(true)))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			fc := newFakeConn(t)
			timedOut := false
			wire := &callbackWritableClientConn{fakeConn: fc}
			wire.check = func() bool {
				checked := make(chan struct{})
				go func() {
					if mode == "close" {
						_ = cl.Socket("/").Close()
					} else {
						cl.Socket("/").Connected()
						cl.Socket("/").ID()
					}
					close(checked)
				}()
				select {
				case <-checked:
					return true
				case <-time.After(time.Second):
					timedOut = true
					return false
				}
			}
			cl.dial = func(string) (engineclient.Conn, error) { return wire, nil }
			ready := make(chan Conn, 1)
			cl.OnConnect(func(c Conn) error { ready <- c; return nil })
			require.NoError(t, cl.Connect())
			fc.reads <- "0"
			nc := recv(t, ready, "namespace CONNECT")
			require.NoError(t, nc.Volatile().Emit("volatile"))
			require.False(t, timedOut, "Writable must run outside the socket state mutex")
			if mode == "state" {
				require.Equal(t, ev("volatile"), recv(t, fc.out, "volatile after reentrant state read"))
			} else {
				require.False(t, cl.Socket("/").Connected())
				require.Equal(t, "1", recv(t, fc.out, "reentrant namespace close"))
				recv(t, fc.closed, "engine close after DISCONNECT")
				require.Empty(t, fc.out, "volatile must recheck socket state after Writable")
			}
		})
	}
}

package client

import (
	"testing"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
)

// A server DISCONNECT invalidates cached root state even before Socket("/") exists.
// Covers C2
// Covers C10
func TestClientLateRootAfterServerDisconnect(t *testing.T) {
	cl, err := New("http://unused/chat", nil)
	require.NoError(t, err)
	fc := newFakeConn(t)
	cl.dial = func(string) (engineclient.Conn, error) { return fc, nil }
	ready := make(chan struct{}, 1)
	cl.OnConnect(func(Conn) error { ready <- struct{}{}; return nil })
	cl.OnEvent("barrier", func(Conn) string { return "ok" })
	require.NoError(t, cl.Connect())
	closeAtEnd(t, fc, cl.conn)
	require.Equal(t, "0/chat,", recv(t, fc.out, "chat CONNECT"))
	fc.reads <- "0"
	fc.reads <- "0/chat,"
	recv(t, ready, "chat connected")
	oldRoot, ok := cl.conn.namespaces.Get(rootNamespace)
	require.True(t, ok)
	fc.reads <- "1"
	fc.reads <- `2/chat,7["barrier"]`
	require.Equal(t, "3/chat,7[\"ok\"]\n", recv(t, fc.out, "root DISCONNECT read barrier"))
	root := cl.Socket("/")
	connected := make(chan Conn, 2)
	root.OnConnect(func(c Conn) error { connected <- c; return nil })
	// Drain tasks queued by late OnConnect registration before asserting absence.
	processed := make(chan struct{})
	cl.conn.queueReadTask(func() { close(processed) })
	recv(t, processed, "late handler task barrier")
	require.Empty(t, connected, "the cached CONNECT was invalidated")
	require.Empty(t, oldRoot.Rooms(), "the disconnected cached Conn left its rooms")
	cl.Emit("barrier")
	require.Equal(t, "2/chat,[\"barrier\"]\n", recv(t, fc.out, "chat remains usable"))
	// Only a fresh CONNECT may make the new root socket connected.
	fc.reads <- "0"
	fresh := recv(t, connected, "fresh root CONNECT")
	require.Equal(t, "/", fresh.Namespace())
	require.NotSame(t, oldRoot, fresh)
	require.NoError(t, cl.Close())
}

// Old read loops can still drain while an explicit Connect replaces their engine.
// Packets from that old engine must not disconnect sockets in the new lifecycle.
// Covers C2
// Covers C10
func TestClientOldDisconnectCannotCloseNewSockets(t *testing.T) {
	for _, staleNamespace := range []string{"/", "/chat"} {
		t.Run(staleNamespace, func(t *testing.T) {
			p := start(t, 'C', hooks{}, "/chat")
			p.stall(t, p.nc, 0)
			defer close(p.fc.release)
			require.NoError(t, p.cl.Close())
			next := newFakeConn(t)
			p.cl.dial = func(string) (engineclient.Conn, error) { return next, nil }
			p.cl.OnConnect(func(c Conn) error { c.Emit("new root"); return nil })
			require.NoError(t, p.cl.Connect())
			closeAtEnd(t, next, p.conn())
			packet := "1" + staleNamespace + ","
			if staleNamespace == "/" {
				packet = "1"
			}
			p.send(t, packet)
			p.send(t, packet) // consuming this header proves the first was dispatched
			// Release the old writer without closing the channel twice at cleanup.
			p.fc.hold.Store(false)
			p.fc.release <- struct{}{}
			require.Equal(t, "0/chat,", recv(t, next.out, "new chat CONNECT"))
			next.reads <- "0"
			require.Equal(t, ev("new root"), recv(t, next.out, "new root remains connected"))
			next.reads <- "0/chat,"
			require.Equal(t, "/chat", recv(t, p.conns, "new chat remains active").Namespace())
			require.NoError(t, p.cl.Close())
		})
	}
}

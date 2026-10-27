package client

import (
	"sync"
	"testing"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/stretchr/testify/require"
)

func reconnectPeer(t *testing.T, p *peer) Conn {
	t.Helper()
	require.NoError(t, p.cl.Close())
	recv(t, p.fc.closed, "old engine close")
	next := newFakeConn(t)
	p.cl.dial = func(string) (engineclient.Conn, error) { return next, nil }
	require.NoError(t, p.cl.Connect())
	closeAtEnd(t, next, p.conn())
	p.fc = next
	for _, nsp := range p.nsps {
		require.Equal(t, "0"+nsp+",", recv(t, next.out, "namespace CONNECT"))
	}
	p.send(t, "0")
	root := recv(t, p.conns, "new root connection")
	for _, nsp := range p.nsps {
		p.send(t, "0"+nsp+",")
		p.extra[nsp] = recv(t, p.conns, "new namespace connection")
	}
	return root
}

func TestClientStaleConnCannotAffectReplacement(t *testing.T) {
	for _, nsp := range []string{"/", "/chat"} {
		for _, operation := range []string{"emit", "disconnect", "close"} {
			t.Run(nsp+"/"+operation, func(t *testing.T) {
				p := start(t, 'C', hooks{}, "/chat")
				old := p.nc
				if nsp != "/" {
					old = p.extra[nsp]
				}
				// Retained builders must have the same lifetime as their Conn.
				compressed, volatile := old.Compress(false), old.Volatile()
				fresh := reconnectPeer(t, p)
				require.False(t, old.Connected())
				switch operation {
				case "emit":
					old.Emit("stale")
					old.Send("stale")
					compressed.Emit("stale")
					compressed.Send("stale")
					volatile.Emit("stale")
					volatile.Send("stale")
				case "disconnect":
					old.Disconnect()
				case "close":
					require.NoError(t, old.Close())
				}
				require.True(t, fresh.Connected(), "stale handle closed the new root")
				require.True(t, p.extra["/chat"].Connected(), "stale handle closed the new namespace")
				fresh.Emit("barrier")
				require.Equal(t, ev("barrier"), recv(t, p.fc.out, "new connection remains usable"))
			})
		}
	}
}

func TestClientConnEmitRechecksGenerationAfterEncoding(t *testing.T) {
	p := start(t, 'C', hooks{})
	old := p.nc
	var fresh Conn
	timedOut := false
	old.Emit("stale", clientReentrantJSON{check: func() {
		fresh = reconnectPeer(t, p)
	}, timedOut: &timedOut})
	require.False(t, timedOut, "encoding must run outside lifecycle locks")
	require.NotNil(t, fresh)
	fresh.Emit("barrier")
	require.Equal(t, ev("barrier"), recv(t, p.fc.out, "new connection after reentrant encoding"))
}

func TestClientConnVolatileRechecksGenerationAfterWritable(t *testing.T) {
	p := newPeer(t, 'C', hooks{})
	var fresh Conn
	wire := &callbackWritableClientConn{fakeConn: p.fc, check: func() bool {
		fresh = reconnectPeer(t, p)
		return true
	}}
	p.cl.dial = func(string) (engineclient.Conn, error) { return wire, nil }
	p.connect(t)
	p.nc.Volatile().Emit("stale")
	fresh.Emit("barrier")
	require.Equal(t, ev("barrier"), recv(t, p.fc.out, "replacement after reentrant Writable"))
}

func TestClientPreconnectConnEmitDoesNotReplayOnReconnect(t *testing.T) {
	emitted := make(chan struct{}, 1)
	p := newPeer(t, 'C', hooks{onError: func(c Conn, err error) {
		c.Emit("old connection")
		c.Compress(false).Emit("old flagged connection")
		// Shutdown may report another write failure after the assertion has read
		// its notification; callbacks must still be able to complete cleanup.
		select {
		case emitted <- struct{}{}:
		default:
		}
	}})
	require.NoError(t, p.cl.Connect())
	closeAtEnd(t, p.fc, p.conn())
	// ERROR can arrive before the server acknowledges the root namespace.
	p.send(t, `4"not yet connected"`)
	recv(t, emitted, "OnError sent through the current concrete Conn")
	p.cl.Socket("/").Emit("stable socket")
	fresh := reconnectPeer(t, p)
	fresh.Emit("barrier")
	require.Equal(t, ev("stable socket"), recv(t, p.fc.out, "stable Socket send survives reconnect"))
	require.Equal(t, ev("barrier"), recv(t, p.fc.out, "concrete Conn sends stayed in their generation"))
}

func TestClientStaleConnCannotCancelReplacementDial(t *testing.T) {
	for _, operation := range []string{"close", "disconnect"} {
		t.Run(operation, func(t *testing.T) {
			p := start(t, 'C', hooks{})
			old := p.nc
			require.NoError(t, p.cl.Close())
			recv(t, p.fc.closed, "old engine close")
			next := newFakeConn(t)
			entered, release := make(chan struct{}), make(chan struct{})
			releaseDial := sync.OnceFunc(func() { close(release) })
			defer releaseDial()
			p.cl.dial = func(string) (engineclient.Conn, error) {
				close(entered)
				<-release
				return next, nil
			}
			result := make(chan error, 1)
			go func() { result <- p.cl.Connect() }()
			recv(t, entered, "replacement dial started")
			if operation == "close" {
				require.NoError(t, old.Close())
			} else {
				old.Disconnect()
			}
			releaseDial()
			require.NoError(t, recv(t, result, "replacement dial completed"))
			closeAtEnd(t, next, p.conn())
			p.fc = next
			p.send(t, "0")
			fresh := recv(t, p.conns, "replacement namespace connected")
			fresh.Emit("barrier")
			require.Equal(t, ev("barrier"), recv(t, next.out, "replacement remains usable"))
		})
	}
}

func TestClientCurrentPreconnectConnCanClose(t *testing.T) {
	for _, operation := range []string{"close", "disconnect"} {
		t.Run(operation, func(t *testing.T) {
			requested := make(chan struct{}, 1)
			p := newPeer(t, 'C', hooks{onError: func(c Conn, err error) {
				require.False(t, c.Connected(), "ERROR arrived before namespace CONNECT")
				if operation == "close" {
					require.NoError(t, c.Close())
				} else {
					c.Disconnect()
				}
				requested <- struct{}{}
			}})
			require.NoError(t, p.cl.Connect())
			closeAtEnd(t, p.fc, p.conn())
			p.send(t, `4"refused"`)
			recv(t, requested, "OnError requested shutdown")
			recv(t, p.fc.closed, "current preconnect Conn closed its engine")
		})
	}
}

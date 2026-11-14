package socketio

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	client "github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// connectView is what OnConnect observes about the connecting socket.
type connectView struct {
	id      string
	rooms   []string
	sockets []string
	ids     []string
}

// welcomeServer registers on namespace nsp the handler socket.io 2.5.0 runs in
// its connection listener: it joins a room and broadcasts to the namespace, to
// the socket's ID room and to the joined room, broadcasts from the socket to the
// others, and emits to the socket. reject, when not nil, is OnConnect's result.
func welcomeServer(t *testing.T, nsp string, reject error) (*Server, string, chan connectView) {
	t.Helper()
	views := make(chan connectView, 4)
	s, url := defectServer(t, func(s *Server) {
		s.OnError(nsp, func(Conn, error) {})
		s.Of(nsp).Use(func(c Conn) error {
			// Middleware runs before the socket becomes a broadcast target.
			assert.Empty(t, c.Rooms())
			assert.NoError(t, s.Of(nsp).Emit("middleware"))
			return nil
		})
		s.OnConnect(nsp, func(c Conn) error {
			c.Join("lobby")
			view := connectView{id: c.ID(), rooms: c.Rooms(), ids: s.Of(nsp).SocketIDs()}
			for _, socket := range s.Of(nsp).Sockets() {
				view.sockets = append(view.sockets, socket.ID())
			}
			views <- view
			assert.True(t, s.BroadcastToNamespace(nsp, "all"))
			assert.NoError(t, s.Of(nsp).To(c.ID()).Emit("self"))
			assert.True(t, s.BroadcastToRoom(nsp, "lobby", "lobby"))
			assert.NoError(t, c.Broadcast().Emit("others"))
			assert.NoError(t, c.To(c.ID()).Emit("to-self-from-socket"))
			c.Emit("direct")
			return reject
		})
	})
	return s, url, views
}

// nspPrefix is the namespace part of a packet of nsp, "" for the root.
func nspPrefix(nsp string) string {
	if nsp == "/" {
		return ""
	}
	return nsp + ","
}

// TestOnConnectBroadcastsReachConnectingSocket pins the frames socket.io 2.5.0
// writes for the welcomeServer handler: CONNECT first, then every broadcast that
// selects the socket and the direct emit, in call order; a broadcast that excludes
// the socket and one issued by namespace middleware do not reach it.
func TestOnConnectBroadcastsReachConnectingSocket(t *testing.T) {
	for _, nsp := range []string{"/", "/welcome"} {
		t.Run(nsp, func(t *testing.T) {
			s, url, views := welcomeServer(t, nsp, nil)
			c := dialRaw(t, url)
			if nsp != "/" {
				require.Equal(t, "0", c.read())
				c.send("0" + nsp + ",")
			}
			p := nspPrefix(nsp)
			view := recv(t, views, "OnConnect")
			require.ElementsMatch(t, []string{view.id, "lobby"}, view.rooms)
			require.Equal(t, []string{view.id}, view.sockets)
			require.Equal(t, []string{view.id}, view.ids)
			require.Equal(t, "0"+p, c.read(), "CONNECT stays first")
			for _, event := range []string{"all", "self", "lobby", "direct"} {
				require.Equal(t, "2"+p+`["`+event+`"]`+"\n", c.read())
			}
			require.Equal(t, 1, s.RoomLen(nsp, view.id))
		})
	}
}

// TestOnConnectBroadcastsReachGoClient runs the welcomeServer handler for the Go
// client over websocket; the raw tests above use polling.
func TestOnConnectBroadcastsReachGoClient(t *testing.T) {
	for _, nsp := range []string{"/", "/welcome"} {
		t.Run(nsp, func(t *testing.T) {
			_, url, views := welcomeServer(t, nsp, nil)
			cl, err := client.New(url, client.WithTransports(websocket.Default))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			got := make(chan string, 8)
			socket := cl.Socket(nsp)
			for _, event := range []string{"middleware", "all", "self", "lobby", "others", "to-self-from-socket", "direct"} {
				socket.OnEvent(event, func(client.Conn) { got <- event })
			}
			require.NoError(t, cl.Connect())
			recv(t, views, "OnConnect")
			for _, want := range []string{"all", "self", "lobby", "direct"} {
				require.Equal(t, want, recv(t, got, want))
			}
		})
	}
}

// TestRejectedConnectReceivesNoBroadcasts: packets addressed to a socket whose
// OnConnect returns an error are discarded with its admission, and the socket
// leaves every room and the namespace's sockets.
func TestRejectedConnectReceivesNoBroadcasts(t *testing.T) {
	for _, nsp := range []string{"/", "/welcome"} {
		t.Run(nsp, func(t *testing.T) {
			s, url, views := welcomeServer(t, nsp, errors.New("denied"))
			c := dialRaw(t, url)
			if nsp != "/" {
				require.Equal(t, "0", c.read())
				c.send("0" + nsp + ",")
			}
			view := recv(t, views, "OnConnect")
			require.Equal(t, `4`+nspPrefix(nsp)+`"denied"`+"\n", c.read(), "only the rejection")
			require.Empty(t, s.Of(nsp).Sockets())
			require.Empty(t, s.Of(nsp).SocketIDs())
			require.Equal(t, 0, s.RoomLen(nsp, view.id))
			require.Equal(t, 0, s.RoomLen(nsp, "lobby"))
		})
	}
}

// TestBroadcastDuringOnConnectWaitsForConnect: while OnConnect runs, a broadcast
// from another goroutine selects the socket; its packet follows CONNECT, or is
// discarded when OnConnect rejects.
func TestBroadcastDuringOnConnectWaitsForConnect(t *testing.T) {
	for name, reject := range map[string]error{"accepted": nil, "rejected": errors.New("denied")} {
		t.Run(name, func(t *testing.T) {
			entered, release := make(chan Conn, 1), make(chan struct{})
			s, url := defectServer(t, func(s *Server) {
				s.OnError("/chat", func(Conn, error) {})
				s.OnConnect("/chat", func(c Conn) error { entered <- c; <-release; return reject })
				s.OnEvent("/", "barrier", func(c Conn) { c.Emit("barrier") })
			})
			c := dialRaw(t, url)
			require.Equal(t, "0", c.read())
			c.send("0/chat,")
			nc := recv(t, entered, "OnConnect")
			require.True(t, s.BroadcastToNamespace("/chat", "early"))
			require.True(t, s.BroadcastToRoom("/chat", nc.ID(), "early-self"))
			close(release)
			if reject != nil {
				require.Equal(t, "4/chat,\"denied\"\n", c.read())
				c.send(`2["barrier"]`)
				require.Equal(t, "2[\"barrier\"]\n", c.read(), "the held packets were discarded")
				return
			}
			require.Equal(t, "0/chat,", c.read())
			require.Equal(t, "2/chat,[\"early\"]\n", c.read())
			require.Equal(t, "2/chat,[\"early-self\"]\n", c.read())
		})
	}
}

// TestRejectedConnectDuringLibraryCloseHasNoLifecycle: a socket whose OnConnect
// returns an error gets neither disconnecting nor OnDisconnect when a library
// close (a queue overflow or a write failure) starts while OnConnect runs, and it
// leaves the namespace's sockets and rooms.
func TestRejectedConnectDuringLibraryCloseHasNoLifecycle(t *testing.T) {
	overflow := func(c, _ Conn, _ *fakeConn) {
		for i := range 4 {
			c.Emit("x", i)
		}
	}
	for _, tc := range []struct {
		name, nsp string
		opts      []Option
		connect   func(c, root Conn, f *fakeConn) // starts the library close
	}{
		{name: "root overflow", nsp: "/", opts: []Option{WithWriteBufferSize(1)}, connect: overflow},
		{name: "namespace overflow", nsp: "/n", opts: []Option{WithWriteBufferSize(1)}, connect: overflow},
		{name: "namespace write failure", nsp: "/n", connect: func(c, root Conn, f *fakeConn) {
			f.failWrite.Store(true)
			root.Emit("tick")
			assert.Eventually(t, func() bool { return !c.Connected() }, waitFor, 5*time.Millisecond, "the write failure starts a library close")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer(tc.opts...)
			f := newFakeConn(t)
			seen := make(chan string, 8)
			for _, nsp := range []string{"/", "/n"} {
				s.Of(nsp).On("disconnecting", func(c Conn, reason string) { seen <- c.Namespace() + " disconnecting: " + reason })
				s.OnDisconnect(nsp, func(c Conn, reason string) { seen <- c.Namespace() + " disconnect: " + reason })
				s.OnError(nsp, func(Conn, error) {})
			}
			roots, returned := make(chan Conn, 1), make(chan struct{})
			var root Conn
			s.OnConnect(tc.nsp, func(c Conn) error {
				defer close(returned)
				tc.connect(c, root, f)
				assert.False(t, c.Connected(), "a library close started")
				return errors.New("denied")
			})
			if tc.nsp != "/" {
				s.OnConnect("/", func(c Conn) error { roots <- c; return nil })
			}
			s.serveConn(f)
			if tc.nsp != "/" {
				root = recv(t, roots, "root OnConnect")
				require.Equal(t, "0", recv(t, f.out, "root CONNECT"))
				f.reads <- "0/n,"
			}
			recv(t, returned, "OnConnect returned")
			require.NoError(t, s.Close())
			for _, event := range drain(seen) {
				require.False(t, strings.HasPrefix(event, tc.nsp+" "), "the rejected socket got %q", event)
			}
			require.Empty(t, s.Of(tc.nsp).Sockets())
			require.Empty(t, s.Rooms(tc.nsp))
		})
	}
}

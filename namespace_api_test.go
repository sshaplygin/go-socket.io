package socketio

import (
	"errors"
	"github.com/alicebob/miniredis/v2"
	gorilla "github.com/gorilla/websocket"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServerOf(t *testing.T) {
	s := NewServer()
	defer func() { _ = s.Close() }()
	require.Same(t, s.Of(""), s.Of("/"))
	require.Same(t, s.Of("chat"), s.Of("/chat"))
	require.Equal(t, "/", s.Of("").Name())
	require.Equal(t, "/chat", s.Of("chat").Name())
	s.OnConnect("/", func(Conn) error { return nil })
	connected, events, disconnected, reported := make(chan string, 2), make(chan int, 2), make(chan string, 2), make(chan error, 2)
	s.Of("chat").OnConnect(func(Conn) error { connected <- "first"; return nil })
	s.Of("chat").OnEvent("event", func(_ Conn, n int) { events <- n })
	s.Of("chat").OnDisconnect(func(_ Conn, reason string) { disconnected <- reason })
	s.Of("chat").OnError(func(_ Conn, err error) { reported <- err })
	go func() { _ = s.Serve() }()
	ts := httptest.NewServer(s)
	defer ts.Close()
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	require.Equal(t, "first", recv(t, connected, "view OnConnect"))
	c.send(`2/chat,["event",7]`)
	require.Equal(t, 7, recv(t, events, "view event registration"))
	c.send("1/chat,")
	require.Equal(t, "client namespace disconnect", recv(t, disconnected, "view OnDisconnect"))
	second := errors.New("second")
	s.OnConnect("chat", func(Conn) error { connected <- "second"; return second })
	c.send("0/chat,")
	require.Equal(t, "4/chat,\"second\"\n", c.read())
	require.Equal(t, "second", recv(t, connected, "server replaces view OnConnect"))
	require.ErrorIs(t, recv(t, reported, "view OnError"), second)
}

func TestNamespaceMiddleware(t *testing.T) {
	for _, where := range []string{"middleware", "OnConnect"} {
		t.Run(where, func(t *testing.T) {
			var order []string
			var rejected Conn
			errs := make(chan error, 1)
			discs := make(chan struct{}, 1)
			ts := newTestServer(t, func(s *Server) {
				s.OnConnect("/", func(Conn) error { return nil })
				s.OnEvent("/", "echo", func(_ Conn, n int) int { return n })
				n := s.Of("chat")
				n.Use(func(c Conn) error {
					order = append(order, "first")
					if len(c.Rooms()) != 0 {
						return errors.New("ID room joined before middleware")
					}
					c.SetContext("middleware")
					return nil
				})
				n.Use(func(c Conn) error {
					order = append(order, "second")
					if where == "middleware" {
						return errors.New("denied")
					}
					return nil
				})
				n.OnConnect(func(c Conn) error {
					order = append(order, "connect")
					rejected = c
					c.Emit("must-not-leak")
					c.Join("temporary")
					if c.Context() != "middleware" {
						return errors.New("lost context")
					}
					return errors.New("denied")
				})
				n.OnError(func(_ Conn, err error) { errs <- err })
				n.OnDisconnect(func(Conn, string) { discs <- struct{}{} })
			})
			c := dialRaw(t, ts.URL)
			require.Equal(t, "0", c.read())
			c.send("0/chat,")
			require.Equal(t, "4/chat,\"denied\"\n", c.read())
			require.EqualError(t, recv(t, errs, "OnError"), "denied")
			want := []string{"first", "second"}
			if where == "OnConnect" {
				want = append(want, "connect")
			}
			require.Equal(t, want, order)
			if rejected != nil {
				require.Empty(t, rejected.Rooms())
			}
			require.Empty(t, discs)
			c.send(`27["echo",42]`)
			require.Equal(t, "37[42]\n", c.read())
		})
	}
	t.Run("root rejection ignores later CONNECT", func(t *testing.T) {
		rootErrors := make(chan bool, 1)
		srv, url := defectServer(t, func(s *Server) {
			s.Use("/", func(Conn) error { return errors.New("root denied") })
			s.OnConnect("/", func(Conn) error { t.Error("rejected OnConnect ran"); return nil })
			s.OnError("/", func(c Conn, err error) { rootErrors <- c == nil && err.Error() == "root denied" })
			s.OnConnect("/chat", func(Conn) error { t.Error("namespace after rejected root connected"); return nil })
		})
		c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(url, "http")+"/?EIO=3&transport=websocket", nil)
		require.NoError(t, err)
		defer func() { _ = c.Close() }()
		_, _, err = c.ReadMessage()
		require.NoError(t, err) // Engine.IO OPEN
		_, packet, err := c.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, "44\"root denied\"\n", string(packet))
		require.True(t, recv(t, rootErrors, "root OnError"))
		require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("40/chat,")))
		require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("40/missing,")))
		require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("2")))
		_, packet, err = c.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, "3", string(packet), "PING barrier: both CONNECT packets were ignored")

		require.Equal(t, 1, srv.Count())
		require.Empty(t, srv.Of("/").Sockets())
		require.Empty(t, srv.Of("/chat").Sockets())
	})
}

func TestNamespaceSend(t *testing.T) {
	var s *Server
	ts := newTestServer(t, func(server *Server) { s = server; s.OnConnect("/", func(Conn) error { return nil }) })
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	require.NoError(t, s.Of("/").Send("hello"))
	require.Equal(t, "2[\"message\",\"hello\"]\n", c.read())
}

func TestNamespaceBroadcastAcrossInstances(t *testing.T) {
	r := miniredis.RunT(t)
	a, b := NewServer(), NewServer()
	useRedis(t, a, r.Addr())
	useRedis(t, b, r.Addr())
	first, second := a.Of("/chat"), b.Of("/chat")
	waitRedisSubscribers(t, r, "socket.io-request#/chat#", 2)
	remote := newRedisTestConn("remote")
	b.getNamespace("/chat").Adapter().Join(remote.ID(), remote)
	// The publishing server has no local socket in this namespace.
	require.Empty(t, first.Sockets())
	require.NoError(t, first.Emit("remote-event", 42))
	expectEvent(t, remote, "remote-event [42]")
	require.NoError(t, first.Send("hello"))
	expectEvent(t, remote, "message [hello]")
	// Once both instances have sockets, the adapter reaches both of them.
	local := newRedisTestConn("local")
	a.getNamespace("/chat").Adapter().Join("one", local)
	require.NoError(t, second.Emit("both"))
	expectEvent(t, local, "both []")
	expectEvent(t, remote, "both []")
	require.Empty(t, local.events)
}

func TestNamespaceSockets(t *testing.T) {
	var s *Server
	ts := newTestServer(t, func(server *Server) {
		s = server
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnConnect("/chat", func(Conn) error { return nil })
	})
	first, second := dialRaw(t, ts.URL), dialRaw(t, ts.URL)
	require.Equal(t, "0", first.read())
	require.Equal(t, "0", second.read())
	first.send("0/chat,")
	second.send("0/chat,")
	require.Equal(t, "0/chat,", first.read())
	require.Equal(t, "0/chat,", second.read())
	n := s.Of("/chat")
	require.Len(t, n.Sockets(), 2)
	id := "/chat#" + first.conn.ID()
	socket, ok := n.Socket(id)
	require.True(t, ok)
	require.Equal(t, id, socket.ID())
	copy := n.Sockets()
	copy[0] = nil
	require.Len(t, n.Sockets(), 2)
	first.send("1/chat,")
	require.Eventually(t, func() bool { return len(n.Sockets()) == 1 }, time.Second, time.Millisecond)
	_, ok = n.Socket(id)
	require.False(t, ok)
	_, ok = n.Socket("missing")
	require.False(t, ok)
	// A later rejected connection never joins the connected-socket view.
	n.Use(func(Conn) error { return errors.New("later rejection") })
	first.send("0/chat,")
	require.Equal(t, "4/chat,\"later rejection\"\n", first.read())
	require.Len(t, n.Sockets(), 1)
}

func TestConnSend(t *testing.T) {
	acknowledged := make(chan string, 1)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnEvent("/", "send", func(c Conn) { c.Send("hello", func(reply string) { acknowledged <- reply }) })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send(`2["send"]`)
	require.Equal(t, "21[\"message\",\"hello\"]\n", c.read())
	c.send(`31["ok"]`)
	require.Equal(t, "ok", recv(t, acknowledged, "Send acknowledgement"))
}

func TestConnDisconnect(t *testing.T) {
	disconnected := make(chan string, 4)
	chat := make(chan Conn, 2)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnConnect("/chat", func(c Conn) error { chat <- c; return nil })
		s.OnEvent("/chat", "bye", func(c Conn) { c.Emit("before"); c.Disconnect(); c.Disconnect() })
		s.OnEvent("/", "echo", func(_ Conn, n int) int { return n })
		s.OnEvent("/", "close", func(c Conn) { _ = c.Close() })
		s.OnDisconnect("/chat", func(_ Conn, reason string) { disconnected <- "chat:" + reason })
		s.OnDisconnect("/", func(_ Conn, reason string) { disconnected <- "root:" + reason })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	stale := recv(t, chat, "chat connection")
	c.send(`2/chat,["bye"]`)
	require.Equal(t, "2/chat,[\"before\"]\n", c.read())
	require.Equal(t, "1/chat,", c.read())
	require.Equal(t, "chat:server namespace disconnect", recv(t, disconnected, "namespace disconnect"))
	require.Empty(t, stale.Rooms())
	c.send(`27["echo",42]`)
	require.Equal(t, "37[42]\n", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	stale.Disconnect() // must not remove a later connection to the same namespace.
	c.send(`2["close"]`)
	require.ElementsMatch(t, []string{"1", "1/chat,"}, []string{c.read(), c.read()})
	require.ElementsMatch(t, []string{"root:server namespace disconnect", "chat:server namespace disconnect"}, []string{recv(t, disconnected, "root close"), recv(t, disconnected, "chat close")})
}

func TestEventMiddleware(t *testing.T) {
	seen := make(chan []interface{}, 8)
	second := make(chan string, 8)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(c Conn) error {
			c.Use(func(event string, args []interface{}) error {
				seen <- append([]interface{}{event}, args...)
				if event == "blocked" {
					return errors.New("event denied")
				}
				return nil
			})
			c.Use(func(event string, _ []interface{}) error { second <- event; return nil })
			return nil
		})
		s.OnEvent("/", "blocked", func(Conn, int) { t.Error("blocked handler ran") })
		s.OnEvent("/", "echo", func(_ Conn, n int) int { return n })
		s.OnEvent("/", "noargs", func(Conn) {})
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send(`27["blocked",41]`)
	require.Equal(t, "4\"event denied\"\n", c.read())
	require.Equal(t, []interface{}{"blocked", 41}, recv(t, seen, "typed middleware"))
	require.Empty(t, second, "later middleware ran after rejection")
	c.send(`2["unknown",42,"generic"]`)
	require.Equal(t, []interface{}{"unknown", float64(42), "generic"}, recv(t, seen, "generic middleware"))
	require.Equal(t, "unknown", recv(t, second, "second middleware"))
	c.send(`51-["unknown-binary",{"_placeholder":true,"num":0}]`)
	w, err := c.conn.NextWriter(eioparser.Binary)
	require.NoError(t, err)
	_, err = w.Write([]byte{1, 2, 3})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	binary := recv(t, seen, "generic binary middleware")
	require.Equal(t, "unknown-binary", recv(t, second, "second binary middleware"))
	require.Equal(t, "unknown-binary", binary[0])
	require.Equal(t, []byte{1, 2, 3}, binary[1].(*parser.Buffer).Data)

	c.send(`2["noargs","ignored"]`)
	require.Equal(t, []interface{}{"noargs"}, recv(t, seen, "zero argument handler"))
	require.Equal(t, "noargs", recv(t, second, "second noargs middleware"))
	c.send(`28["echo",42]`)
	require.Equal(t, "38[42]\n", c.read())
	require.Equal(t, []interface{}{"echo", 42}, recv(t, seen, "following middleware"))
	require.Equal(t, "echo", recv(t, second, "second following middleware"))
}

func TestConnIDNamespace(t *testing.T) {
	connected := make(chan Conn, 2)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(c Conn) error { connected <- c; return nil })
		s.OnConnect("/chat", func(c Conn) error { connected <- c; return nil })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	root := recv(t, connected, "root")
	require.Equal(t, c.conn.ID(), root.ID())
	require.Contains(t, root.Rooms(), root.ID())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	chat := recv(t, connected, "chat")
	require.Equal(t, "/chat#"+root.ID(), chat.ID())
	require.Contains(t, chat.Rooms(), chat.ID())
}

func TestDisconnectDuringConnect(t *testing.T) {
	var server *Server
	reasons := make(chan string, 1)
	ts := newTestServer(t, func(s *Server) {
		server = s
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnConnect("/early", func(c Conn) error { c.Emit("hello"); c.Disconnect(); return nil })
		s.OnDisconnect("/early", func(_ Conn, reason string) { reasons <- reason })
		s.OnEvent("/", "echo", func(_ Conn, n int) int { return n })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send("0/early,")
	require.Equal(t, "0/early,", c.read())
	require.Equal(t, "2/early,[\"hello\"]\n", c.read())
	require.Equal(t, "1/early,", c.read())
	require.Equal(t, "server namespace disconnect", recv(t, reasons, "early disconnect"))
	require.Empty(t, server.Of("/early").Sockets())
	require.Empty(t, server.Rooms("/early"))
	c.send(`27["echo",42]`)
	require.Equal(t, "37[42]\n", c.read())
}

func TestNamespaceSocketLifecycleView(t *testing.T) {
	observed := make(chan bool, 2)
	ts := newTestServer(t, func(s *Server) {
		n := s.Of("/")
		n.OnConnect(func(Conn) error { return nil })
		n.OnEvent("disconnecting", func(c Conn, _ string) { _, ok := n.Socket(c.ID()); observed <- ok })
		n.OnDisconnect(func(c Conn, _ string) { _, ok := n.Socket(c.ID()); observed <- ok })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send("1")
	require.True(t, recv(t, observed, "socket visible during disconnecting"))
	require.False(t, recv(t, observed, "socket absent during OnDisconnect"))
}

func TestConnectQueueCapacity(t *testing.T) {
	p := start(t, 'S', hooks{connect: func(c Conn) error { flood(c, defaultWriteBufferSize); return nil }})
	for i := 0; i < defaultWriteBufferSize; i++ {
		require.Equal(t, ev("x", i), recv(t, p.fc.out, "buffered OnConnect event"))
	}
	require.False(t, isDone(p.fc.closed))
	require.Empty(t, p.errs)
}

func TestNamespaceConnectQueueCapacity(t *testing.T) {
	p := start(t, 'S', hooks{
		setup: func(s *Server) { s.limits.writeBufferSize = 1 },
		connect: func(c Conn) error {
			if c.Namespace() == "/chat" {
				c.Emit("welcome")
			}
			return nil
		},
	}, "/chat")
	p.send(t, "0/chat,")
	require.Equal(t, "0/chat,", recv(t, p.fc.out, "namespace CONNECT"))
	require.Equal(t, "2/chat,[\"welcome\"]\n", recv(t, p.fc.out, "pending event"))
	require.False(t, isDone(p.fc.closed))
	require.Empty(t, p.errs)
}

func TestNamespaceReconnectQueueBound(t *testing.T) {
	p := start(t, 'S', hooks{setup: func(s *Server) { s.limits.writeBufferSize = 1 }}, "/chat")
	// Keep the writer in the first application packet without queueing any
	// more application packets. Only CONNECT replies fill the queue below.
	p.stall(t, p.nc, 0)
	p.send(t, "0/chat,")
	p.send(t, "1/chat,")
	require.Equal(t, "/chat", recv(t, p.discs, "namespace disconnect"))
	p.send(t, "0/chat,")
	p.send(t, "1/chat,")
	require.Equal(t, "/chat", recv(t, p.discs, "namespace disconnect"))
	p.send(t, "0/chat,")
	recv(t, p.fc.closed, "bounded lifecycle queue overflow")
	p.overflowReported(t, "/chat")
	p.disconnected(t, "/", "/chat")
}

func TestNamespaceDisconnectPendingFullBudget(t *testing.T) {
	p := start(t, 'S', hooks{
		setup:   func(s *Server) { s.limits.writeBufferSize = 1 },
		connect: func(c Conn) error { c.Emit("welcome"); c.Disconnect(); return nil },
	})
	require.Equal(t, "2[\"welcome\"]\n", recv(t, p.fc.out, "pending event"))
	require.Equal(t, "1", recv(t, p.fc.out, "namespace DISCONNECT"))
	require.Equal(t, "/", recv(t, p.discs, "namespace disconnect"))
	require.False(t, isDone(p.fc.closed))
	require.Empty(t, p.errs)
}

func TestCloseControlsIncludeClaimedNamespaces(t *testing.T) {
	entered, resume := make(chan struct{}, 2), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	defer release()
	p := start(t, 'S', hooks{setup: func(s *Server) { s.limits.writeBufferSize = 1 }, disconnect: func(c Conn) {
		if c.Namespace() != "/" {
			entered <- struct{}{}
			<-resume
		}
	}}, "/a", "/b")
	a, b := p.join(t, "/a"), p.join(t, "/b")
	p.stall(t, p.nc, 0)
	finished := make(chan struct{}, 2)
	for _, nc := range []Conn{a, b} {
		go func(c Conn) { c.Disconnect(); finished <- struct{}{} }(nc)
	}
	recv(t, entered, "first claimed disconnect callback")
	recv(t, entered, "second claimed disconnect callback")
	require.NoError(t, p.nc.Close())
	p.nc.Emit("late")
	release()
	recv(t, finished, "first claimed disconnect completion")
	recv(t, finished, "second claimed disconnect completion")
	close(p.fc.release)
	require.Equal(t, ev("first"), recv(t, p.fc.out, "previous application packet"))
	packets := []string{recv(t, p.fc.out, "DISCONNECT"), recv(t, p.fc.out, "DISCONNECT"), recv(t, p.fc.out, "DISCONNECT")}
	require.ElementsMatch(t, []string{"1/a,", "1/b,", "1"}, packets)
	recv(t, p.fc.closed, "engine closed after controls drain")
	p.disconnected(t, "/", "/a", "/b")
	require.Empty(t, p.fc.out, "Emit after Close must be dropped")
	require.Empty(t, p.errs)
}

func TestCloseWaitsForClaimedDisconnectPackets(t *testing.T) {
	entered, resume := make(chan struct{}, 2), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	defer release()
	p := start(t, 'S', hooks{setup: func(s *Server) { s.limits.writeBufferSize = 1 }, disconnect: func(c Conn) {
		if c.Namespace() != "/" {
			entered <- struct{}{}
			<-resume
			c.Emit("late from claimed namespace")
		}
	}}, "/a", "/b")
	a, b := p.join(t, "/a"), p.join(t, "/b")
	p.stall(t, p.nc, 0)
	finished := make(chan struct{}, 2)
	for _, nc := range []Conn{a, b} {
		go func(c Conn) { c.Disconnect(); finished <- struct{}{} }(nc)
	}
	recv(t, entered, "first claimed disconnect callback")
	recv(t, entered, "second claimed disconnect callback")
	require.NoError(t, p.nc.Close())
	p.nc.Emit("late")
	release()
	recv(t, finished, "first claimed disconnect completion")
	recv(t, finished, "second claimed disconnect completion")
	close(p.fc.release)
	require.Equal(t, ev("first"), recv(t, p.fc.out, "previous application packet"))
	packets := []string{recv(t, p.fc.out, "DISCONNECT"), recv(t, p.fc.out, "DISCONNECT"), recv(t, p.fc.out, "DISCONNECT")}
	require.ElementsMatch(t, []string{"1/a,", "1/b,", "1"}, packets)
	recv(t, p.fc.closed, "engine closed after controls drain")
	p.disconnected(t, "/", "/a", "/b")
	require.Empty(t, p.fc.out, "Emit after Close must be dropped")
	require.Empty(t, p.errs)
}

func TestCloseDeadlineWithClaimedDisconnect(t *testing.T) {
	entered, resume := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	defer release()
	p := start(t, 'S', hooks{setup: func(s *Server) { s.limits.drainTimeout = 50 * time.Millisecond }, disconnect: func(c Conn) {
		if c.Namespace() == "/a" {
			close(entered)
			<-resume
		}
	}}, "/a")
	a := p.join(t, "/a")
	p.stall(t, p.nc, 0)
	finished := inBackground(a.Disconnect)
	recv(t, entered, "claimed disconnect callback")
	require.NoError(t, p.nc.Close())
	recv(t, p.fc.closed, "drain deadline during claimed callback")
	release()
	recv(t, finished, "claimed disconnect completion")
	p.disconnected(t, "/", "/a")
	require.Empty(t, p.errs)
}

func TestCloseFromClaimedDisconnectCallback(t *testing.T) {
	p := start(t, 'S', hooks{disconnect: func(c Conn) {
		if c.Namespace() == "/a" {
			_ = c.Close()
		}
	}}, "/a")
	a := p.join(t, "/a")
	done := inBackground(a.Disconnect)
	recv(t, done, "Disconnect callback calling Close")
	require.Equal(t, "1/a,", recv(t, p.fc.out, "namespace DISCONNECT"))
	require.Equal(t, "1", recv(t, p.fc.out, "root DISCONNECT"))
	recv(t, p.fc.closed, "engine closed")
	p.disconnected(t, "/", "/a")
}

func TestPendingConnectUsesSharedBudget(t *testing.T) {
	observed := make(chan bool, 1)
	p := start(t, 'S', hooks{connect: func(c Conn) error {
		if c.Namespace() == "/a" {
			flood(c, 2)
			observed <- !c.Connected()
		}
		return nil
	}}, "/a")
	p.stall(t, p.nc, defaultWriteBufferSize-1)
	p.send(t, "0/a,")
	require.True(t, recv(t, observed, "overflow inside pending connect"))
	p.overflowReported(t, "/a")
	p.disconnected(t, "/", "/a")
}

func TestRootAlwaysExists(t *testing.T) {
	ts := newTestServer(t, func(*Server) {})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
}

func TestNamespaceBroadcastOnce(t *testing.T) {
	connected := make(chan Conn, 2)
	s, url := defectServer(t, func(s *Server) {
		s.OnConnect("/", func(c Conn) error { c.Join("one"); c.Join("two"); connected <- c; return nil })
	})
	a, b := dialRaw(t, url), dialRaw(t, url)
	require.Equal(t, "0", a.read())
	require.Equal(t, "0", b.read())
	ca, cb := recv(t, connected, "first connection"), recv(t, connected, "second connection")
	require.True(t, s.BroadcastToNamespace("/", "news", "once"))
	ca.Emit("after")
	cb.Emit("after")
	for _, c := range []*rawClient{a, b} {
		require.Equal(t, "2[\"news\",\"once\"]\n", c.read())
		require.Equal(t, "2[\"after\"]\n", c.read(), "a broadcast must not be repeated for each room")
	}
}

package socketio

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
	redisadapter "github.com/sshaplygin/go-socket.io/adapter/redis"
)

// useRedis sets the Adapter of srv to s and closes srv at cleanup.
func useRedis(t *testing.T, srv *Server, addr string) {
	t.Helper()
	srv.SetAdapter(redisadapter.New(redisadapter.WithAddr(addr)))
	t.Cleanup(func() { _ = srv.Close() })
}

// delayRedisSubscriptions holds each PSUBSCRIBE that reaches s, and the commands queued behind it
// on the same connection, until d after the call. A broadcast subscribes with PSUBSCRIBE first,
// so its subscription registers late while commands on other connections complete, as a loaded
// Redis server may do. Call it after miniredis.RunT, so the hold ends before s closes.
func delayRedisSubscriptions(t *testing.T, s *miniredis.Miniredis, d time.Duration) {
	t.Helper()
	release := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(release) }) }
	timer := time.AfterFunc(d, stop)
	t.Cleanup(func() { timer.Stop(); stop() })
	s.Server().SetPreHook(func(_ *server.Peer, cmd string, _ ...string) bool {
		if cmd == "PSUBSCRIBE" {
			<-release
		}
		return false
	})
}

// serveAsync runs Serve in a goroutine and returns its result channel.
func serveAsync(srv *Server) <-chan error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve() }()
	return errc
}

// TestRedisConstructionErrorReturnedByServe checks that Serve returns the first recorded
// construction error and that a later registration does not rebuild the broadcast.
//
// Covers 1I-T7 (S).
func TestRedisConstructionErrorReturnedByServe(t *testing.T) {
	s := miniredis.RunT(t)
	srv := NewServer()
	useRedis(t, srv, s.Addr())
	s.Close()
	srv.OnConnect("/first", func(Conn) error { return nil })
	srv.OnConnect("/second", func(Conn) error { return nil })
	require.NoError(t, s.Restart())
	srv.OnEvent("/first", "e", func(Conn) {})

	select {
	case err := <-serveAsync(srv):
		require.ErrorContains(t, err, `"/first"`)
		require.NotContains(t, err.Error(), "/second")
		var opErr *net.OpError
		require.ErrorAs(t, err, &opErr)
	case <-time.After(time.Second):
		t.Fatal("Serve did not return the construction error")
	}
	require.Equal(t, -1, srv.RoomLen("/first", "r"))
}

// TestRedisFailedNamespaceRoomMethods checks the Server room methods of a namespace whose
// Redis broadcast could not be created.
//
// Covers 1I-T9 (S).
func TestRedisFailedNamespaceRoomMethods(t *testing.T) {
	s := miniredis.RunT(t)
	srv := NewServer()
	useRedis(t, srv, s.Addr())
	s.Close()
	srv.OnConnect("/f", func(Conn) error { return nil })

	c := newRedisTestConn("c")
	require.False(t, srv.JoinRoom("/f", "r", c), "JoinRoom")
	require.False(t, srv.LeaveRoom("/f", "r", c), "LeaveRoom")
	require.False(t, srv.LeaveAllRooms("/f", c), "LeaveAllRooms")
	require.False(t, srv.ClearRoom("/f", "r"), "ClearRoom")
	require.False(t, srv.BroadcastToRoom("/f", "r", "e"), "BroadcastToRoom")
	require.False(t, srv.BroadcastToNamespace("/f", "e"), "BroadcastToNamespace")
	require.Equal(t, -1, srv.RoomLen("/f", "r"))
	require.Nil(t, srv.Rooms("/f"))
	require.False(t, srv.ForEach("/f", "r", func(adapter.Socket) { t.Error("ForEach called its function") }))
	require.Empty(t, c.events)
}

// serveWithRedis sets the Adapter of srv to addr, runs Serve and waits until Serve passed
// its entry check, so that the handlers registered next fail only after it.
func serveWithRedis(t *testing.T, srv *Server, addr string) {
	t.Helper()
	useRedis(t, srv, addr)
	srv.served = make(chan struct{})
	serveAsync(srv)
	recv(t, srv.served, "Serve's entry check")
}

// TestRedisConstructionErrorFailsConnections checks that a connection to a namespace whose
// Redis broadcast could not be created fails before the namespace is registered.
//
// Covers 1I-T8 (S).
// Covers 1L-T10 (S).
func TestRedisConstructionErrorFailsConnections(t *testing.T) {
	var opErr *net.OpError
	t.Run("root", func(t *testing.T) {
		s, rec := miniredis.RunT(t), &recordingHandler{}
		setDefault(t, rec)
		p := newPeer(t, 'S', hooks{setup: func(srv *Server) { serveWithRedis(t, srv, s.Addr()); s.Close() }})
		p.srv.serveConn(p.fc)

		err := recv(t, p.nilErrs, "root OnError with a nil Conn")
		require.ErrorAs(t, err, &opErr)
		require.ErrorContains(t, err, `namespace "/"`)
		recv(t, p.fc.closed, "engine.io close")
		require.Zero(t, p.fc.texts.Load(), "socket.io packets written")
		require.Empty(t, drain(p.conns), "OnConnect calls")
		require.Empty(t, drain(p.discs), "OnDisconnect calls")
		require.Empty(t, drain(p.nilErrs), "a second report")
		require.Empty(t, drain(p.errs), "a report with a Conn")
		require.Equal(t, map[string]any{"/": err}, rec.byNsp(t, "socketio: namespace connect", "err", slog.LevelDebug))
	})
	t.Run("namespace", func(t *testing.T) {
		s, rec := miniredis.RunT(t), &recordingHandler{}
		setDefault(t, rec)
		p := start(t, 'S', hooks{setup: func(srv *Server) { serveWithRedis(t, srv, s.Addr()) }})
		addr := s.Addr()
		s.Close()
		// A new factory must dial instead of reusing the root namespace's
		// shared subscriber, whose buffered SUBSCRIBE may still succeed.
		p.srv.SetAdapter(redisadapter.New(redisadapter.WithAddr(addr)))
		errs, rooms := make(chan error, 4), make(chan []string, 4)
		p.srv.OnConnect("/a", func(Conn) error { t.Error("OnConnect of a failed namespace"); return nil })
		p.srv.OnDisconnect("/a", func(Conn, string) { t.Error("OnDisconnect of a failed namespace") })
		p.srv.OnError("/a", func(c Conn, err error) {
			c.Join("r")
			c.Leave("r")
			c.LeaveAll()
			errs <- err
			rooms <- c.Rooms()
		})
		p.send(t, "0/a")

		err := recv(t, errs, "OnError of /a")
		require.ErrorAs(t, err, &opErr)
		require.Nil(t, recv(t, rooms, "Rooms of /a"))
		recv(t, p.fc.closed, "engine.io close")
		p.disconnected(t, "/")
		require.Empty(t, drain(p.fc.out), "a CONNECT reply for /a")
		require.Empty(t, drain(errs), "a second report")
		require.Empty(t, drain(p.errs), "a report to root OnError")
		require.Empty(t, drain(p.nilErrs), "a report to root OnError")
		require.Equal(t, err, rec.byNsp(t, "socketio: namespace connect", "err", slog.LevelDebug)["/a"])
	})
}

// TestRedisServerCloseStopsBroadcasts checks that Server.Close stops the Redis connections of
// every namespace and that a handler registered after Close builds no broadcast.
//
// Covers 1I-T10 (S).
func TestRedisServerCloseStopsBroadcasts(t *testing.T) {
	s := miniredis.RunT(t)
	srv := NewServer()
	useRedis(t, srv, s.Addr())
	srv.OnConnect("/one", func(Conn) error { return nil })
	srv.OnConnect("/two", func(Conn) error { return nil })
	s.Close()
	srv.OnConnect("/three", func(Conn) error { return nil })
	require.NoError(t, s.Restart())
	require.Eventually(t, func() bool {
		return s.PubSubNumPat() == 1 &&
			s.PubSubNumSub("socket.io-request#/one#")["socket.io-request#/one#"] == 1 &&
			s.PubSubNumSub("socket.io-request#/two#")["socket.io-request#/two#"] == 1
	}, 2*time.Second, 5*time.Millisecond, "shared subscriber restored every namespace after reconnect")

	require.NotPanics(t, func() { require.NoError(t, srv.Close()) })
	require.Eventually(t, func() bool {
		return s.CurrentConnectionCount() == 0 && s.PubSubNumPat() == 0 && len(s.PubSubChannels("")) == 0
	}, time.Second, 5*time.Millisecond, "Redis connections or subscribers left after Close")

	dials := s.TotalConnectionCount()
	srv.OnConnect("/four", func(Conn) error { return nil })
	require.Equal(t, dials, s.TotalConnectionCount(), "a handler registered after Close dialled Redis")
	require.Equal(t, -1, srv.RoomLen("/four", "r"))
	select {
	case err := <-serveAsync(srv):
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after Close")
	}
}

// delayedProxy forwards each connection it accepts to addr after a delay of d.
func delayedProxy(t *testing.T, addr string, d time.Duration) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				time.Sleep(d)
				forward(c, addr)
			}()
		}
	}()
	return ln.Addr().String()
}

// forward copies between c and a new connection to addr until either side closes.
func forward(c net.Conn, addr string) {
	defer func() { _ = c.Close() }()
	up, err := net.Dial("tcp", addr)
	if err != nil {
		return
	}
	go func() { _, _ = io.Copy(up, c); _ = up.Close() }()
	_, _ = io.Copy(c, up)
}

// TestRedisConcurrentRegistrationBuildsOneBroadcast checks that concurrent registrations on
// one new namespace build one Redis broadcast, which Close stops.
//
// Covers 1I-T12 (S).
func TestRedisConcurrentRegistrationBuildsOneBroadcast(t *testing.T) {
	s := miniredis.RunT(t)
	addr := delayedProxy(t, s.Addr(), 50*time.Millisecond)
	factory := redisadapter.New(redisadapter.WithAddr(addr))
	settled := func() int { // the connections of the broadcasts built so far, once all reached Redis
		t.Helper()
		require.Eventually(t, func() bool { return s.PubSubNumPat() > 0 }, time.Second, 5*time.Millisecond)
		time.Sleep(150 * time.Millisecond)
		require.Equal(t, 1, s.PubSubNumPat(), "broadcasts subscribed")
		return s.CurrentConnectionCount()
	}
	closed := func(what string) {
		t.Helper()
		require.Eventually(t, func() bool { return s.CurrentConnectionCount() == 0 }, time.Second, 5*time.Millisecond, what)
	}
	single, err := factory("/single")
	require.NoError(t, err)
	want := settled()
	require.NoError(t, single.(io.Closer).Close())
	closed("the connections of a single broadcast")

	srv := NewServer()
	srv.OnConnect("/", func(Conn) error { return nil })
	useRedis(t, srv, addr)
	observed := make(chan string, 8)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			event := fmt.Sprintf("e%d", i)
			srv.OnEvent("/x", event, func(Conn) { observed <- event })
		}()
	}
	close(start)
	wg.Wait()
	require.Equal(t, want, settled(), "Redis connections of the registered namespace")
	srv.OnConnect("/x", func(Conn) error { return nil })
	go func() { _ = srv.Serve() }()
	ts := httptest.NewServer(srv)
	defer ts.Close()
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send("0/x,")
	require.Equal(t, "0/x,", c.read())
	for i := 0; i < 8; i++ {
		event := fmt.Sprintf("e%d", i)
		c.send("2/x,[\"" + event + "\"]")
		require.Equal(t, event, recv(t, observed, "concurrently registered event"))
	}

	require.NoError(t, srv.Close())
	closed("Redis connections left after Close")
}

// TestRedisCloseStopsRacingRegistration checks that Close waits for a registration that
// is building a Redis broadcast and stops that broadcast too. AUTH through the delayed
// proxy keeps each of the registration's two dials waiting for 100 ms.
func TestRedisCloseStopsRacingRegistration(t *testing.T) {
	s := miniredis.RunT(t)
	s.RequireAuth("secret")
	srv := NewServer()
	srv.SetAdapter(redisadapter.New(redisadapter.WithAddr(delayedProxy(t, s.Addr(), 100*time.Millisecond)), redisadapter.WithPassword("secret")))
	go srv.OnConnect("/x", func(Conn) error { return nil })
	require.Eventually(t, func() bool {
		srv.createMu.Lock()
		building := srv.creating["/x"] != nil
		srv.createMu.Unlock()
		return building
	}, time.Second, time.Millisecond, "namespace factory is building outside the registry lock")

	require.NoError(t, srv.Close())
	require.NotNil(t, srv.getNamespace("/x"), "Close returned before the registration finished")
	require.Eventually(t, func() bool { return s.CurrentConnectionCount() == 0 && s.PubSubNumPat() == 0 },
		time.Second, 5*time.Millisecond, "Redis connections of the racing registration left after Close")
}

package socketio

import (
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

// useRedis sets the Adapter of srv to s and closes srv at cleanup.
func useRedis(t *testing.T, srv *Server, addr string) {
	t.Helper()
	ok, err := srv.Adapter(&RedisAdapterOptions{Addr: addr})
	require.True(t, ok)
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })
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
	srv := NewServer(nil)
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
	srv := NewServer(nil)
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
	require.False(t, srv.ForEach("/f", "r", func(Conn) { t.Error("ForEach called its function") }))
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
		require.Equal(t, map[string]any{"/": err}, rec.byNsp("socketio: namespace connect", "err"))
	})
	t.Run("namespace", func(t *testing.T) {
		s, rec := miniredis.RunT(t), &recordingHandler{}
		setDefault(t, rec)
		p := start(t, 'S', hooks{setup: func(srv *Server) { serveWithRedis(t, srv, s.Addr()) }})
		s.Close()
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
		require.Equal(t, err, rec.byNsp("socketio: namespace connect", "err")["/a"])
	})
}

// TestRedisServerCloseStopsBroadcasts checks that Server.Close stops the Redis connections of
// every namespace and that a handler registered after Close builds no broadcast.
//
// Covers 1I-T10 (S).
func TestRedisServerCloseStopsBroadcasts(t *testing.T) {
	s := miniredis.RunT(t)
	srv := NewServer(nil)
	useRedis(t, srv, s.Addr())
	srv.OnConnect("/one", func(Conn) error { return nil })
	srv.OnConnect("/two", func(Conn) error { return nil })
	s.Close()
	srv.OnConnect("/three", func(Conn) error { return nil })
	require.NoError(t, s.Restart())
	require.Eventually(t, func() bool { return s.PubSubNumPat() == 2 }, 2*time.Second, 5*time.Millisecond)

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
	opts := &RedisAdapterOptions{Addr: delayedProxy(t, s.Addr(), 50*time.Millisecond), Prefix: "socket.io", Network: "tcp"}
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
	single, err := newRedisBroadcast("/single", opts)
	require.NoError(t, err)
	want := settled()
	single.close()
	closed("the connections of a single broadcast")

	srv := NewServer(nil)
	useRedis(t, srv, opts.Addr)
	closed("the connection of Adapter")
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			srv.OnEvent("/x", fmt.Sprintf("e%d", i), func(Conn) {})
		}()
	}
	close(start)
	wg.Wait()
	require.Equal(t, want, settled(), "Redis connections of the registered namespace")
	require.Len(t, srv.getNamespace("/x").events, 8)

	require.NoError(t, srv.Close())
	closed("Redis connections left after Close")
}

// TestRedisCloseDoesNotWaitForSilentRedis checks that Close returns within the Redis dial
// timeout while a handler registration waits for a Redis server that accepted the
// connection and never answers AUTH, and that the registration records the failure.
func TestRedisCloseDoesNotWaitForSilentRedis(t *testing.T) {
	defer func(d time.Duration) { redisDialTimeout = d }(redisDialTimeout)
	redisDialTimeout = 100 * time.Millisecond
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 4)
	go func() {
		for c, err := ln.Accept(); err == nil; c, err = ln.Accept() {
			accepted <- c
		}
	}()
	srv := NewServer(nil)
	srv.redisAdapter = getOptions(&RedisAdapterOptions{Addr: ln.Addr().String(), Password: "secret"})
	go srv.OnConnect("/x", func(Conn) error { return nil })
	c := recv(t, accepted, "the registration's Redis dial")
	t.Cleanup(func() { _ = c.Close() }) // at the latest, ends the registration's dial

	closed := make(chan error, 1)
	go func() { closed <- srv.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close still waits for a registration whose Redis server never answers AUTH")
	}
	require.ErrorContains(t, srv.getNamespace("/x").err, `"/x"`)
}

// TestRedisCloseDoesNotWaitForSilentSubscriber checks that the Redis dial timeout also
// bounds the subscriber dial: the publishing connection reaches a Redis server that
// requires AUTH, the subscriber connection is accepted and never answered, and Close
// still returns while the registration waits; the registration records the failure and
// closes the publishing connection.
func TestRedisCloseDoesNotWaitForSilentSubscriber(t *testing.T) {
	defer func(d time.Duration) { redisDialTimeout = d }(redisDialTimeout)
	redisDialTimeout = 100 * time.Millisecond
	s := miniredis.RunT(t)
	s.RequireAuth("secret")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	silent := make(chan net.Conn, 4)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go forward(c, s.Addr())
		for c, err := ln.Accept(); err == nil; c, err = ln.Accept() {
			silent <- c
		}
	}()
	srv := NewServer(nil)
	srv.redisAdapter = getOptions(&RedisAdapterOptions{Addr: ln.Addr().String(), Password: "secret"})
	go srv.OnConnect("/x", func(Conn) error { return nil })
	c := recv(t, silent, "the registration's subscriber dial")
	t.Cleanup(func() { _ = c.Close() }) // at the latest, ends the subscriber dial
	require.Equal(t, 1, s.TotalConnectionCount(), "publishing connections that reached Redis")

	closed := make(chan error, 1)
	go func() { closed <- srv.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close still waits for a registration whose Redis subscriber dial is never answered")
	}
	require.ErrorContains(t, srv.getNamespace("/x").err, `"/x"`)
	require.Eventually(t, func() bool { return s.CurrentConnectionCount() == 0 }, time.Second, 5*time.Millisecond,
		"the publishing connection left open after the failed construction")
}

// TestRedisCloseStopsRacingRegistration checks that Close waits for a registration that
// is building a Redis broadcast and stops that broadcast too. AUTH through the delayed
// proxy keeps each of the registration's two dials waiting for 100 ms.
func TestRedisCloseStopsRacingRegistration(t *testing.T) {
	s := miniredis.RunT(t)
	s.RequireAuth("secret")
	srv := NewServer(nil)
	ok, err := srv.Adapter(&RedisAdapterOptions{Addr: delayedProxy(t, s.Addr(), 100*time.Millisecond), Password: "secret"})
	require.True(t, ok)
	require.NoError(t, err)
	go srv.OnConnect("/x", func(Conn) error { return nil })
	require.Eventually(t, func() bool {
		if srv.createMu.TryLock() {
			srv.createMu.Unlock()
			return false
		}
		return true
	}, time.Second, time.Millisecond, "the registration holding the creation mutex")

	require.NoError(t, srv.Close())
	require.NotNil(t, srv.getNamespace("/x"), "Close returned before the registration finished")
	require.Eventually(t, func() bool { return s.CurrentConnectionCount() == 0 && s.PubSubNumPat() == 0 },
		time.Second, 5*time.Millisecond, "Redis connections of the racing registration left after Close")
}

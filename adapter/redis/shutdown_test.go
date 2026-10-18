package redis

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/adapter"
)

// startRegistration lowers redisDialTimeout, registers namespace "/x" on its own goroutine and
// at cleanup waits for it, then restores the timeout: the registration reads the variable, which
// the next test changes.
func startRegistration(t *testing.T, srv *socketio.Server) {
	t.Helper()
	d := redisDialTimeout
	redisDialTimeout = 300 * time.Millisecond
	t.Cleanup(func() { redisDialTimeout = d })
	reg := make(chan struct{})
	go func() { srv.OnConnect("/x", func(socketio.Conn) error { return nil }); close(reg) }()
	t.Cleanup(func() { recv(t, reg, "the registration to end") })
}

// TestRedisCloseDoesNotWaitForSilentRedis checks that Close returns within the Redis dial
// timeout while a handler registration waits for a Redis server that accepted the
// connection and never answers AUTH, and that the registration records the failure.
func TestRedisCloseDoesNotWaitForSilentRedis(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 4)
	go func() {
		for c, err := ln.Accept(); err == nil; c, err = ln.Accept() {
			accepted <- c
		}
	}()
	srv := socketio.NewServer()
	failures := make(chan error, 1)
	factory := New(WithAddr(ln.Addr().String()), WithPassword("secret"))
	srv.SetAdapter(func(name string) (adapter.Adapter, error) {
		bc, err := factory(name)
		if err != nil {
			failures <- fmt.Errorf("namespace %q: %w", name, err)
		}
		return bc, err
	})
	startRegistration(t, srv)
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
	require.Nil(t, srv.Of("/x").Adapter())
	require.ErrorContains(t, recv(t, failures, "recorded construction failure"), `"/x"`)
}

// TestRedisCloseDoesNotWaitForSilentSubscriber checks that the Redis dial timeout also
// bounds the subscriber dial: the publishing connection reaches a Redis server that
// requires AUTH, the subscriber connection is accepted and never answered, and Close
// still returns while the registration waits; the registration records the failure and
// closes the publishing connection.
func TestRedisCloseDoesNotWaitForSilentSubscriber(t *testing.T) {
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
	srv := socketio.NewServer()
	failures := make(chan error, 1)
	factory := New(WithAddr(ln.Addr().String()), WithPassword("secret"))
	srv.SetAdapter(func(name string) (adapter.Adapter, error) {
		bc, err := factory(name)
		if err != nil {
			failures <- fmt.Errorf("namespace %q: %w", name, err)
		}
		return bc, err
	})
	startRegistration(t, srv)
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
	require.Nil(t, srv.Of("/x").Adapter())
	require.ErrorContains(t, recv(t, failures, "recorded construction failure"), `"/x"`)
	require.Eventually(t, func() bool { return s.CurrentConnectionCount() == 0 }, time.Second, 5*time.Millisecond,
		"the publishing connection left open after the failed construction")
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
func recv[T any](t *testing.T, channel <-chan T, what string) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

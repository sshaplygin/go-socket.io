package client

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
)

// Reentrant Connect returns after OPEN, but its read handlers must wait for the
// reconnect callback that called it to return.
// Covers C3
func TestClientReconnectHandlerSerial(t *testing.T) {
	for _, mode := range []string{"attempt", "error", "failed", "success"} {
		t.Run(mode, func(t *testing.T) {
			cl, err := New("http://unused/", &Options{ReconnectionAttempts: 1, ReconnectionDelay: time.Millisecond, RandomizationFactor: -1})
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			first, second, third := newFakeConn(t), newFakeConn(t), newFakeConn(t)
			second.reads = make(chan string, 1)
			second.reads <- "0"
			third.reads = make(chan string, 1)
			third.reads <- "0"
			dials := 0
			cl.dial = func(string) (engineio.Conn, error) {
				dials++
				if dials == 1 {
					return first, nil
				}
				if dials == 2 && (mode == "error" || mode == "failed") {
					return nil, errors.New("down")
				}
				if dials == 2 {
					return second, nil
				}
				return third, nil
			}
			ready := make(chan Conn, 4)
			cl.OnConnect(func(c Conn) error { ready <- c; return nil })
			callbackEntered, callbackRelease := make(chan error, 1), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(callbackRelease) })
			callback := func() {
				if mode == "success" {
					_ = cl.Close()
				}
				callbackEntered <- cl.Connect()
				<-callbackRelease
			}
			switch mode {
			case "attempt":
				cl.OnReconnectAttempt(func(int) { callback() })
			case "error":
				cl.OnReconnectError(func(error) { callback() })
			case "failed":
				cl.OnReconnectFailed(callback)
			case "success":
				cl.OnReconnect(func(int) { callback() })
			}
			require.NoError(t, cl.Connect())
			first.reads <- "0"
			recv(t, ready, "initial CONNECT")
			close(first.peerGone)
			require.NoError(t, recv(t, callbackEntered, "reentrant Connect"))
			select {
			case <-ready:
				t.Fatal("OnConnect overlapped a reconnect callback")
			case <-time.After(30 * time.Millisecond):
			}
			once.Do(func() { close(callbackRelease) })
			recv(t, ready, "CONNECT after reconnect callback returned")
		})
	}
}

// Covers C3
func TestClientReconnectCancellation(t *testing.T) {
	for _, mode := range []string{"client", "socket", "attempt callback"} {
		t.Run(mode, func(t *testing.T) {
			cl, err := New("http://unused/", &Options{Timeout: -1, ReconnectionDelay: time.Millisecond, RandomizationFactor: -1})
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			first, late := newFakeConn(t), newFakeConn(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			dials := 0
			cl.dial = func(string) (engineio.Conn, error) {
				dials++
				if dials == 1 {
					return first, nil
				}
				close(entered)
				<-release
				return late, nil
			}
			ready := make(chan Conn, 1)
			cl.OnConnect(func(c Conn) error { ready <- c; return nil })
			disconnecting, finishDisconnect := make(chan struct{}), make(chan struct{})
			var finishOnce sync.Once
			defer finishOnce.Do(func() { close(finishDisconnect) })
			cl.OnDisconnect(func(Conn, string) { close(disconnecting); <-finishDisconnect })
			events := make(chan string, 4)
			attempted := make(chan struct{}, 1)
			cl.OnReconnectAttempt(func(int) {
				if mode == "attempt callback" {
					_ = cl.Close()
				}
				attempted <- struct{}{}
			})
			cl.OnReconnect(func(int) { events <- "success" })
			cl.OnReconnectError(func(error) { events <- "error" })
			cl.OnReconnectFailed(func() { events <- "failed" })
			require.NoError(t, cl.Connect())
			first.reads <- "0"
			recv(t, ready, "initial connect")
			old := cl.conn
			close(first.peerGone)
			recv(t, disconnecting, "old disconnect handler")
			require.Empty(t, attempted, "retry must wait for old handlers")
			finishOnce.Do(func() { close(finishDisconnect) })
			recv(t, attempted, "retry attempt")
			if mode == "attempt callback" {
				recv(t, old.readStopped, "old read ended")
				require.Empty(t, entered, "Close inside attempt callback cancels before dialing")
				return
			}
			recv(t, entered, "unbounded retry dial")
			cl.state.mu.Lock()
			run := cl.state.retry
			cl.state.mu.Unlock()
			require.NotNil(t, run)
			if mode == "client" {
				require.NoError(t, cl.Close())
			} else {
				require.NoError(t, cl.Socket("/").Close())
			}
			recv(t, run.done, "retry loop cancellation")
			once.Do(func() { close(release) })
			recv(t, late.closed, "late retry result closed")
			require.Empty(t, events)
		})
	}
}

// Covers C11
func TestClientVolatileBeforeConnect(t *testing.T) {
	cl, err := New("http://unused/", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	fc := newFakeConn(t)
	cl.dial = func(string) (engineio.Conn, error) { return fc, nil }
	ready := make(chan Conn, 1)
	cl.OnConnect(func(c Conn) error { ready <- c; return nil })
	require.NoError(t, cl.Connect())
	nc, ok := cl.conn.namespaces.Get(rootNamespace)
	require.True(t, ok)
	require.False(t, nc.Connected())
	require.NoError(t, nc.Volatile().Emit("drop"))
	nc.Emit("plain")
	require.Empty(t, fc.out)
	fc.reads <- "0"
	recv(t, ready, "CONNECT reply")
	require.Equal(t, ev("plain"), recv(t, fc.out, "buffered ordinary event"))
	nc.Emit("barrier")
	require.Equal(t, ev("barrier"), recv(t, fc.out, "absence barrier"))
	require.Empty(t, fc.out)
}

func TestClientBackoff(t *testing.T) {
	cl, err := New("http://unused", nil)
	require.NoError(t, err)
	require.Equal(t, time.Second, cl.options.ReconnectionDelay)
	require.Equal(t, 5*time.Second, cl.options.ReconnectionDelayMax)
	require.Equal(t, 0.5, cl.options.RandomizationFactor)
	opts := Options{ReconnectionDelay: 100 * time.Millisecond, ReconnectionDelayMax: 500 * time.Millisecond, RandomizationFactor: 0.5}
	// backo2's single random sample controls both magnitude and sign.
	require.Equal(t, 90*time.Millisecond, reconnectDelay(opts, 1, 0.2))
	require.Equal(t, 230*time.Millisecond, reconnectDelay(opts, 2, 0.3))
	require.Equal(t, 500*time.Millisecond, reconnectDelay(opts, 4, 0.9))
	require.Equal(t, 500*time.Millisecond, reconnectDelay(opts, 10000, 0))
	opts.RandomizationFactor = -1
	for i, want := range []time.Duration{100, 200, 400, 500} {
		require.Equal(t, want*time.Millisecond, reconnectDelay(opts, i+1, 0.9))
	}
}

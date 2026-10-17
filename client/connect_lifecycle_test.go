package client

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/stretchr/testify/require"
)

// Covers C4
func TestClientCloseCancelsPendingConnect(t *testing.T) {
	cl, err := New("http://unused/", &Options{Timeout: -1})
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	late := newFakeConn(t)
	cl.dial = func(string) (engineio.Conn, error) { close(entered); <-release; return late, nil }
	result := make(chan error, 1)
	go func() { result <- cl.Connect() }()
	recv(t, entered, "pending unbounded dial")
	require.NoError(t, cl.Close())
	require.ErrorIs(t, recv(t, result, "cancelled Connect"), io.ErrClosedPipe)
	once.Do(func() { close(release) })
	recv(t, late.closed, "late connection closed")
}

// Socket.destroy closes the manager while opening when its last socket leaves.
// Covers C4
// Covers C10
func TestClientLastSocketCloseCancelsDial(t *testing.T) {
	for _, timeout := range []time.Duration{-1, time.Hour} {
		t.Run(timeout.String(), func(t *testing.T) {
			cl, err := New("http://unused/", &Options{Timeout: timeout})
			require.NoError(t, err)
			fc := newFakeConn(t)
			t.Cleanup(func() { _ = cl.Close() })
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			cl.dial = func(string) (engineio.Conn, error) { close(entered); <-release; return fc, nil }
			result := make(chan error, 1)
			go func() { result <- cl.Connect() }()
			recv(t, entered, "pending dial")
			require.NoError(t, cl.Socket("/").Close())
			select {
			case err := <-result:
				require.ErrorIs(t, err, io.ErrClosedPipe)
			case <-time.After(time.Second):
				t.Fatal("last Socket.Close did not cancel Connect")
			}
			once.Do(func() { close(release) })
			recv(t, fc.closed, "late dial result discarded")
			require.Nil(t, cl.conn)
		})
	}
}

// Covers C4
func TestClientConcurrentConnectAndRetry(t *testing.T) {
	cl, err := New("http://unused/", nil)
	require.NoError(t, err)
	refused := errors.New("dial rejected")
	cl.dial = func(string) (engineio.Conn, error) { return nil, refused }
	require.ErrorIs(t, cl.Connect(), refused)
	fc := newFakeConn(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	cl.dial = func(string) (engineio.Conn, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return fc, nil
	}
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() { results <- cl.Connect() }()
	}
	recv(t, entered, "single shared dial")
	close(release)
	for i := 0; i < 20; i++ {
		require.NoError(t, recv(t, results, "concurrent Connect"))
	}
	require.EqualValues(t, 1, calls.Load())
	closeAtEnd(t, fc, cl.conn)
	require.NoError(t, cl.Connect(), "Connect on an open manager is idempotent")
	require.NoError(t, cl.Close())
}

// Registering after CONNECT still invokes the callback on the read goroutine,
// and a callback failure has the same reporting and shutdown as an immediate one.
// Covers C2
func TestClientLateConnectHandler(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			cl, err := New("http://unused/", nil)
			require.NoError(t, err)
			fc := newFakeConn(t)
			cl.dial = func(string) (engineio.Conn, error) { return fc, nil }
			cl.OnEvent("barrier", func(Conn) string { return "ready" })
			errs := make(chan error, 1)
			cl.OnError(func(_ Conn, err error) { errs <- err })
			require.NoError(t, cl.Connect())
			closeAtEnd(t, fc, cl.conn)
			fc.reads <- "0"
			fc.reads <- `27["barrier"]`
			require.Equal(t, "37[\"ready\"]\n", recv(t, fc.out, "CONNECT barrier"))
			called := make(chan Conn, 2)
			refused := errors.New("late callback rejected")
			cl.OnConnect(func(c Conn) error {
				called <- c
				if fail {
					return refused
				}
				return nil
			})
			require.Equal(t, "/", recv(t, called, "late OnConnect").Namespace())
			if fail {
				require.ErrorIs(t, recv(t, errs, "late callback error"), refused)
				recv(t, fc.closed, "late rejection closes engine")
			} else {
				cl.OnConnect(func(c Conn) error { called <- c; return nil })
				fc.reads <- `28["barrier"]`
				require.Equal(t, "38[\"ready\"]\n", recv(t, fc.out, "handler replacement barrier"))
				require.Empty(t, called, "replacing an already delivered handler does not call it again")
				require.NoError(t, cl.Close())
			}
		})
	}
}

// A namespace closed while its CONNECT is in flight cannot be revived by the reply.
// Covers C2
// Covers C10
func TestClientLateConnectReplyAfterClose(t *testing.T) {
	p := newPeer(t, 'C', hooks{events: map[string]interface{}{"barrier": func(Conn) string { return "ready" }}}, "/chat")
	require.NoError(t, p.cl.Connect())
	closeAtEnd(t, p.fc, p.conn())
	require.Equal(t, "0/chat,", recv(t, p.fc.out, "namespace CONNECT"))
	p.send(t, "0")
	recv(t, p.conns, "root CONNECT")
	require.NoError(t, p.cl.Socket("/chat").Close())
	require.Equal(t, "1/chat,", recv(t, p.fc.out, "namespace DISCONNECT"))
	p.send(t, "0/chat,")
	p.send(t, "0") // a duplicate root reply also cannot fire OnConnect twice
	p.send(t, `27["barrier"]`)
	require.Equal(t, "37[\"ready\"]\n", recv(t, p.fc.out, "late reply barrier"))
	require.Empty(t, p.conns)
	require.Empty(t, p.discs, "a never-connected socket has no disconnect callback")
	require.NoError(t, p.cl.Close())
}

// A default named socket may connect before the application asks for the root socket.
// Covers C2
func TestClientRootSocketAddedAfterConnect(t *testing.T) {
	cl, err := New("http://unused/chat", nil)
	require.NoError(t, err)
	fc := newFakeConn(t)
	cl.dial = func(string) (engineio.Conn, error) { return fc, nil }
	chatReady := make(chan struct{}, 1)
	cl.OnConnect(func(Conn) error { chatReady <- struct{}{}; return nil })
	require.NoError(t, cl.Connect())
	closeAtEnd(t, fc, cl.conn)
	require.Equal(t, "0/chat,", recv(t, fc.out, "named CONNECT"))
	fc.reads <- "0"
	fc.reads <- "0/chat,"
	recv(t, chatReady, "default named CONNECT")
	root := cl.Socket("/")
	ready := make(chan Conn, 1)
	root.OnConnect(func(c Conn) error { ready <- c; return nil })
	require.Equal(t, fc.ID(), recv(t, ready, "cached root CONNECT").ID())
	require.Empty(t, fc.out, "root creation must not send another CONNECT")
	require.NoError(t, cl.Close())
}

// A closed connection can be replaced explicitly after all old workers have exited.
// Covers C4
func TestClientConnectAfterTransportFailure(t *testing.T) {
	p := start(t, 'C', hooks{})
	old := p.conn()
	close(p.fc.peerGone)
	recv(t, old.done, "old connection close")
	next := newFakeConn(t)
	p.cl.dial = func(string) (engineio.Conn, error) { return next, nil }
	require.NoError(t, p.cl.Connect())
	closeAtEnd(t, next, p.conn())
	select {
	case next.reads <- "0":
	case <-time.After(waitFor):
		t.Fatal("new reader did not start")
	}
	recv(t, p.conns, "new namespace CONNECT")
	require.NoError(t, p.cl.Close())
}

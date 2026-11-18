package websocket

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

func TestWebsocketSetReadDeadline(t *testing.T) {
	at := assert.New(t)
	must := assert.New(t)

	tran := &Transport{}
	conn := make(chan transport.Conn, 1)
	handler := func(w http.ResponseWriter, r *http.Request) {
		c, err := tran.Accept(w, r)
		require.NoError(t, err)

		conn <- c
	}
	httpSvr := httptest.NewServer(http.HandlerFunc(handler))
	defer httpSvr.Close()

	u, err := url.Parse(httpSvr.URL)
	require.NoError(t, err)

	u.Scheme = "ws"

	header := make(http.Header)
	cc, err := tran.Dial(u, header)
	require.NoError(t, err)

	defer func() {
		must.NoError(cc.Close())
	}()

	sc := <-conn
	defer func() {
		must.NoError(sc.Close())
	}()

	err = cc.SetReadDeadline(time.Now().Add(time.Second / 10))
	require.NoError(t, err)

	_, _, _, err = cc.NextReader()
	require.Error(t, err)

	timeout, ok := err.(net.Error)
	at.True(ok)
	at.True(timeout.Timeout())

	op, ok := err.(net.Error)
	at.True(ok)
	at.True(op.Timeout())
}

// TestWebsocketCloseIdempotent checks that closing a connection a second
// time is not reported as an error. engineio's client.Close and the client's
// serve goroutine both close the transport connection, in either order.
func TestWebsocketCloseIdempotent(t *testing.T) {
	tran := &Transport{}
	conn := make(chan transport.Conn, 1)
	httpSvr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := tran.Accept(w, r)
		require.NoError(t, err)
		conn <- c
	}))
	defer httpSvr.Close()

	u, err := url.Parse(httpSvr.URL)
	require.NoError(t, err)
	u.Scheme = "ws"
	cc, err := tran.Dial(u, make(http.Header))
	require.NoError(t, err)
	sc := <-conn

	require.NoError(t, cc.Close())
	require.NoError(t, cc.Close(), "second Close")
	require.NoError(t, sc.Close())
	require.NoError(t, sc.Close(), "second Close")
}

// TestWebsocketWriterSecondCloseKeepsLock checks that closing a frame writer
// again, after the next writer has taken the write lock, fails without
// releasing the lock that the next writer holds.
func TestWebsocketWriterSecondCloseKeepsLock(t *testing.T) {
	tran := &Transport{}
	accepted := make(chan transport.Conn, 1)
	httpSvr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := tran.Accept(w, r)
		require.NoError(t, err)
		accepted <- c
	}))
	defer httpSvr.Close()

	u, err := url.Parse(httpSvr.URL)
	require.NoError(t, err)
	u.Scheme = "ws"
	cc, err := tran.Dial(u, make(http.Header))
	require.NoError(t, err)
	defer func() { _ = cc.Close() }()
	sc := <-accepted
	defer func() { _ = sc.Close() }()

	ws := cc.(*conn).ws
	first, err := ws.NextWriter(eioparser.String)
	require.NoError(t, err)
	require.NoError(t, first.Close())
	second, err := ws.NextWriter(eioparser.String)
	require.NoError(t, err)

	err = first.Close()
	require.False(t, ws.writeLocker.TryLock(), "a stale Close released the open writer's lock")
	require.True(t, ws.writing.Load(), "a stale Close cleared the open writer's state")
	require.ErrorIs(t, err, io.ErrClosedPipe, "second Close of a finished writer")
	require.NoError(t, second.Close())
	require.True(t, ws.writeLocker.TryLock(), "Close of the open writer releases the lock")
	ws.writeLocker.Unlock()
}

func TestWebsocketWriterConcurrentCloseUnlocksOnce(t *testing.T) {
	tran := &Transport{}
	accepted := make(chan transport.Conn, 1)
	httpSvr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := tran.Accept(w, r)
		require.NoError(t, err)
		accepted <- c
	}))
	defer httpSvr.Close()

	u, err := url.Parse(httpSvr.URL)
	require.NoError(t, err)
	u.Scheme = "ws"
	cc, err := tran.Dial(u, make(http.Header))
	require.NoError(t, err)
	defer func() { _ = cc.Close() }()
	sc := <-accepted
	defer func() { _ = sc.Close() }()

	ws := cc.(*conn).ws
	for i := 0; i < 200; i++ {
		w, err := ws.NextWriter(eioparser.String)
		require.NoError(t, err)
		errs := make(chan error, 2)
		start := make(chan struct{})
		for j := 0; j < 2; j++ {
			go func() {
				<-start
				errs <- w.Close()
			}()
		}
		close(start)
		first, second := <-errs, <-errs
		if first != nil {
			first, second = second, first
		}
		require.NoError(t, first, "one Close finishes the frame")
		require.ErrorIs(t, second, io.ErrClosedPipe, "the other Close reports the finished writer")
		require.True(t, ws.writeLocker.TryLock(), "the frame's lock is released once")
		ws.writeLocker.Unlock()
	}
}

// requireTimeout checks that err has an error reporting Timeout in its chain, on
// which the session reports "ping timeout". Gorilla replaces the
// os.ErrDeadlineExceeded of its connection with such an error of its own.
func requireTimeout(t *testing.T, err error) {
	t.Helper()
	var timeout interface{ Timeout() bool }
	require.True(t, errors.As(err, &timeout) && timeout.Timeout(), "not a timeout: %v", err)
}

// TestWebsocketDeadlineReportsTimeout checks the heartbeat expiry paths of the
// websocket transport: a read deadline inside a message and then between
// messages, and a write deadline.
func TestWebsocketDeadlineReportsTimeout(t *testing.T) {
	tran := &Transport{}
	conn := make(chan transport.Conn, 1)
	httpSvr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := tran.Accept(w, r)
		require.NoError(t, err)
		conn <- c
	}))
	defer httpSvr.Close()
	u, err := url.Parse(httpSvr.URL)
	require.NoError(t, err)
	u.Scheme = "ws"
	cc, err := tran.Dial(u, make(http.Header))
	require.NoError(t, err)
	defer func() { _ = cc.Close() }()
	sc := <-conn
	defer func() { _ = sc.Close() }()

	// The client sends the first fragments of a message and stops: the read
	// deadline expires inside the message.
	w, err := cc.NextWriter(eioparser.String, eioparser.MESSAGE)
	require.NoError(t, err)
	_, err = w.Write(make([]byte, 16<<10))
	require.NoError(t, err)
	require.NoError(t, sc.SetReadDeadline(time.Now().Add(50*time.Millisecond)))
	_, _, r, err := sc.NextReader()
	require.NoError(t, err)
	_, err = io.ReadAll(r)
	requireTimeout(t, err)
	_, _, _, err = sc.NextReader()
	requireTimeout(t, err)
	require.NoError(t, w.Close())

	require.NoError(t, sc.SetWriteDeadline(time.Now().Add(-time.Millisecond)))
	sw, err := sc.NextWriter(eioparser.String, eioparser.MESSAGE)
	require.NoError(t, err)
	_, err = sw.Write([]byte("late"))
	if err == nil {
		err = sw.Close()
	}
	requireTimeout(t, err)
}

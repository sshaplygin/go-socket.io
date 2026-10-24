package websocket

import (
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

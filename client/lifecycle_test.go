package client_test

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
)

// rawServer is an engine.io server whose single session the test drives
// packet by packet, to exercise the Go client's packet handlers.
type rawServer struct {
	t    *testing.T
	conn chan engineio.Conn
	url  string
}

func newRawServer(t *testing.T) *rawServer {
	t.Helper()
	eio := engineio.NewServer(shortPingOptions()...)
	ts := httptest.NewServer(eio)
	rs := &rawServer{t: t, conn: make(chan engineio.Conn, 1), url: ts.URL}
	go func() {
		c, err := eio.Accept()
		if err == nil {
			rs.conn <- c
		}
	}()
	t.Cleanup(func() {
		_ = eio.Close()
		ts.Close()
	})
	return rs
}

func (rs *rawServer) accept() *rawClient {
	rs.t.Helper()
	c := recv(rs.t, rs.conn, "engine.io session")
	rs.t.Cleanup(func() { _ = c.Close() })
	return &rawClient{t: rs.t, conn: c}
}

// connectRawClient connects a Go client with the given handlers to a raw
// engine.io server and completes the root CONNECT exchange.
func connectRawClient(t *testing.T, setup func(*client.Client)) *rawClient {
	t.Helper()
	rs := newRawServer(t)
	cl, err := client.New(rs.url)
	require.NoError(t, err)
	connected := make(chan struct{}, 1)
	cl.OnConnect(func(client.Conn) error {
		connected <- struct{}{}
		return nil
	})
	setup(cl)
	require.NoError(t, cl.Connect())
	t.Cleanup(func() { _ = cl.Close() })

	srv := rs.accept()
	srv.send("0")
	recv(t, connected, "client OnConnect")
	return srv
}

// TestClientEventAndDisconnect drives the Go client through server CONNECT,
// an event and a server DISCONNECT.
func TestClientEventAndDisconnect(t *testing.T) {
	events := make(chan int, 1)
	reasons := make(chan string, 1)
	srv := connectRawClient(t, func(cl *client.Client) {
		cl.OnEvent("num", func(_ client.Conn, n int) { events <- n })
		cl.OnDisconnect(func(_ client.Conn, reason string) { reasons <- reason })
	})

	srv.send(`2["num",5]`)
	require.Equal(t, 5, recv(t, events, "client event"))

	// The client reports the server-initiated disconnect reason.
	srv.send("1")
	require.Equal(t, "io server disconnect", recv(t, reasons, "client OnDisconnect"))
}

// TestClientDecodeErrorReachesOnError sends a malformed event to the Go
// client; the decode error is reported to OnError.
func TestClientDecodeErrorReachesOnError(t *testing.T) {
	errs := make(chan error, 1)
	srv := connectRawClient(t, func(cl *client.Client) {
		cl.OnEvent("num", func(client.Conn, int) {})
		cl.OnError(func(_ client.Conn, err error) { errs <- err })
	})

	srv.send(`2["num","five"]`)
	require.Error(t, recv(t, errs, "client OnError"))
}

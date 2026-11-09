package engineio_test

import (
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// heldOpenWait bounds how long a held session handshake waits for its release,
// so a test that forgets to release cannot hang the suite.
const heldOpenWait = 10 * time.Second

// newHeldOpenServer returns a server whose polling sessions finish their
// handshake only when release is called.
//
// The server marks the session open before it delivers the OPEN packet, so
// from the moment the client has the handshake response Server.Clients lists the
// session. Accept does not hand it out until the session's Init returns, which
// is a few microseconds after delivery. This server makes that window last
// until release: the OPEN packet is delivered immediately, but Init does not
// return, and Accept does not hand out the connection, before release is called.
//
// release is idempotent. Tests defer it so that Server.Close never waits for a
// handshake that is still held.
func newHeldOpenServer(t *testing.T) (s *engineio.Server, release func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	s = engineio.NewServer(engineio.WithTransports(
		&heldOpenTransport{Transport: &polling.Transport{}, gate: gate},
		websocket.Default,
	))
	return s, release
}

// heldOpenTransport wraps the polling transport; see newHeldOpenServer.
type heldOpenTransport struct {
	transport.Transport
	gate <-chan struct{}
}

func (t *heldOpenTransport) Accept(w http.ResponseWriter, r *http.Request) (transport.Conn, error) {
	conn, err := t.Transport.Accept(w, r)
	if err != nil {
		return nil, err
	}
	return &heldOpenConn{Conn: conn, gate: t.gate}, nil
}

// heldOpenConn delays the return of the OPEN packet writer's Close until the
// gate opens. The packet itself is already handed to the transport by then.
// It forwards the optional interfaces the session looks up on its connection.
type heldOpenConn struct {
	transport.Conn
	gate <-chan struct{}
}

func (c *heldOpenConn) NextWriter(ft eioparser.FrameType, pt eioparser.PacketType) (io.WriteCloser, error) {
	w, err := c.Conn.NextWriter(ft, pt)
	if err != nil || pt != eioparser.OPEN {
		return w, err
	}
	return &heldOpenWriter{WriteCloser: w, gate: c.gate}, nil
}

type heldOpenWriter struct {
	io.WriteCloser
	gate <-chan struct{}
}

func (w *heldOpenWriter) Close() error {
	err := w.WriteCloser.Close()
	select {
	case <-w.gate:
	case <-time.After(heldOpenWait):
	}
	return err
}

func (c *heldOpenConn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.Conn.(http.Handler).ServeHTTP(w, r)
}

func (c *heldOpenConn) SetSessionID(id string) {
	if t, ok := c.Conn.(interface{ SetSessionID(string) }); ok {
		t.SetSessionID(id)
	}
}

func (c *heldOpenConn) SetReadLimit(n int64) {
	if t, ok := c.Conn.(interface{ SetReadLimit(int64) }); ok {
		t.SetReadLimit(n)
	}
}

func (c *heldOpenConn) CloseGracefully() error {
	if t, ok := c.Conn.(interface{ CloseGracefully() error }); ok {
		return t.CloseGracefully()
	}
	return c.Conn.Close()
}

func (c *heldOpenConn) Drained() <-chan struct{} {
	if t, ok := c.Conn.(interface{ Drained() <-chan struct{} }); ok {
		return t.Drained()
	}
	done := make(chan struct{})
	close(done)
	return done
}

func (c *heldOpenConn) Writable() bool {
	t, ok := c.Conn.(interface{ Writable() bool })
	return ok && t.Writable()
}

func (c *heldOpenConn) Abort() error {
	if t, ok := c.Conn.(interface{ Abort() error }); ok {
		return t.Abort()
	}
	return c.Conn.Close()
}

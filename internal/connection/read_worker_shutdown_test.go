package connection

import (
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/stretchr/testify/require"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

type trackedHeaderConn struct {
	*fakeConn
	entered, returned chan struct{}
	calls             atomic.Int32
}

func (c *trackedHeaderConn) NextReader() (eioparser.FrameType, io.ReadCloser, error) {
	if c.calls.Add(1) == 1 {
		close(c.entered)
		defer close(c.returned)
	}
	return c.fakeConn.NextReader()
}

func TestIdleHeaderWorkerStopsBeforeConnectionCompletion(t *testing.T) {
	p := newPeer(t, 'S', hooks{})
	wire := &trackedHeaderConn{fakeConn: p.fc, entered: make(chan struct{}), returned: make(chan struct{})}
	p.serveConn(wire)
	p.nc = recv(t, p.conns, "root connected")
	require.Equal(t, "0", recv(t, p.fc.out, "root CONNECT"))
	closeAtEnd(t, p.fc, p.connection)
	recv(t, wire.entered, "header worker blocked in NextReader")
	require.NoError(t, p.connection.Close())
	select {
	case <-p.connection.done:
	case <-time.After(time.Second):
		t.Fatal("connection completion did not join its blocked header worker")
	}
	select {
	case <-wire.returned:
	default:
		t.Fatal("connection completed before header worker returned")
	}
	p.connection.Wait()
}

package client

import (
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// trackedFrameConn records every frame reader it returns.
type trackedFrameConn struct {
	*fakeConn
	frames chan *trackedFrame
}

type trackedFrame struct {
	io.ReadCloser
	closed atomic.Bool
}

func (f *trackedFrame) Close() error { f.closed.Store(true); return f.ReadCloser.Close() }

func (c *trackedFrameConn) NextReader() (eioparser.FrameType, io.ReadCloser, error) {
	ft, r, err := c.fakeConn.NextReader()
	if err != nil {
		return ft, r, err
	}
	frame := &trackedFrame{ReadCloser: r}
	c.frames <- frame
	return ft, frame, nil
}

// A packet that fails to decode stops the read loop while the decoder still
// holds its frame. engineio.Conn requires the caller to close every frame
// reader, so the loop closes that frame before it reports that it stopped,
// whether or not the transport keeps a resource the leak gate would report.
func TestMalformedPacketClosesFrameReader(t *testing.T) {
	cl, err := newTestManager(t, "http://unused/", WithReconnection(false))
	require.NoError(t, err)
	wire := &trackedFrameConn{fakeConn: newFakeConn(t), frames: make(chan *trackedFrame, 8)}
	cl.dial = func(string) (engineclient.Conn, error) { return wire, nil }
	connected := make(chan struct{}, 1)
	cl.OnConnect(func(Conn) error { connected <- struct{}{}; return nil })
	cl.OnError(func(Conn, error) {})
	require.NoError(t, cl.Connect())
	send := func(frame string) {
		select {
		case wire.reads <- frame:
		case <-time.After(waitFor):
			t.Fatalf("timed out sending %q", frame)
		}
	}
	send("0")
	recv(t, connected, "root OnConnect")
	c := cl.state.conn
	closeAtEnd(t, wire.fakeConn, c)
	send("9")
	recv(t, c.readStopped, "reader stop after malformed packet")
	frames := drain(wire.frames)
	require.NotEmpty(t, frames)
	for i, frame := range frames {
		require.True(t, frame.closed.Load(), "frame %d is still open after the reader stopped", i)
	}
}

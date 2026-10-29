package socketio

import (
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

func TestMalformedPacketReleasesFrameReader(t *testing.T) {
	disconnected := make(chan struct{}, 1)
	srv, addr := defectServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnError("/", func(Conn, error) {})
		s.OnDisconnect("/", func(Conn, string) { disconnected <- struct{}{} })
	})
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(addr, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	for i := 0; i < 2; i++ {
		_, _, err := c.ReadMessage()
		require.NoError(t, err)
	}
	require.NoError(t, c.WriteMessage(websocket.TextMessage, []byte("49")))
	recv(t, disconnected, "disconnect after malformed packet")
	require.NoError(t, srv.Close())
	require.NoError(t, c.Close())
	require.Eventually(t, func() bool { return len(leakedModuleStacks()) == 0 }, time.Second, 10*time.Millisecond,
		"frame reader or another library worker remains after close")
}

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
// reader, so the loop closes that frame before the connection completes,
// whether or not the transport keeps a resource the leak gate would report.
func TestMalformedPacketClosesFrameReader(t *testing.T) {
	p := newConnectionPeer(t, 'S', connectionHooks{})
	wire := &trackedFrameConn{fakeConn: p.fc, frames: make(chan *trackedFrame, 8)}
	p.serveConn(wire)
	p.nc = recv(t, p.conns, "root OnConnect")
	require.Equal(t, "0", recv(t, p.fc.out, "root CONNECT"))
	closeAtEnd(t, p.fc, p.connection.done)
	p.send(t, "9")
	recv(t, p.connection.done, "close after malformed packet")
	frames := drain(wire.frames)
	require.NotEmpty(t, frames)
	for i, frame := range frames {
		require.True(t, frame.closed.Load(), "frame %d is still open after the connection completed", i)
	}
}

// A polling POST whose Socket.IO packet fails to decode is aborted while its
// body is still unread. Closing the packet reader must not decode further from
// the released body: the connection still reports its disconnect, Server.Close
// does not wait on it and no library goroutine remains.
func TestMalformedPollingPacketReleasesPayload(t *testing.T) {
	disconnected := make(chan string, 1)
	srv, addr := defectServer(t, func(s *Server) {
		s.OnError("/", func(Conn, error) {})
		s.OnDisconnect("/", func(_ Conn, reason string) { disconnected <- reason })
	})
	c := dialRaw(t, addr)
	require.Equal(t, "0", c.read())
	c.send("9" + strings.Repeat("x", 100000))
	recv(t, disconnected, "disconnect after malformed polling packet")
	closed := make(chan error, 1)
	go func() { closed <- srv.Close() }()
	require.NoError(t, recv(t, closed, "Server.Close"))
	require.NoError(t, c.conn.Close())
	require.Eventually(t, func() bool { return len(leakedModuleStacks()) == 0 }, time.Second, 10*time.Millisecond,
		"payload reader or another library worker remains after close")
}

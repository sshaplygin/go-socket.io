package socketio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/googollee/go-socket.io/engineio/session"
)

// stallConn is an engineio.Conn for Server.serveConn. The first allowed
// frames are recorded on frames; every later NextWriter blocks until release
// or Close (allowed < 0 never blocks), and none succeeds after Close.
// NextReader returns frames from reads until Close.
type stallConn struct {
	id      string
	allowed int32
	writes  atomic.Int32

	frames    chan string
	reads     chan string
	stalled   chan struct{}
	stallOnce sync.Once
	release   chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

func newStallConn(id string, allowed int32) *stallConn {
	return &stallConn{
		id:      id,
		allowed: allowed,
		frames:  make(chan string, 256),
		reads:   make(chan string, 1),
		stalled: make(chan struct{}),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (s *stallConn) NextWriter(session.FrameType) (io.WriteCloser, error) {
	if n := s.writes.Add(1); s.allowed >= 0 && n > s.allowed {
		s.stallOnce.Do(func() { close(s.stalled) })
		select {
		case <-s.release:
		case <-s.closed:
		}
	}
	select {
	case <-s.closed:
		return nil, io.EOF
	default:
		return &frameWriter{out: s.frames}, nil
	}
}

func (s *stallConn) NextReader() (session.FrameType, io.ReadCloser, error) {
	select {
	case frame := <-s.reads:
		return session.TEXT, io.NopCloser(bytes.NewBufferString(frame)), nil
	case <-s.closed:
		return session.TEXT, nil, io.EOF
	}
}

func (s *stallConn) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func (s *stallConn) ID() string                { return s.id }
func (s *stallConn) URL() url.URL              { return url.URL{} }
func (s *stallConn) LocalAddr() net.Addr       { return nil }
func (s *stallConn) RemoteAddr() net.Addr      { return nil }
func (s *stallConn) RemoteHeader() http.Header { return nil }
func (s *stallConn) SetContext(interface{})    {}
func (s *stallConn) Context() interface{}      { return nil }

type frameWriter struct {
	bytes.Buffer
	out chan<- string
}

func (w *frameWriter) Close() error {
	w.out <- w.String()
	return nil
}

type connErr struct {
	id  string
	err error
}

type backpressureServer struct {
	*Server
	connected   chan Conn
	errs        chan connErr
	disconnects chan string
}

// newBackpressureServer returns a server whose root handlers join every
// connection to room "r" and record OnError and OnDisconnect without blocking.
func newBackpressureServer(t *testing.T) *backpressureServer {
	srv := &backpressureServer{
		Server:      NewServer(nil),
		connected:   make(chan Conn, 4),
		errs:        make(chan connErr, 64),
		disconnects: make(chan string, 4),
	}
	t.Cleanup(func() { _ = srv.Close() })

	srv.OnConnect("/", func(c Conn) error {
		c.Join("r")
		srv.connected <- c
		return nil
	})
	srv.OnError("/", func(c Conn, err error) {
		if c == nil {
			return
		}
		select {
		case srv.errs <- connErr{id: c.ID(), err: err}:
		default:
		}
	})
	srv.OnDisconnect("/", func(c Conn, _ string) {
		select {
		case srv.disconnects <- c.ID():
		default:
		}
	})

	return srv
}

// serve runs the socket.io connection over fc and waits for its connect packet.
func (srv *backpressureServer) serve(t *testing.T, fc *stallConn) Conn {
	t.Helper()
	srv.serveConn(fc)
	t.Cleanup(func() { _ = fc.Close() })

	require.Equal(t, "0", recv(t, fc.frames, "connect packet of "+fc.id))
	return recv(t, srv.connected, "OnConnect of "+fc.id)
}

// expectOverflow waits for the overflow error and the disconnect of fc.
func (srv *backpressureServer) expectOverflow(t *testing.T, fc *stallConn) {
	t.Helper()
	ev := recv(t, srv.errs, "OnError of "+fc.id)
	require.Equal(t, fc.id, ev.id)
	require.ErrorIs(t, ev.err, errWriteBufferFull)
	require.Equal(t, fc.id, recv(t, srv.disconnects, "OnDisconnect of "+fc.id))
	recv(t, fc.closed, "engine.io close of "+fc.id)
}

func inBackground(f func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	return done
}

// TestBackpressureStalledMemberDoesNotBlockRoom checks that a room member
// whose transport never accepts a write neither delays the broadcast nor the
// other member, and is closed once its queue overflows.
func TestBackpressureStalledMemberDoesNotBlockRoom(t *testing.T) {
	srv := newBackpressureServer(t)
	stalled := newStallConn("stalled", 1)
	healthy := newStallConn("healthy", -1)
	srv.serve(t, stalled)
	srv.serve(t, healthy)

	// One packet held by the stalled writer, a full queue, and one more. Each
	// broadcast waits for the healthy member's frame so that its own queue
	// never fills up.
	for i := 0; i < defaultWriteBufferSize+2; i++ {
		recv(t, inBackground(func() { srv.BroadcastToRoom("/", "r", "msg", i) }), "broadcast past the stalled member")
		require.Equal(t, fmt.Sprintf("2[\"msg\",%d]\n", i), recv(t, healthy.frames, "broadcast to the healthy member"))
	}

	srv.expectOverflow(t, stalled)
	// OnDisconnect runs before Close makes the connection leave its rooms.
	require.Eventually(t, func() bool { return srv.RoomLen("/", "r") == 1 }, waitFor, time.Millisecond)
	select {
	case <-healthy.closed:
		t.Fatal("the healthy member was closed")
	default:
	}
}

// TestBackpressureQueueCapacity checks that exactly defaultWriteBufferSize
// packets wait behind a stalled writer, the next one closes the connection,
// and only the first overflow is reported.
func TestBackpressureQueueCapacity(t *testing.T) {
	srv := newBackpressureServer(t)
	fc := newStallConn("stalled", 1)
	nc := srv.serve(t, fc)

	nc.Emit("msg")
	recv(t, fc.stalled, "the writer to take the first packet")

	recv(t, inBackground(func() {
		for i := 0; i < defaultWriteBufferSize; i++ {
			nc.Emit("msg")
		}
	}), "packets queued behind the stalled writer")
	require.Len(t, nc.(*namespaceConn).writeChan, defaultWriteBufferSize)

	recv(t, inBackground(func() { nc.Emit("msg"); nc.Emit("msg"); nc.Emit("msg") }), "the overflowing Emits")
	srv.expectOverflow(t, fc)
	for len(srv.errs) > 0 {
		require.NotErrorIs(t, (<-srv.errs).err, errWriteBufferFull, "a second overflow report")
	}
}

// TestBackpressureOverflowInCloseFromOnError checks that Close called from
// OnError, whose OnDisconnect overflows the queue of a stalled writer, does not
// deadlock. The overflow report waits for the goroutine that runs OnError, so
// closeOnOverflow must not run on the emitter.
func TestBackpressureOverflowInCloseFromOnError(t *testing.T) {
	srv := newBackpressureServer(t)
	srv.OnError("/", func(c Conn, err error) {
		if c != nil && !errors.Is(err, errWriteBufferFull) {
			_ = c.Close()
		}
	})
	srv.OnDisconnect("/", func(c Conn, _ string) {
		for i := 0; i < defaultWriteBufferSize+2; i++ {
			c.Emit("msg")
		}
		srv.disconnects <- c.ID()
	})
	fc := newStallConn("stalled", 2)
	nc := srv.serve(t, fc)

	nc.Emit("bad", make(chan int)) // fails to encode, so the writer reports it
	require.Equal(t, fc.id, recv(t, srv.disconnects, "OnDisconnect of "+fc.id))
	recv(t, fc.closed, "engine.io close of "+fc.id)
}

// TestBackpressureOverflowDisconnectsOnReadGoroutine checks that a handler
// overflowing its own queue sees the engine.io connection closed while it runs,
// but OnDisconnect only after it returns, as the one-goroutine contract needs.
func TestBackpressureOverflowDisconnectsOnReadGoroutine(t *testing.T) {
	srv := newBackpressureServer(t)
	fc := newStallConn("stalled", 1)
	var disconnected atomic.Bool
	srv.OnDisconnect("/", func(c Conn, _ string) {
		disconnected.Store(true)
		srv.disconnects <- c.ID()
	})
	early := make(chan bool, 1)
	srv.OnEvent("/", "flood", func(c Conn) {
		for i := 0; i < defaultWriteBufferSize+2; i++ {
			c.Emit("msg")
		}
		<-fc.closed // closed by the overflow or, on failure, by the test cleanup
		early <- disconnected.Load()
	})
	srv.serve(t, fc)

	fc.reads <- `2["flood"]`
	require.False(t, recv(t, early, "the overflowing handler"), "OnDisconnect ran while the overflowing handler was running")
	srv.expectOverflow(t, fc)
}

// TestBackpressureCloseWritesQueuedPackets checks that a normal Close writes
// the packets queued before it and only then closes the engine.io connection.
func TestBackpressureCloseWritesQueuedPackets(t *testing.T) {
	srv := newBackpressureServer(t)
	fc := newStallConn("slow", 1)
	nc := srv.serve(t, fc)

	for i := 0; i < 5; i++ {
		nc.Emit("msg", i)
	}
	recv(t, fc.stalled, "the writer to take the first packet")
	closed := inBackground(func() { _ = nc.Close() })
	require.Equal(t, fc.id, recv(t, srv.disconnects, "OnDisconnect of "+fc.id))
	close(fc.release)
	recv(t, closed, "Close")

	require.Len(t, fc.frames, 5, "packets written before Close returned")
	for i := 0; i < 5; i++ {
		require.Equal(t, fmt.Sprintf("2[\"msg\",%d]\n", i), <-fc.frames)
	}
}

// TestBackpressureCloseStalledWriterIsBounded checks that Close stops waiting
// for queued packets once closeWait has passed, also with a full queue: the
// flush token takes the slot that write leaves free.
func TestBackpressureCloseStalledWriterIsBounded(t *testing.T) {
	srv := newBackpressureServer(t)
	srv.OnConnect("/", func(c Conn) error {
		c.(*namespaceConn).closeWait = 10 * time.Millisecond
		srv.connected <- c
		return nil
	})
	fc := newStallConn("stalled", 1)
	nc := srv.serve(t, fc)

	nc.Emit("msg")
	recv(t, fc.stalled, "the writer to take the first packet")
	for i := 0; i < defaultWriteBufferSize; i++ {
		nc.Emit("msg")
	}
	require.Len(t, nc.(*namespaceConn).writeChan, defaultWriteBufferSize)
	recv(t, inBackground(func() { _ = nc.Close() }), "Close past the stalled writer")
	require.Empty(t, srv.errs, "overflow reported")
	recv(t, fc.closed, "engine.io close of "+fc.id)
}

// TestBackpressureConnectErrorSkipsFlush checks that Close does not wait for
// packets that OnConnect queued before it failed: no writer reads them.
func TestBackpressureConnectErrorSkipsFlush(t *testing.T) {
	srv := newBackpressureServer(t)
	srv.OnConnect("/", func(c Conn) error {
		c.Emit("msg")
		return errors.New("refused")
	})
	fc := newStallConn("refused", -1)
	t.Cleanup(func() { _ = fc.Close() })

	recv(t, inBackground(func() { srv.serveConn(fc) }), "serveConn of a refused connection")
	recv(t, fc.closed, "engine.io close of "+fc.id)
}

// TestBackpressureCloseFromOnErrorWhileWriterFails checks that Close called
// from OnError returns while the writer fails on a queued packet: the writer
// must not wait to report that failure to the goroutine running Close.
func TestBackpressureCloseFromOnErrorWhileWriterFails(t *testing.T) {
	srv := newBackpressureServer(t)
	srv.OnError("/", func(c Conn, _ error) {
		if c != nil {
			_ = c.Close()
		}
	})
	fc := newStallConn("failing", 1)
	nc := srv.serve(t, fc)

	nc.Emit("bad", make(chan int)) // both fail to encode
	nc.Emit("bad", make(chan int))
	recv(t, fc.stalled, "the writer to take the first packet")
	close(fc.release)
	recv(t, fc.closed, "engine.io close of "+fc.id)
}

// TestBackpressureCloseDropsLateEmit checks that an Emit made while Close
// waits for the queue is dropped: exactly the packets queued before Close are
// written, and the flush token is neither written nor taken for a packet.
func TestBackpressureCloseDropsLateEmit(t *testing.T) {
	srv := newBackpressureServer(t)
	fc := newStallConn("slow", 1)
	nc := srv.serve(t, fc)

	for i := 0; i < 3; i++ {
		nc.Emit("msg", i)
	}
	recv(t, fc.stalled, "the writer to take the first packet")
	closed := inBackground(func() { _ = nc.Close() })
	queue := nc.(*namespaceConn).writeChan
	require.Eventually(t, func() bool { return len(queue) == 3 }, waitFor, time.Millisecond, "the flush token")
	nc.Emit("late")
	close(fc.release)
	recv(t, closed, "Close")

	require.Len(t, fc.frames, 3, "packets written before Close returned")
	for i := 0; i < 3; i++ {
		require.Equal(t, fmt.Sprintf("2[\"msg\",%d]\n", i), <-fc.frames)
	}
}

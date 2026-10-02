package socketio

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/session"
	"github.com/googollee/go-socket.io/parser"
)

// fakeConn is an engineio.Conn driven by the test. The peer sends frames on
// reads (unbuffered: a send returns once the previous frame is handled); each
// written frame goes to out. While hold is set, NextWriter signals held and
// blocks until a send on release, its close, or Close; after Close, or with
// failWrite, it fails.
type fakeConn struct {
	reads     chan string
	out       chan string
	hold      atomic.Bool
	held      chan struct{}
	release   chan struct{}
	texts     atomic.Int32
	failWrite atomic.Bool
	peerGone  chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeConn(t *testing.T) *fakeConn {
	f := &fakeConn{
		reads:    make(chan string),
		out:      make(chan string, 256),
		held:     make(chan struct{}, 1),
		release:  make(chan struct{}),
		peerGone: make(chan struct{}),
		closed:   make(chan struct{}),
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func (f *fakeConn) NextWriter(ft session.FrameType) (io.WriteCloser, error) {
	if ft == session.TEXT {
		f.texts.Add(1)
	}
	if f.hold.Load() {
		select {
		case f.held <- struct{}{}:
		default:
		}
		select {
		case <-f.release:
		case <-f.closed:
		}
	}
	if isDone(f.closed) || f.failWrite.Load() {
		return nil, io.EOF
	}
	return &frameWriter{out: f.out}, nil
}

func (f *fakeConn) NextReader() (session.FrameType, io.ReadCloser, error) {
	select {
	case frame := <-f.reads:
		return session.TEXT, io.NopCloser(strings.NewReader(frame)), nil
	case <-f.peerGone:
	case <-f.closed:
	}
	return session.TEXT, nil, io.EOF
}

func (f *fakeConn) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func (f *fakeConn) ID() string                { return fmt.Sprintf("%p", f) }
func (f *fakeConn) URL() url.URL              { return url.URL{} }
func (f *fakeConn) LocalAddr() net.Addr       { return nil }
func (f *fakeConn) RemoteAddr() net.Addr      { return nil }
func (f *fakeConn) RemoteHeader() http.Header { return nil }
func (f *fakeConn) SetContext(interface{})    {}
func (f *fakeConn) Context() interface{}      { return nil }

type frameWriter struct {
	bytes.Buffer
	out chan<- string
}

func (w *frameWriter) Close() error {
	w.out <- w.String()
	return nil
}

func inBackground(f func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	return done
}

func drain[T any](ch <-chan T) []T {
	var got []T
	for {
		select {
		case v := <-ch:
			got = append(got, v)
		default:
			return got
		}
	}
}

// ev is the frame that Emit(name, args...) writes for the root namespace.
func ev(name string, args ...interface{}) string {
	b, _ := json.Marshal(append([]interface{}{name}, args...))
	return "2" + string(b) + "\n"
}

type nsErr struct {
	nsp string
	err error
}

type hooks struct {
	connect    func(Conn) error
	disconnect func(Conn)
	onError    func(Conn, error)
	events     map[string]interface{}
}

// peer is a Server's (S) or a Client's (C) connection over a fakeConn. Its
// handlers record each call (nilErrs: OnError with a nil Conn), then run hooks.
type peer struct {
	fc      *fakeConn
	srv     *Server
	cl      *Client
	nc      Conn // the root namespace
	conns   chan Conn
	errs    chan nsErr
	nilErrs chan error
	discs   chan string
}

func newPeer(t *testing.T, side byte, h hooks, nsps ...string) *peer {
	p := &peer{
		fc:      newFakeConn(t),
		conns:   make(chan Conn, 8),
		errs:    make(chan nsErr, 128),
		nilErrs: make(chan error, 8),
		discs:   make(chan string, 8),
	}
	var nhs []*namespaceHandler
	if side == 'S' {
		p.srv = NewServer(nil)
		for _, nsp := range append([]string{"/"}, nsps...) {
			nhs = append(nhs, p.srv.createNamespace(nsp))
		}
	} else {
		cl, err := NewClient("http://127.0.0.1/", nil)
		require.NoError(t, err)
		cl.dial = func(string) (engineio.Conn, error) { return p.fc, nil }
		p.cl = cl
		nhs = append(nhs, cl.createNamespace(cl.namespace))
	}
	for _, nh := range nhs {
		nh.OnConnect(func(c Conn) error {
			p.conns <- c
			if h.connect != nil {
				return h.connect(c)
			}
			return nil
		})
		nh.OnDisconnect(func(c Conn, _ string) {
			p.discs <- c.Namespace()
			if h.disconnect != nil {
				h.disconnect(c)
			}
		})
		nh.OnError(func(c Conn, err error) {
			if c == nil {
				p.nilErrs <- err
				return
			}
			p.errs <- nsErr{c.Namespace(), err}
			if h.onError != nil {
				h.onError(c, err)
			}
		})
		for name, f := range h.events {
			nh.OnEvent(name, f)
		}
	}
	return p
}

// start returns a peer whose root namespace is connected.
func start(t *testing.T, side byte, h hooks, nsps ...string) *peer {
	return newPeer(t, side, h, nsps...).connect(t)
}

func (p *peer) connect(t *testing.T) *peer {
	t.Helper()
	if p.srv != nil {
		p.srv.serveConn(p.fc)
	} else {
		require.NoError(t, p.cl.Connect())
	}
	require.Equal(t, "0", recv(t, p.fc.out, "the CONNECT packet"))
	if p.cl != nil {
		p.send(t, "0")
	}
	p.nc = recv(t, p.conns, "root OnConnect")
	return p
}

// join connects the server namespace nsp and returns its Conn.
func (p *peer) join(t *testing.T, nsp string) Conn {
	t.Helper()
	p.send(t, "0"+nsp)
	recv(t, p.fc.out, "the CONNECT reply for "+nsp)
	return recv(t, p.conns, "OnConnect of "+nsp)
}

func (p *peer) send(t *testing.T, frame string) {
	t.Helper()
	select {
	case p.fc.reads <- frame:
	case <-time.After(waitFor):
		t.Fatalf("timed out sending %q", frame)
	}
}

// Close is the application's Close: Conn.Close on S, Client.Close on C.
func (p *peer) Close() error {
	if p.cl != nil {
		return p.cl.Close()
	}
	return p.nc.Close()
}

func (p *peer) conn() *conn {
	if p.cl != nil {
		return p.cl.conn
	}
	return p.nc.(*namespaceConn).conn
}

// stall holds the writer on a first packet of nc and queues n more behind it.
func (p *peer) stall(t *testing.T, nc Conn, n int) {
	t.Helper()
	p.fc.hold.Store(true)
	nc.Emit("first")
	recv(t, p.fc.held, "the writer to block on the first packet")
	flood(nc, n)
}

// disconnected waits for the close to run its OnDisconnect calls and checks them.
func (p *peer) disconnected(t *testing.T, nsps ...string) {
	t.Helper()
	recv(t, p.conn().done, "the close to run OnDisconnect")
	require.ElementsMatch(t, nsps, drain(p.discs), "OnDisconnect calls")
}

// overflowReported waits for the one ErrWriteBufferFull report, to nsp.
func (p *peer) overflowReported(t *testing.T, nsp string) {
	t.Helper()
	got := recv(t, p.errs, "the overflow report")
	require.Equal(t, nsp, got.nsp)
	require.ErrorIs(t, got.err, ErrWriteBufferFull)
	for _, e := range drain(p.errs) {
		require.NotErrorIs(t, e.err, ErrWriteBufferFull, "a second overflow report")
	}
}

func sides(t *testing.T, list string, f func(t *testing.T, side byte)) {
	for _, side := range []byte(list) {
		t.Run(string(side), func(t *testing.T) { f(t, side) })
	}
}

func flood(c Conn, n int) {
	for i := 0; i < n; i++ {
		c.Emit("x", i)
	}
}

// Covers 1B-T1 (S).
func TestBackpressureStalledMemberDoesNotBlockRoom(t *testing.T) {
	p := start(t, 'S', hooks{connect: func(c Conn) error { c.Join("r"); return nil }})
	healthy := newFakeConn(t)
	p.srv.serveConn(healthy)
	require.Equal(t, "0", recv(t, healthy.out, "the healthy member's CONNECT"))
	recv(t, p.conns, "OnConnect of the healthy member")

	p.stall(t, p.nc, defaultWriteBufferSize)
	for i := 0; i < 2; i++ { // the first broadcast overflows the stalled member
		recv(t, inBackground(func() { p.srv.BroadcastToRoom("/", "r", "msg", i) }), "a broadcast past the stalled member")
		require.Equal(t, ev("msg", i), recv(t, healthy.out, "the broadcast to the healthy member"))
	}
	recv(t, p.fc.closed, "engine.io close of the stalled member")
	p.overflowReported(t, "/")
	p.disconnected(t, "/")
	require.Equal(t, 1, p.srv.RoomLen("/", "r"))
	require.False(t, isDone(healthy.closed), "the healthy member was closed")
}

// Covers 1B-T2 (S, C).
func TestBackpressureCloseDeliversQueue(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		for _, n := range []int{1, defaultWriteBufferSize} {
			p := start(t, side, hooks{})
			p.stall(t, p.nc, n)
			var err error
			recv(t, inBackground(func() { err = p.Close() }), "Close with the writer blocked")
			require.NoError(t, err)
			require.False(t, isDone(p.fc.closed), "closed before the queue was written")

			close(p.fc.release)
			require.Equal(t, ev("first"), recv(t, p.fc.out, "the first packet"))
			for i := 0; i < n; i++ {
				require.Equal(t, ev("x", i), recv(t, p.fc.out, "a queued packet"))
			}
			recv(t, p.fc.closed, "engine.io close after the drain")
			p.disconnected(t, "/")
			require.Empty(t, drain(p.fc.out))
		}
	})
}

// Covers 1B-T3 (S, C).
func TestBackpressureCloseWritesOnDisconnectEmits(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		var p *peer
		p = start(t, side, hooks{disconnect: func(c Conn) {
			c.Emit("a")
			require.Equal(t, ev("a"), recv(t, p.fc.out, "the first OnDisconnect Emit"))
			c.Emit("b") // the writer has emptied the queue
		}})
		require.NoError(t, p.Close())
		require.Equal(t, ev("b"), recv(t, p.fc.out, "the second OnDisconnect Emit"))
		recv(t, p.fc.closed, "engine.io close after the drain")
		p.disconnected(t, "/")
		require.Empty(t, drain(p.fc.out))
	})
}

// Covers 1B-T4 (S, C).
func TestBackpressureCloseDropsLateAndOverflowingEmits(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		p := start(t, side, hooks{disconnect: func(c Conn) {
			for i := 0; i <= defaultWriteBufferSize; i++ { // the last one finds the queue full
				c.Emit("q", i)
			}
		}})
		p.stall(t, p.nc, 0)
		require.NoError(t, p.Close())
		p.fc.release <- struct{}{} // writes "first", then blocks on the next packet
		require.Equal(t, ev("first"), recv(t, p.fc.out, "the first packet"))
		recv(t, p.fc.held, "the writer to block on the second packet")
		p.nc.Emit("after the seal") // the queue has room
		close(p.fc.release)
		for i := 0; i < defaultWriteBufferSize; i++ {
			require.Equal(t, ev("q", i), recv(t, p.fc.out, "an OnDisconnect Emit"))
		}
		recv(t, p.fc.closed, "engine.io close after the drain")
		p.disconnected(t, "/")
		require.Empty(t, drain(p.fc.out))
		require.Empty(t, drain(p.errs))
	})
}

// Covers 1B-T5 (S, C).
func TestBackpressureCloseStopsDispatch(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		var ran atomic.Int32
		p := start(t, side, hooks{events: map[string]interface{}{"ev": func(Conn) { ran.Add(1) }}}, "/a")
		p.stall(t, p.nc, 0) // keeps the drain running
		require.NoError(t, p.Close())

		for _, frame := range []string{`2["ev"]`, map[byte]string{'S': "0/a", 'C': "0"}[side], `2["ev"]`} {
			p.send(t, frame)
		}
		close(p.fc.release)
		recv(t, p.fc.closed, "engine.io close after the drain")
		p.disconnected(t, "/")
		require.Zero(t, ran.Load(), "an event handler ran")
		require.Empty(t, drain(p.conns), "OnConnect ran")
	})
}

// Covers 1B-T6 (S, C).
func TestBackpressureDrainDeadline(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		p := start(t, side, hooks{})
		p.conn().drainTimeout = 50 * time.Millisecond
		p.stall(t, p.nc, 1)
		begin := time.Now()
		require.NoError(t, p.Close())
		recv(t, p.fc.closed, "engine.io close at the drain deadline")
		require.Less(t, time.Since(begin), 50*time.Millisecond+time.Second)
		p.disconnected(t, "/")
		require.Empty(t, drain(p.errs), "OnError was called")
	})
}

// Each trigger closes the connection, writer blocked, at once; OnDisconnect
// overflows the queue without a report.
// Covers 1B-T7 (S, C).
// Covers 1B-T8 (S, C).
// Covers 1B-T9 (S, C).
// Covers 1B-T12 (S, C).
// Covers 1B-T13 (S, C).
func TestBackpressureLibraryCloseDiscards(t *testing.T) {
	triggers := []struct {
		name string
		run  func(t *testing.T, p *peer)
	}{
		{"read error", func(t *testing.T, p *peer) { p.send(t, "x") }},
		{"engine.io close", func(_ *testing.T, p *peer) { close(p.fc.peerGone) }},
		{"dispatch error", func(t *testing.T, p *peer) { p.send(t, `2["boom"]`) }},
		{"after a draining Close", func(t *testing.T, p *peer) {
			require.NoError(t, p.Close())
			require.False(t, isDone(p.fc.closed), "the drain ended early")
			close(p.fc.peerGone)
		}},
	}
	sides(t, "SC", func(t *testing.T, side byte) {
		for _, tr := range triggers {
			t.Run(tr.name, func(t *testing.T) {
				p := start(t, side, hooks{
					disconnect: func(c Conn) { flood(c, defaultWriteBufferSize+1) },
					events:     map[string]interface{}{"boom": func(Conn) { panic("boom") }},
				})
				p.stall(t, p.nc, 3)
				tr.run(t, p)
				recv(t, p.fc.closed, "engine.io close with the writer blocked")
				p.disconnected(t, "/")
				require.Empty(t, drain(p.fc.out), "a packet was written")
				for _, e := range drain(p.errs) {
					require.NotErrorIs(t, e.err, ErrWriteBufferFull)
				}
			})
		}
	})
}

// Covers 1B-T10 (S, C).
func TestBackpressureEncodeErrorClosesAfterReport(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		var p *peer
		closedAtReport := make(chan bool, 4)
		p = start(t, side, hooks{onError: func(Conn, error) { closedAtReport <- isDone(p.fc.closed) }}, "/a")
		nc, nsps := p.nc, []string{"/"}
		if side == 'S' {
			nc, nsps = p.join(t, "/a"), []string{"/", "/a"}
		}
		p.fc.hold.Store(true)
		nc.Emit("bad", make(chan int)) // json cannot encode a channel
		recv(t, p.fc.held, "the writer to block on the bad packet")
		flood(nc, 3)
		close(p.fc.release)

		got := recv(t, p.errs, "the encode error report")
		require.Equal(t, nc.Namespace(), got.nsp)
		require.False(t, recv(t, closedAtReport, "OnError"), "closed before the report")
		recv(t, p.fc.closed, "engine.io close after the report")
		p.disconnected(t, nsps...)
		require.Empty(t, drain(p.errs), "a second report")
		for _, frame := range drain(p.fc.out) {
			require.NotContains(t, frame, `"x"`, "a packet queued after the bad one was written")
		}
	})
}

// Covers 1B-T11 (S, C).
func TestBackpressureConnectFailureDiscards(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		p := newPeer(t, side, hooks{connect: func(c Conn) error {
			c.Emit("q")
			return errors.New("refused")
		}})
		if side == 'S' {
			p.connect(t)
		} else {
			p.fc.failWrite.Store(true) // the CONNECT packet fails to encode
			require.Error(t, p.cl.Connect())
		}
		recv(t, p.fc.closed, "engine.io close of the failed connection")
		p.disconnected(t, "/")
		require.Len(t, drain(p.nilErrs), 1, "connect error reports")
		require.Empty(t, drain(p.fc.out), "a packet was written")
	})
}

// Covers 1B-T14 (S, C).
func TestBackpressureNamespaceDisconnectRacesClose(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		target, frame, others := "/", "1", []string(nil)
		if side == 'S' {
			target, frame, others = "/a", "1/a", []string{"/"}
		}
		hold := make(chan struct{})
		p := start(t, side, hooks{disconnect: func(c Conn) {
			if c.Namespace() == target {
				<-hold
			}
		}}, "/a")
		if side == 'S' {
			p.join(t, "/a")
		}
		p.send(t, frame)
		require.Equal(t, target, recv(t, p.discs, "the held OnDisconnect"))
		recv(t, inBackground(func() { _ = p.Close() }), "Close while OnDisconnect is held")
		close(hold)
		p.disconnected(t, others...)
	})
}

// Covers 1B-T15 (S, C).
// Covers 1B-T16 (S, C).
func TestBackpressureOverflow(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		p := start(t, side, hooks{}, "/a")
		nc, nsps := p.nc, []string{"/"}
		if side == 'S' {
			nc, nsps = p.join(t, "/a"), []string{"/", "/a"}
		}
		p.fc.hold.Store(true)
		nc.Emit("bin", &parser.Buffer{Data: []byte{1}})
		recv(t, p.fc.held, "the writer to block on the binary packet")
		started := p.fc.texts.Load()

		flood(nc, defaultWriteBufferSize+3)
		recv(t, p.fc.closed, "engine.io close on overflow")
		p.overflowReported(t, nc.Namespace())
		p.disconnected(t, nsps...)
		require.Never(t, func() bool { return p.fc.texts.Load() != started }, 100*time.Millisecond, time.Millisecond, "the writer started a packet")
	})
}

// Covers 1B-T17 (S).
// Covers 1B-T18 (S).
func TestBackpressureOverflowInOnConnect(t *testing.T) {
	for _, connectErr := range []error{nil, errors.New("refused")} {
		p := start(t, 'S', hooks{connect: func(c Conn) error {
			flood(c, defaultWriteBufferSize+1)
			return connectErr
		}})
		require.True(t, isDone(p.conn().done) && len(p.errs) == 1, "serveConn did not close the connection itself")
		recv(t, p.fc.closed, "engine.io close of the failed connection")
		p.disconnected(t, "/")
		p.overflowReported(t, "/")
		if connectErr != nil {
			require.Equal(t, []error{connectErr}, drain(p.nilErrs))
		}
		require.Empty(t, drain(p.nilErrs))
		require.Empty(t, drain(p.fc.out), "a packet was written")
	}
}

// Covers 1B-T19 (S).
// Covers 1B-T20 (S).
func TestBackpressureOverflowInBroadcast(t *testing.T) {
	for _, overflow := range []func(*Server){
		func(s *Server) { s.BroadcastToRoom("/", "r", "x") },
		func(s *Server) { s.ForEach("/", "r", func(c Conn) { flood(c, 2) }) },
	} {
		p := start(t, 'S', hooks{connect: func(c Conn) error { c.Join("r"); return nil }})
		p.stall(t, p.nc, defaultWriteBufferSize)
		recv(t, inBackground(func() { overflow(p.srv) }), "the overflowing broadcast")
		recv(t, p.fc.closed, "engine.io close on overflow")
		p.overflowReported(t, "/")
		p.disconnected(t, "/")
		require.Zero(t, p.srv.RoomLen("/", "r"))
	}
}

// Covers 1B-T21 (S, C).
func TestBackpressureOverflowInOnError(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		p := start(t, side, hooks{
			events: map[string]interface{}{"boom": func(Conn) { panic("boom") }},
			onError: func(c Conn, err error) {
				if !errors.Is(err, ErrWriteBufferFull) {
					flood(c, defaultWriteBufferSize+1)
				}
			},
		})
		p.stall(t, p.nc, 0)
		p.send(t, `2["boom"]`)
		require.NotErrorIs(t, recv(t, p.errs, "the dispatch error").err, ErrWriteBufferFull)
		recv(t, p.fc.closed, "engine.io close on overflow")
		p.overflowReported(t, "/")
		p.disconnected(t, "/")
	})
}

// Covers 1B-T22 (S, C).
// Covers 1B-T23 (S, C).
// Covers 1B-T24 (S, C).
func TestBackpressureCloseFromHandlers(t *testing.T) {
	closeConn := func(c Conn) { _ = c.Close() }
	cases := []struct {
		name string
		h    hooks
		run  func(t *testing.T, p *peer)
	}{
		{"OnError", hooks{
			events:  map[string]interface{}{"boom": func(Conn) { panic("boom") }},
			onError: func(c Conn, _ error) { closeConn(c) },
		}, func(t *testing.T, p *peer) { p.send(t, `2["boom"]`) }},
		{"OnDisconnect", hooks{disconnect: closeConn}, func(t *testing.T, p *peer) {
			recv(t, inBackground(func() { _ = p.Close() }), "Close")
		}},
		{"twice concurrently", hooks{}, func(t *testing.T, p *peer) {
			var errs [2]error
			a := inBackground(func() { errs[0] = p.Close() })
			b := inBackground(func() { errs[1] = p.Close() })
			recv(t, a, "the first Close")
			recv(t, b, "the second Close")
			require.Equal(t, [2]error{}, errs)
		}},
	}
	sides(t, "SC", func(t *testing.T, side byte) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				p := start(t, side, tc.h)
				tc.run(t, p)
				recv(t, p.fc.closed, "engine.io close")
				p.disconnected(t, "/")
			})
		}
	})
}

// Covers 1B-T25 (S, C).
func TestBackpressureNamespaceDisconnectKeepsSession(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		p := start(t, side, hooks{events: map[string]interface{}{"echo": func(Conn) string { return "ok" }}}, "/a")
		target, frame := "/", "1"
		if side == 'S' {
			target, frame = "/a", "1/a"
			p.join(t, "/a")
		}
		p.stall(t, p.nc, 0)
		p.send(t, frame)
		require.Equal(t, target, recv(t, p.discs, "OnDisconnect of "+target))
		close(p.fc.release)
		require.Equal(t, ev("first"), recv(t, p.fc.out, "the packet queued before the DISCONNECT"))
		if side == 'S' {
			p.send(t, `21["echo"]`)
			require.Equal(t, `31["ok"]`+"\n", recv(t, p.fc.out, "the root namespace's ACK"))
		}
		p.send(t, `2["echo"]`)
		require.False(t, isDone(p.fc.closed), "the session was closed")
		require.Empty(t, drain(p.discs))
	})
}

// Covers 1B-T26 (S).
func TestBackpressureCloseFromOnConnect(t *testing.T) {
	p := start(t, 'S', hooks{
		connect: func(c Conn) error {
			c.Emit("a")
			require.NoError(t, c.Close())
			c.Emit("after the seal")
			return nil
		},
		disconnect: func(c Conn) { c.Emit("b") },
	})
	require.Equal(t, ev("a"), recv(t, p.fc.out, "the OnConnect Emit"))
	require.Equal(t, ev("b"), recv(t, p.fc.out, "the OnDisconnect Emit"))
	recv(t, p.fc.closed, "engine.io close after the drain")
	p.disconnected(t, "/")
	require.Empty(t, drain(p.fc.out))
	require.Empty(t, drain(p.nilErrs), "reported as a connect failure")
}

// Covers 1B-T27 (S).
func TestBackpressureAckOverflow(t *testing.T) {
	p := start(t, 'S', hooks{events: map[string]interface{}{"echo": func(Conn) string { return "ok" }}})
	p.stall(t, p.nc, defaultWriteBufferSize)
	p.send(t, `21["echo"]`)
	recv(t, p.fc.closed, "engine.io close on overflow")
	p.overflowReported(t, "/")
	p.disconnected(t, "/") // the read goroutine ran it, so it did not block
}

func isDone(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

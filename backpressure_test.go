package socketio

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// fakeConn is an engineio.Conn driven by the test. The peer sends frames on reads (unbuffered: a
// send returns once the previous frame is handled); each written frame goes to out. While hold is
// set, NextWriter signals held and blocks until a send on release, its close, or Close; after
// Close, or with failWrite, it fails.
type fakeConn struct {
	engineio.Conn                   // methods the library does not call
	reads, out                      chan string
	hold, failWrite                 atomic.Bool
	held, release, peerGone, closed chan struct{}
	texts                           atomic.Int32
	closeOnce                       sync.Once
}

func newFakeConn(t *testing.T) *fakeConn {
	f := &fakeConn{reads: make(chan string), out: make(chan string, 256), held: make(chan struct{}, 1)}
	f.release, f.peerGone, f.closed = make(chan struct{}), make(chan struct{}), make(chan struct{})
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

func (f *fakeConn) Close() error { f.closeOnce.Do(func() { close(f.closed) }); return nil }

func (f *fakeConn) ID() string           { return fmt.Sprintf("%p", f) }
func (f *fakeConn) Context() interface{} { return nil }

type frameWriter struct {
	bytes.Buffer
	out chan<- string
}

func (w *frameWriter) Close() error { w.out <- w.String(); return nil }

func inBackground(f func()) <-chan struct{} {
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	return done
}

func drain[T any](ch <-chan T) (got []T) {
	for len(ch) > 0 {
		got = append(got, <-ch)
	}
	return got
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
	nc      Conn                         // the root namespace
	emit    func(string, ...interface{}) // the application's root Emit: Client.Emit on C
	conns   chan Conn
	errs    chan nsErr
	nilErrs chan error
	discs   chan string
}

func newPeer(t *testing.T, side byte, h hooks, nsps ...string) *peer {
	p := &peer{fc: newFakeConn(t), conns: make(chan Conn, 8), errs: make(chan nsErr, 128)}
	p.nilErrs, p.discs = make(chan error, 8), make(chan string, 8)
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
			} else {
				p.errs <- nsErr{c.Namespace(), err}
			}
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
	if p.emit = p.nc.Emit; p.cl != nil {
		p.emit = p.cl.Emit
	}
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

// sub joins /a on S and returns its Conn, its DISCONNECT frame and the other
// connected namespaces; on C it returns those of the root namespace.
func (p *peer) sub(t *testing.T) (Conn, string, []string) {
	t.Helper()
	if p.srv == nil {
		return p.nc, "1", nil
	}
	return p.join(t, "/a"), "1/a", []string{"/"}
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
			recv(t, inBackground(func() { _ = p.Close() }), "Close with the writer blocked")
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
		p = start(t, side, hooks{disconnect: func(Conn) {
			p.emit("a")
			require.Equal(t, ev("a"), recv(t, p.fc.out, "the first OnDisconnect Emit"))
			p.emit("b") // the writer has emptied the queue
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
		p := start(t, side, hooks{disconnect: func(c Conn) { flood(c, defaultWriteBufferSize+1) }}) // the last finds the queue full
		p.stall(t, p.nc, 0)
		require.NoError(t, p.Close())
		p.fc.release <- struct{}{} // writes "first", then blocks on the next packet
		require.Equal(t, ev("first"), recv(t, p.fc.out, "the first packet"))
		recv(t, p.fc.held, "the writer to block on the second packet")
		p.emit("after the seal") // the queue has room
		close(p.fc.release)
		for i := 0; i < defaultWriteBufferSize; i++ {
			require.Equal(t, ev("x", i), recv(t, p.fc.out, "an OnDisconnect Emit"))
		}
		recv(t, p.fc.closed, "engine.io close after the drain")
		p.disconnected(t, "/")
		require.Empty(t, drain(p.fc.out))
		require.Empty(t, drain(p.errs))
	})
}

// A CONNECT to a namespace without a handler does not end the drain either.
// Covers 1B-T5 (S, C).
func TestBackpressureCloseStopsDispatch(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		var ran atomic.Int32
		p := start(t, side, hooks{events: map[string]interface{}{"ev": func(Conn) { ran.Add(1) }}}, "/a")
		p.stall(t, p.nc, 0) // keeps the drain running
		require.NoError(t, p.Close())

		for _, frame := range []string{`2["ev"]`, map[byte]string{'S': "0/a", 'C': "0"}[side], "0/none", `2["ev"]`} {
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
// overflows the queue without a report. A read or write failure ends a drain.
// Covers 1B-T7 (S, C).
// Covers 1B-T8 (S, C).
// Covers 1B-T9 (S, C).
// Covers 1B-T12 (S, C).
// Covers 1B-T13 (S, C).
func TestBackpressureLibraryCloseDiscards(t *testing.T) {
	triggers := []struct {
		name    string
		reports int // to root OnError
		run     func(t *testing.T, p *peer)
	}{
		{"read error", 1, func(t *testing.T, p *peer) { p.send(t, "x") }},
		{"engine.io close", 1, func(_ *testing.T, p *peer) { close(p.fc.peerGone) }},
		{"dispatch error", 1, func(t *testing.T, p *peer) { p.send(t, `2["boom"]`) }},
		{"read failure during a draining Close", 0, func(t *testing.T, p *peer) {
			require.NoError(t, p.Close())
			require.False(t, isDone(p.fc.closed), "the drain ended early")
			close(p.fc.peerGone)
		}},
		{"write failure during a draining Close", 0, func(t *testing.T, p *peer) {
			require.NoError(t, p.Close())
			p.fc.failWrite.Store(true)
			p.fc.release <- struct{}{}
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
				errs := drain(p.errs)
				require.Len(t, errs, tr.reports, "reports")
				for _, e := range errs {
					require.True(t, e.nsp == "/" && !errors.Is(e.err, ErrWriteBufferFull), "report %v", e)
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
		nc, _, others := p.sub(t)
		p.fc.hold.Store(true)
		nc.Emit("bad", make(chan int)) // json cannot encode a channel
		recv(t, p.fc.held, "the writer to block on the bad packet")
		flood(nc, 3)
		close(p.fc.release)

		got := recv(t, p.errs, "the encode error report")
		require.Equal(t, nc.Namespace(), got.nsp)
		require.False(t, recv(t, closedAtReport, "OnError"), "closed before the report")
		recv(t, p.fc.closed, "engine.io close after the report")
		p.disconnected(t, append(others, nc.Namespace())...)
		require.Empty(t, drain(p.errs), "a second report")
		for _, frame := range drain(p.fc.out) {
			require.NotContains(t, frame, `"x"`, "a packet queued after the bad one was written")
		}
	})
}

// Covers 1B-T11 (S, C).
func TestBackpressureConnectFailureDiscards(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		var p *peer
		reportsAtDisconnect := make(chan int, 1)
		p = newPeer(t, side, hooks{
			connect:    func(c Conn) error { c.Emit("q"); return errors.New("refused") },
			disconnect: func(Conn) { reportsAtDisconnect <- len(p.nilErrs) },
		})
		if side == 'S' {
			p.connect(t)
		} else {
			p.fc.failWrite.Store(true) // the CONNECT packet fails to encode
			require.Error(t, p.cl.Connect())
		}
		recv(t, p.fc.closed, "engine.io close of the failed connection")
		p.disconnected(t, "/")
		require.Equal(t, 1, recv(t, reportsAtDisconnect, "OnDisconnect"), "reports before OnDisconnect")
		require.Len(t, drain(p.nilErrs), 1, "connect error reports")
		require.Empty(t, drain(p.fc.out), "a packet was written")
	})
}

// Covers 1B-T14 (S, C).
func TestBackpressureNamespaceDisconnectRacesClose(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		var target string
		hold := make(chan struct{})
		p := start(t, side, hooks{disconnect: func(c Conn) {
			if c.Namespace() == target {
				<-hold
			}
		}}, "/a")
		nc, frame, others := p.sub(t)
		target = nc.Namespace()
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
		nc, _, others := p.sub(t)
		p.fc.hold.Store(true)
		nc.Emit("bin", &parser.Buffer{Data: []byte{1}})
		recv(t, p.fc.held, "the writer to block on the binary packet")
		started := p.fc.texts.Load()

		flood(nc, defaultWriteBufferSize+3)
		recv(t, p.fc.closed, "engine.io close on overflow")
		p.overflowReported(t, nc.Namespace())
		p.disconnected(t, append(others, nc.Namespace())...)
		require.Never(t, func() bool { return p.fc.texts.Load() != started }, 100*time.Millisecond, time.Millisecond, "the writer started a packet")
	})
}

// An overflow, or an encode error, of a packet whose namespace the peer
// disconnected is reported once, to that namespace's OnError.
func TestBackpressureReportForDisconnectedNamespace(t *testing.T) {
	sides(t, "SC", func(t *testing.T, side byte) {
		for _, n := range []int{defaultWriteBufferSize + 1, 0} {
			p := start(t, side, hooks{}, "/a")
			nc, frame, others := p.sub(t)
			p.send(t, frame)
			require.Equal(t, nc.Namespace(), recv(t, p.discs, "OnDisconnect on the DISCONNECT"))
			if p.stall(t, nc, n); n == 0 {
				nc.Emit("bad", make(chan int)) // json cannot encode a channel
				close(p.fc.release)
			}
			got := recv(t, p.errs, "the report")
			require.Equal(t, nc.Namespace(), got.nsp)
			require.Equal(t, n > 0, errors.Is(got.err, ErrWriteBufferFull), "an overflow report")
			p.disconnected(t, others...)
			require.Empty(t, drain(p.errs), "a second report")
		}
	})
}

// Covers 1B-T17 (S).
// Covers 1B-T18 (S).
func TestBackpressureOverflowInOnConnect(t *testing.T) {
	for _, connectErr := range []error{nil, errors.New("refused"), ErrWriteBufferFull} {
		var cc *conn
		var late []bool // per report: made after the discard, which precedes OnDisconnect
		p := newPeer(t, 'S', hooks{
			connect: func(c Conn) error {
				cc = c.(*namespaceConn).conn
				flood(c, defaultWriteBufferSize+1)
				return connectErr
			},
			onError: func(Conn, error) { late = append(late, isDone(cc.discard)) },
		})
		p.connect(t)
		require.True(t, isDone(p.conn().done), "serveConn did not close the connection itself")
		recv(t, p.fc.closed, "engine.io close of the failed connection")
		p.disconnected(t, "/")
		want := []error{ErrWriteBufferFull}
		if connectErr != nil {
			want = []error{connectErr, ErrWriteBufferFull}
		}
		require.Equal(t, want, drain(p.nilErrs), "connect-failure reports, in order, with a nil Conn")
		require.Equal(t, make([]bool, len(want)), late, "a report came after the close's effects")
		require.Empty(t, drain(p.errs))
		require.Empty(t, drain(p.fc.out), "a packet was written")
	}
}

// The close of a failed connect starts before its report, so an Emit made from
// another goroutine during root OnError is dropped and cannot overflow.
func TestBackpressureConnectFailureDropsConcurrentEmit(t *testing.T) {
	late, refused := make(chan struct{}, 2), errors.New("refused")
	var emitted <-chan struct{}
	p := newPeer(t, 'S', hooks{
		connect: func(c Conn) error {
			flood(c, defaultWriteBufferSize) // the queue is full
			emitted = inBackground(func() { <-late; c.Emit("late") })
			return refused
		},
		onError: func(Conn, error) { late <- struct{}{}; time.Sleep(20 * time.Millisecond) },
	}).connect(t)
	recv(t, emitted, "the Emit during root OnError")
	p.disconnected(t, "/")
	require.Equal(t, []error{refused}, drain(p.nilErrs), "connect-failure reports")
	require.Empty(t, drain(p.errs))
	require.Empty(t, drain(p.fc.out), "a packet was written")
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
			events:  map[string]interface{}{"boom": func(Conn) { panic("boom") }},
			onError: func(c Conn, _ error) { flood(c, defaultWriteBufferSize+1) }, // dropped once closing
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
	cases := []struct {
		name string
		h    hooks
		run  func(t *testing.T, p *peer)
	}{
		{"OnError", hooks{
			events:  map[string]interface{}{"boom": func(Conn) { panic("boom") }},
			onError: func(c Conn, _ error) { _ = c.Close() },
		}, func(t *testing.T, p *peer) { p.send(t, `2["boom"]`) }},
		{"OnDisconnect", hooks{disconnect: func(c Conn) { _ = c.Close() }}, func(t *testing.T, p *peer) {
			recv(t, inBackground(func() { _ = p.Close() }), "Close")
		}},
		{"twice concurrently", hooks{}, func(t *testing.T, p *peer) {
			var errs [2]error
			a, b := inBackground(func() { errs[0] = p.Close() }), inBackground(func() { errs[1] = p.Close() })
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
		nc, frame, _ := p.sub(t)
		p.stall(t, p.nc, 0)
		p.send(t, frame)
		require.Equal(t, nc.Namespace(), recv(t, p.discs, "OnDisconnect of "+nc.Namespace()))
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

// Covers 1B-T27 (S, C).
func TestBackpressureLibraryPacketOverflow(t *testing.T) {
	for _, tc := range []struct {
		side  byte
		frame string
		nsps  []string // the last one overflows
	}{{'S', `21["echo"]`, []string{"/"}}, {'C', `21["echo"]`, []string{"/"}}, {'S', "0/a", []string{"/", "/a"}}} {
		p := start(t, tc.side, hooks{events: map[string]interface{}{"echo": func(Conn) string { return "ok" }}}, "/a")
		p.stall(t, p.nc, defaultWriteBufferSize)
		p.send(t, tc.frame) // the ACK or CONNECT reply finds the queue full
		recv(t, p.fc.closed, "engine.io close on overflow")
		p.overflowReported(t, tc.nsps[len(tc.nsps)-1])
		p.disconnected(t, tc.nsps...) // the read goroutine ran them, so it did not block
	}
}

// Covers 1B-T9 (S).
func TestBackpressureArgDecodeErrorReport(t *testing.T) {
	p := start(t, 'S', hooks{events: map[string]interface{}{"num": func(Conn, int) {}}}, "/a")
	p.join(t, "/a")
	p.send(t, `2/a,["num","x"]`)
	require.Equal(t, "/a", recv(t, p.errs, "the report").nsp)
	p.disconnected(t, "/", "/a")
	require.Empty(t, drain(p.errs), "a second report")
}

// emitConn is a Conn for broadcast tests: ID and Emit only. Emit calls onEmit.
type emitConn struct {
	Conn
	id     string
	onEmit func(event string)
}

func (c *emitConn) ID() string { return c.id }

func (c *emitConn) Emit(event string, _ ...interface{}) { c.onEmit(event) }

// A recipient whose Emit blocks does not stop others from joining or leaving
// rooms; one whose Emit leaves all rooms does not deadlock Send or SendAll.
func TestBroadcastDoesNotHoldLockWhileEmitting(t *testing.T) {
	bc := newBroadcast()
	entered, release := make(chan struct{}), make(chan struct{})
	slow := &emitConn{id: "slow", onEmit: func(string) {
		entered <- struct{}{}
		<-release
	}}
	other := &emitConn{id: "other", onEmit: func(string) {}}
	bc.Join("r", slow)
	sent := inBackground(func() { bc.Send("r", "msg") })
	recv(t, entered, "Emit on the slow member")
	recv(t, inBackground(func() {
		bc.Join("r", other)
		bc.LeaveAll(other)
	}), "Join and LeaveAll during a blocked Emit")
	close(release)
	recv(t, sent, "Send")
	bc.LeaveAll(slow)
	var events []string
	leaver := &emitConn{id: "leaver"}
	leaver.onEmit = func(event string) {
		events = append(events, event)
		bc.LeaveAll(leaver)
	}
	for _, send := range []func(){
		func() { bc.Send("a", "send") },
		func() { bc.SendAll("send-all") },
	} {
		bc.Join("a", leaver)
		recv(t, inBackground(send), "a broadcast whose recipient leaves")
	}
	require.Equal(t, []string{"send", "send-all"}, events)
}

// The ForEach callback may change rooms; it visits the members present when
// ForEach started.
func TestBroadcastForEachCallbackChangesRooms(t *testing.T) {
	bc := newBroadcast()
	bc.Join("r", &emitConn{id: "a"})
	bc.Join("r", &emitConn{id: "b"})
	var visited []string
	recv(t, inBackground(func() {
		bc.ForEach("r", func(c Conn) {
			visited = append(visited, c.ID())
			bc.Leave("r", c)
			bc.Join("moved", c)
		})
	}), "ForEach whose callback changes rooms")
	require.ElementsMatch(t, []string{"a", "b"}, visited)
	require.Equal(t, 0, bc.Len("r"))
	require.Equal(t, 2, bc.Len("moved"))
}

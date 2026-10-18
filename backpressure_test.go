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

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/internal/namespace"
	"github.com/sshaplygin/go-socket.io/parser"
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

func (f *fakeConn) NextWriter(ft eioparser.FrameType) (io.WriteCloser, error) {
	if ft == eioparser.String {
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

func (f *fakeConn) NextReader() (eioparser.FrameType, io.ReadCloser, error) {
	select {
	case frame := <-f.reads:
		return eioparser.String, io.NopCloser(strings.NewReader(frame)), nil
	case <-f.peerGone:
	case <-f.closed:
	}
	return eioparser.String, nil, io.EOF
}

func (f *fakeConn) Close() error { f.closeOnce.Do(func() { close(f.closed) }); return nil }

// closeAtEnd closes f at cleanup and waits for the serving conn c, so that its "socketio:
// disconnect" records are logged before the test returns, not into the next test's recorder.
func closeAtEnd(t *testing.T, f *fakeConn, completion <-chan struct{}) {
	t.Cleanup(func() {
		_ = f.Close()
		select {
		case <-completion:
		case <-time.After(waitFor):
		}
	})
}

func (f *fakeConn) ID() string { return fmt.Sprintf("%p", f) }

func (f *fakeConn) ReadyState() engineio.ReadyState { return engineio.StateOpen }
func (f *fakeConn) Transport() string               { return "fake" }
func (f *fakeConn) Upgraded() bool                  { return false }
func (f *fakeConn) Context() interface{}            { return nil }

// gatedFinishConn lets the writer fail while finishRead is still obtaining
// the final engine close cause, before Close returns to the reader.
type gatedFinishConn struct {
	*fakeConn
	entered, release, writeFailed chan struct{}
	closeEntered, writerFailed    sync.Once
}

func (f *gatedFinishConn) Close() error {
	_ = f.fakeConn.Close()
	f.closeEntered.Do(func() { close(f.entered) })
	<-f.release
	return nil
}

func (f *gatedFinishConn) NextWriter(ft eioparser.FrameType) (io.WriteCloser, error) {
	w, err := f.fakeConn.NextWriter(ft)
	if err != nil {
		f.writerFailed.Do(func() { close(f.writeFailed) })
	}
	return w, err
}

func TestBackpressurePeerCloseReportsOnce(t *testing.T) {
	p := newPeer(t, 'S', hooks{})
	f := &gatedFinishConn{fakeConn: p.fc, entered: make(chan struct{}), release: make(chan struct{}), writeFailed: make(chan struct{})}
	release := sync.OnceFunc(func() { close(f.release) })
	defer release()
	p.srv.serveConn(f)
	p.nc = recv(t, p.conns, "root OnConnect")
	require.Equal(t, "0", recv(t, p.fc.out, "root CONNECT"))
	closeAtEnd(t, p.fc, p.completion())
	p.stall(t, p.nc, 0)
	close(p.fc.peerGone)
	require.ErrorIs(t, recv(t, p.errs, "read error report").err, io.EOF)
	recv(t, f.entered, "engine Close entered")
	recv(t, f.writeFailed, "writer observed engine close")
	require.False(t, p.nc.Connected(), "library close removes namespace membership")
	require.Empty(t, p.errs, "writer failure during read finalization must not report again")
	release()
	p.disconnected(t, "/")
}

// Covers S18
func TestServerEngine(t *testing.T) {
	s := NewServer()
	t.Cleanup(func() { _ = s.Close() })
	require.NotNil(t, s.Engine())
	require.Same(t, s.engine, s.Engine())
	require.Same(t, s.Engine(), s.Engine())
	require.Equal(t, s.Count(), s.Engine().Count())
}

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
	setup      func(*Server) // runs on S before the handlers are registered
	connect    func(Conn) error
	disconnect func(Conn)
	onError    func(Conn, error)
	events     map[string]interface{}
}

// peer is a Server's (S) or a Client's (C) connection over a fakeConn. Its
// handlers record each call (nilErrs: OnError with a nil Conn), then run hooks.
type peer struct {
	fc           *fakeConn
	srv          *Server
	rootRejected error
	nc           Conn                         // the root namespace
	emit         func(string, ...interface{}) // the application's root Emit: Client.Emit on C
	conns        chan Conn
	errs         chan nsErr
	nilErrs      chan error
	discs        chan string
}

func newPeer(t *testing.T, side byte, h hooks, nsps ...string) *peer {
	p := &peer{fc: newFakeConn(t), conns: make(chan Conn, 8), errs: make(chan nsErr, 128)}
	p.nilErrs, p.discs = make(chan error, 8), make(chan string, 8)
	var nhs []*namespace.Handler

	p.srv = NewServer()
	if h.setup != nil {
		h.setup(p.srv)
	}
	for _, nsp := range append([]string{"/"}, nsps...) {
		nhs = append(nhs, p.srv.createNamespace(nsp))
	}

	for _, nh := range nhs {
		nh.OnConnect(func(c Conn) error {
			p.conns <- c
			if h.connect != nil {
				err := h.connect(c)
				if c.Namespace() == "/" {
					p.rootRejected = err
				}
				return err
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

// connect returns a connected server peer.
func (p *peer) connect(t *testing.T) *peer {
	t.Helper()
	p.srv.serveConn(p.fc)
	p.nc = recv(t, p.conns, "root OnConnect")
	if !isDone(p.completion()) || len(p.fc.out) > 0 {
		if p.rootRejected != nil {
			data, _ := json.Marshal(p.rootRejected.Error())
			require.Equal(t, "4"+string(data)+"\n", recv(t, p.fc.out, "root rejection ERROR"))
		} else {
			require.Equal(t, "0", recv(t, p.fc.out, "the CONNECT packet"))
		}
	}
	closeAtEnd(t, p.fc, p.completion())
	p.emit = p.nc.Emit
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

// Close closes the server connection.
func (p *peer) Close() error { return p.nc.Close() }

func (p *peer) completion() <-chan struct{} { return serverCompletion(p.srv, p.fc.ID()) }

func serverCompletion(s *Server, id string) <-chan struct{} {
	done := make(chan struct{})
	s.activeMu.Lock()
	for c := range s.active {
		if c.Conn.ID() == id {
			s.activeMu.Unlock()
			go func() { c.Wait(); close(done) }()
			return done
		}
	}
	s.activeMu.Unlock()
	close(done)
	return done
}

func isDone(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// sub joins /a and returns its connection, DISCONNECT frame and remaining namespaces.
func (p *peer) sub(t *testing.T) (Conn, string, []string) {
	t.Helper()
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
	recv(t, p.completion(), "the close to run OnDisconnect")
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

// TestBackpressureStalledMemberDoesNotBlockRoom runs 1B-T1 with the in-memory
// broadcast and, as 1I-T1, with the Redis broadcast. The Redis subscription registers
// 50 ms late, and the test waits for it: RoomLen counts only the instances subscribed
// when it asks.
//
// Covers 1B-T1 (S).
// Covers 1I-T1 (S).
func TestBackpressureStalledMemberDoesNotBlockRoom(t *testing.T) {
	for _, adapter := range []string{"memory", "redis"} {
		t.Run(adapter, func(t *testing.T) {
			h := hooks{connect: func(c Conn) error { c.Join("r"); return nil }}
			var s *miniredis.Miniredis
			if adapter == "redis" {
				s = miniredis.RunT(t)
				delayRedisSubscriptions(t, s, 50*time.Millisecond)
				h.setup = func(srv *Server) { useRedis(t, srv, s.Addr()) }
			}
			p := start(t, 'S', h)
			if s != nil { // RoomLen asks the instances subscribed to the root namespace
				waitRedisSubscribers(t, s, "socket.io-request#", 1)
			}
			healthy := newFakeConn(t)
			p.srv.serveConn(healthy)
			require.Equal(t, "0", recv(t, healthy.out, "the healthy member's CONNECT"))
			closeAtEnd(t, healthy, serverCompletion(p.srv, recv(t, p.conns, "OnConnect of the healthy member").EngineConn().ID()))

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
		})
	}
}

// Covers 1B-T2 (S).
func TestBackpressureCloseDeliversQueue(t *testing.T) {
	sides(t, "S", func(t *testing.T, side byte) {
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

			require.Equal(t, "1", recv(t, p.fc.out, "DISCONNECT"))

			require.Empty(t, drain(p.fc.out))
		}
	})
}

// Covers 1B-T3 (S).
func TestBackpressureCloseWritesOnDisconnectEmits(t *testing.T) {
	sides(t, "S", func(t *testing.T, side byte) {
		var p *peer
		p = start(t, side, hooks{disconnect: func(Conn) {
			p.emit("a")

			require.Equal(t, "1", recv(t, p.fc.out, "DISCONNECT before OnDisconnect emits"))

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

// Covers 1B-T4 (S).
func TestBackpressureCloseDropsLateAndOverflowingEmits(t *testing.T) {
	sides(t, "S", func(t *testing.T, side byte) {
		p := start(t, side, hooks{disconnect: func(c Conn) { flood(c, defaultWriteBufferSize+1) }}) // the last finds the queue full
		p.stall(t, p.nc, 0)
		require.NoError(t, p.Close())
		p.fc.release <- struct{}{} // writes "first", then blocks on the next packet
		require.Equal(t, ev("first"), recv(t, p.fc.out, "the first packet"))
		recv(t, p.fc.held, "the writer to block on the second packet")
		p.emit("after the seal") // the queue has room
		close(p.fc.release)

		require.Equal(t, "1", recv(t, p.fc.out, "DISCONNECT"))

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
// Covers 1B-T5 (S).
func TestBackpressureCloseStopsDispatch(t *testing.T) {
	sides(t, "S", func(t *testing.T, side byte) {
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

// Covers 1B-T6 (S).
func TestBackpressureDrainDeadline(t *testing.T) {
	sides(t, "S", func(t *testing.T, side byte) {
		p := start(t, side, hooks{setup: func(s *Server) { s.limits.drainTimeout = 50 * time.Millisecond }})
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
// Covers 1B-T7 (S).
// Covers 1B-T8 (S).
// Covers 1B-T9 (S).
// Covers 1B-T12 (S).
// Covers 1B-T13 (S).
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
	sides(t, "S", func(t *testing.T, side byte) {
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

// Covers 1B-T10 (S).
func TestBackpressureEncodeErrorRecoversAfterReport(t *testing.T) {
	sides(t, "S", func(t *testing.T, side byte) {
		p := start(t, side, hooks{}, "/a")
		nc, _, _ := p.sub(t)
		drain(p.fc.out)
		nc.Emit("bad", make(chan int))
		got := recv(t, p.errs, "encode error report")
		require.Equal(t, nc.Namespace(), got.nsp)
		require.Equal(t, "4\"encode error\"\n", recv(t, p.fc.out, "ERROR packet"))
		nc.Emit("next", 42)
		require.Contains(t, recv(t, p.fc.out, "following event"), `"next",42`)
		require.False(t, isDone(p.fc.closed))
		require.Empty(t, drain(p.errs))
	})
}

// Covers 1B-T11 (S).
func TestBackpressureConnectFailureDiscards(t *testing.T) {
	sides(t, "S", func(t *testing.T, side byte) {
		var p *peer
		reportsAtDisconnect := make(chan int, 1)
		p = newPeer(t, side, hooks{
			connect:    func(c Conn) error { c.Emit("q"); return errors.New("refused") },
			disconnect: func(Conn) { reportsAtDisconnect <- len(p.nilErrs) },
		})

		p.connect(t)
		require.False(t, isDone(p.fc.closed))
		require.Len(t, drain(p.nilErrs), 1)
		require.Empty(t, drain(p.fc.out), "rejected OnConnect emission leaked")
		close(p.fc.peerGone)
		p.disconnected(t)
		require.Empty(t, reportsAtDisconnect)

	})
}

// Covers 1B-T14 (S).
func TestBackpressureNamespaceDisconnectRacesClose(t *testing.T) {
	sides(t, "S", func(t *testing.T, side byte) {
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

// Covers 1B-T15 (S).
// Covers 1B-T16 (S).
func TestBackpressureOverflow(t *testing.T) {
	sides(t, "S", func(t *testing.T, side byte) {
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
	sides(t, "S", func(t *testing.T, side byte) {
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
			if n == 0 {
				require.False(t, isDone(p.fc.closed))
				require.NoError(t, p.Close())
			}
			p.disconnected(t, others...)
			require.Empty(t, drain(p.errs), "a second report")
		}
	})
}

// Covers 1B-T17 (S).
// Covers 1B-T18 (S).
func TestBackpressureOverflowInOnConnect(t *testing.T) {
	for _, connectErr := range []error{nil, errors.New("refused"), ErrWriteBufferFull} {
		cc, late := Conn(nil), []bool(nil) // late, per report: made after the discard, which precedes OnDisconnect
		p := newPeer(t, 'S', hooks{
			connect: func(c Conn) error {
				cc = c
				flood(c, defaultWriteBufferSize+1)
				return connectErr
			},
			onError: func(Conn, error) { late = append(late, isDone(cc.EngineConn().(*fakeConn).closed)) },
		})
		p.connect(t)
		require.True(t, isDone(p.completion()), "serveConn did not close the connection itself")
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

// A failed connect's close starts after root OnError returns, so an Emit during that call overflows.
func TestBackpressureConnectFailureReportsOverflowFromOnError(t *testing.T) {
	refused := errors.New("refused")
	for _, late := range []func(*peer, Conn){
		func(_ *peer, c Conn) { c.Emit("late") },
		func(p *peer, _ Conn) { p.srv.BroadcastToRoom("/", "r", "late") },
		func(_ *peer, c Conn) { <-inBackground(func() { c.Emit("late") }) },
	} {
		p, kept, discarded := (*peer)(nil), Conn(nil), []bool(nil) // late is dropped after the overflow report
		p = newPeer(t, 'S', hooks{
			connect: func(c Conn) error { kept = c; c.Join("r"); flood(c, defaultWriteBufferSize); return refused },
			onError: func(Conn, error) { discarded = append(discarded, isDone(p.fc.closed)); late(p, kept) },
		})
		p.connect(t)
		require.False(t, isDone(p.fc.closed))
		require.Equal(t, []error{refused}, drain(p.nilErrs), "one rejection report")
		require.Equal(t, []bool{false}, discarded, "reported before close")
		_ = p.nc.Close()
		p.disconnected(t)
		require.Empty(t, drain(p.errs))
		require.Empty(t, drain(p.fc.out), "a packet was written")
	}
}

// Covers 1B-T19 (S).
// Covers 1B-T20 (S).
func TestBackpressureOverflowInBroadcast(t *testing.T) {
	for _, overflow := range []func(*Server){
		func(s *Server) { s.BroadcastToRoom("/", "r", "x") },
		func(s *Server) { s.ForEach("/", "r", func(c adapter.Socket) { c.Emit("x"); c.Emit("x") }) },
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

// Covers 1B-T21 (S).
func TestBackpressureOverflowInOnError(t *testing.T) {
	sides(t, "S", func(t *testing.T, side byte) {
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

// Covers 1B-T22 (S).
// Covers 1B-T23 (S).
// Covers 1B-T24 (S).
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
	sides(t, "S", func(t *testing.T, side byte) {
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

// Covers 1B-T25 (S).
func TestBackpressureNamespaceDisconnectKeepsSession(t *testing.T) {
	sides(t, "S", func(t *testing.T, side byte) {
		p := start(t, side, hooks{events: map[string]interface{}{"echo": func(Conn) string { return "ok" }}}, "/a")
		nc, frame, _ := p.sub(t)
		p.stall(t, p.nc, 0)
		p.send(t, frame)
		require.Equal(t, nc.Namespace(), recv(t, p.discs, "OnDisconnect of "+nc.Namespace()))
		close(p.fc.release)
		require.Equal(t, ev("first"), recv(t, p.fc.out, "the packet queued before the DISCONNECT"))

		p.send(t, `21["echo"]`)
		require.Equal(t, `31["ok"]`+"\n", recv(t, p.fc.out, "the root namespace's ACK"))

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
	require.Equal(t, "1", recv(t, p.fc.out, "DISCONNECT"))
	require.Equal(t, ev("b"), recv(t, p.fc.out, "the OnDisconnect Emit"))
	recv(t, p.fc.closed, "engine.io close after the drain")
	p.disconnected(t, "/")
	require.Empty(t, drain(p.fc.out))
	require.Empty(t, drain(p.nilErrs), "reported as a connect failure")
}

// Covers 1B-T27 (S).
func TestBackpressureLibraryPacketOverflow(t *testing.T) {
	for _, tc := range []struct {
		side  byte
		frame string
		nsps  []string // the last one overflows
	}{{'S', `21["echo"]`, []string{"/"}}, {'S', "0/a", []string{"/", "/a"}}} {
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
	p := start(t, 'S', hooks{events: map[string]interface{}{"num": func(_ Conn, n int) int { return n }}}, "/a")
	p.join(t, "/a")
	p.send(t, `2/a,["num","x"]`)
	require.Equal(t, "/a", recv(t, p.errs, "the report").nsp)
	p.send(t, `2/a,7["num",42]`)
	require.Equal(t, "3/a,7[42]\n", recv(t, p.fc.out, "a valid event after the decode error"))
	require.NoError(t, p.Close())
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
	bc := adapter.NewMemory()
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
	bc := adapter.NewMemory()
	bc.Join("r", &emitConn{id: "a"})
	bc.Join("r", &emitConn{id: "b"})
	var visited []string
	recv(t, inBackground(func() {
		bc.ForEach("r", func(c adapter.Socket) {
			visited = append(visited, c.ID())
			bc.Leave("r", c)
			bc.Join("moved", c)
		})
	}), "ForEach whose callback changes rooms")
	require.ElementsMatch(t, []string{"a", "b"}, visited)
	require.Equal(t, 0, bc.Len("r"))
	require.Equal(t, 2, bc.Len("moved"))
}

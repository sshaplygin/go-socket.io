package connection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	namespaceImpl "github.com/sshaplygin/go-socket.io/internal/namespace"
)

type Conn = namespaceImpl.Conn

const waitFor = 5 * time.Second
const defaultWriteBufferSize = DefaultWriteBufferSize

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

func (f *gatedFinishConn) NextWriter(ft eioparser.FrameType) (io.WriteCloser, error) {
	w, err := f.fakeConn.NextWriter(ft)
	if err != nil {
		f.writerFailed.Do(func() { close(f.writeFailed) })
	}
	return w, err
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

func (f *gatedFinishConn) Close() error {
	_ = f.fakeConn.Close()
	f.closeEntered.Do(func() { close(f.entered) })
	<-f.release
	return nil
}

func (w *frameWriter) Close() error { w.out <- w.String(); return nil }

func (f *fakeConn) ID() string { return fmt.Sprintf("%p", f) }

func (f *fakeConn) ReadyState() engineio.ReadyState { return engineio.StateOpen }

func (f *fakeConn) Transport() string { return "fake" }

func (f *fakeConn) Upgraded() bool { return false }

func (f *fakeConn) Context() interface{} { return nil }

// closeAtEnd closes f at cleanup and waits for the serving conn c, so that its "socketio:
// disconnect" records are logged before the test returns, not into the next test's recorder.
func closeAtEnd(t *testing.T, f *fakeConn, c *Connection) {
	t.Cleanup(func() {
		_ = f.Close()
		select {
		case <-c.done:
		case <-time.After(waitFor):
		}
	})
}

// gatedFinishConn lets the writer fail while finishRead is still obtaining
// the final engine close cause, before Close returns to the reader.
type gatedFinishConn struct {
	*fakeConn
	entered, release, writeFailed chan struct{}
	closeEntered, writerFailed    sync.Once
}

type frameWriter struct {
	bytes.Buffer
	out chan<- string
}

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

func flood(c Conn, n int) {
	for i := 0; i < n; i++ {
		c.Emit("x", i)
	}
}

func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(waitFor):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

type volatileBarrierConn struct {
	*fakeConn
	entered chan struct{}
	resume  chan struct{}
}

func (c *volatileBarrierConn) Writable() bool {
	c.entered <- struct{}{}
	<-c.resume
	return true
}

func ackEntries(c Conn) int {
	count := 0
	c.(*namespaceConn).ack.Range(func(_, _ interface{}) bool { count++; return true })
	return count
}

// peer is an owner-local concrete connection over a test engine and real registry/adapter.
// Public Server integration remains in the root tests; this fixture exposes no runtime state.
type hooks struct {
	setup      func(*peer)
	connect    func(Conn) error
	disconnect func(Conn)
	onError    func(Conn, error)
	events     map[string]interface{}
}
type peer struct {
	fc       *fakeConn
	nc       Conn
	handlers *namespaceImpl.Registry
	limits   struct {
		writeBufferSize int
		drainTimeout    time.Duration
	}
	connection *Connection
	conns      chan Conn
	errs       chan nsErr
	nilErrs    chan error
	discs      chan string
}

func newPeer(t *testing.T, _ byte, h hooks, nsps ...string) *peer {
	p := &peer{fc: newFakeConn(t), handlers: namespaceImpl.NewRegistry(), conns: make(chan Conn, 8), errs: make(chan nsErr, 128), nilErrs: make(chan error, 8), discs: make(chan string, 8)}
	p.limits.writeBufferSize, p.limits.drainTimeout = DefaultWriteBufferSize, DefaultDrainTimeout
	if h.setup != nil {
		h.setup(p)
	}
	for _, nsp := range append([]string{"/"}, nsps...) {
		nh := namespaceImpl.NewHandler()
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
		for event, f := range h.events {
			nh.OnEvent(event, f)
		}
		p.handlers.Set(fmtNS(nsp), nh)
	}
	return p
}
func (p *peer) serveConn(engine engineio.Conn) {
	p.connection = New(engine, p.handlers, p.limits.writeBufferSize, p.limits.drainTimeout, slog.Default())
	if p.connection.Start(nil) {
		go p.connection.Read()
	}
}
func start(t *testing.T, side byte, h hooks, nsps ...string) *peer {
	return newPeer(t, side, h, nsps...).connect(t)
}
func (p *peer) connect(t *testing.T) *peer {
	p.serveConn(p.fc)
	p.nc = recv(t, p.conns, "root OnConnect")
	if p.conn().rootRejected != nil {
		data, _ := json.Marshal(p.conn().rootRejected.Error())
		require.Equal(t, "4"+string(data)+"\n", recv(t, p.fc.out, "root rejection ERROR"))
	} else if !isDone(p.conn().done) || p.conn().draining {
		require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
	}
	closeAtEnd(t, p.fc, p.conn())
	return p
}
func (p *peer) conn() *Connection { return p.nc.(*namespaceConn).Connection }
func (p *peer) send(t *testing.T, frame string) {
	t.Helper()
	select {
	case p.fc.reads <- frame:
	case <-time.After(waitFor):
		t.Fatalf("timed out sending %q", frame)
	}
}
func (p *peer) join(t *testing.T, nsp string) Conn {
	p.send(t, "0"+nsp)
	recv(t, p.fc.out, "namespace CONNECT")
	return recv(t, p.conns, "namespace OnConnect")
}
func (p *peer) stall(t *testing.T, nc Conn, n int) {
	p.fc.hold.Store(true)
	nc.Emit("first")
	recv(t, p.fc.held, "held writer")
	flood(nc, n)
}
func (p *peer) disconnected(t *testing.T, nsps ...string) {
	recv(t, p.conn().done, "close completion")
	require.ElementsMatch(t, nsps, drain(p.discs))
}
func (p *peer) broadcastRoom(room, event string) {
	nh, _ := p.handlers.Get(rootNamespace)
	nh.Adapter().Send(room, event)
}
func expectBeforeBarrier(t *testing.T, p *peer, packets ...string) {
	p.nc.Emit("barrier")
	for _, packet := range packets {
		require.Equal(t, packet, recv(t, p.fc.out, "packet"))
	}
	require.Equal(t, ev("barrier"), recv(t, p.fc.out, "barrier"))
}

func TestCloseControlsIncludeClaimedNamespacesPrivate(t *testing.T) {
	p := start(t, 'S', hooks{setup: func(s *peer) { s.limits.writeBufferSize = 1 }}, "/a", "/b")
	a, b := p.join(t, "/a").(*namespaceConn), p.join(t, "/b").(*namespaceConn)
	p.stall(t, p.nc, 0)
	root := p.nc.(*namespaceConn)
	root.outMu.Lock()
	unlockRoot := sync.OnceFunc(root.outMu.Unlock)
	defer unlockRoot()
	var unlocks []func()
	for _, nc := range []*namespaceConn{a, b} {
		nc.outMu.Lock()
		unlock := sync.OnceFunc(nc.outMu.Unlock)
		unlocks = append(unlocks, unlock)
		defer unlock()
		go nc.Disconnect()
	}
	require.Eventually(t, func() bool {
		_, a := p.conn().namespaces.Get("/a")
		_, b := p.conn().namespaces.Get("/b")
		return !a && !b
	}, time.Second, time.Millisecond)
	closed := make(chan error, 1)
	go func() { closed <- p.nc.Close() }()
	recv(t, p.conn().closing, "connection close started")
	for _, unlock := range unlocks {
		unlock()
	}
	recv(t, p.discs, "first claimed namespace disconnected")
	recv(t, p.discs, "second claimed namespace disconnected")
	unlockRoot()
	require.NoError(t, recv(t, closed, "connection Close"))
	close(p.fc.release)
	require.Equal(t, ev("first"), recv(t, p.fc.out, "previous application packet"))
	packets := []string{recv(t, p.fc.out, "DISCONNECT"), recv(t, p.fc.out, "DISCONNECT"), recv(t, p.fc.out, "DISCONNECT")}
	require.ElementsMatch(t, []string{"1/a,", "1/b,", "1"}, packets)
}

func TestCloseWaitsForClaimedDisconnectPacketsPrivate(t *testing.T) {
	p := start(t, 'S', hooks{
		setup: func(s *peer) { s.limits.writeBufferSize = 1 },
		disconnect: func(c Conn) {
			if c.Namespace() != "/" {
				c.Emit("late from claimed namespace")
			}
		},
	}, "/a", "/b")
	a, b := p.join(t, "/a").(*namespaceConn), p.join(t, "/b").(*namespaceConn)
	p.stall(t, p.nc, 0)
	var unlocks []func()
	for _, nc := range []*namespaceConn{a, b} {
		nc.outMu.Lock()
		unlock := sync.OnceFunc(nc.outMu.Unlock)
		unlocks = append(unlocks, unlock)
		defer unlock()
		go nc.Disconnect()
	}
	require.Eventually(t, func() bool {
		_, a := p.conn().namespaces.Get("/a")
		_, b := p.conn().namespaces.Get("/b")
		return !a && !b
	}, time.Second, time.Millisecond)
	// Close returns before either already-claimed Disconnect can enqueue.
	require.NoError(t, p.nc.Close())
	p.nc.Emit("late")
	for _, unlock := range unlocks {
		unlock()
	}
	for i := 0; i < 3; i++ {
		recv(t, p.discs, "namespace disconnect")
	}
	close(p.fc.release)
	require.Equal(t, ev("first"), recv(t, p.fc.out, "previous application packet"))
	packets := []string{recv(t, p.fc.out, "DISCONNECT"), recv(t, p.fc.out, "DISCONNECT"), recv(t, p.fc.out, "DISCONNECT")}
	require.ElementsMatch(t, []string{"1/a,", "1/b,", "1"}, packets)
	recv(t, p.fc.closed, "engine closed after controls drain")
	require.Empty(t, p.fc.out, "Emit after Close must be dropped")
}

func TestCloseDeadlineWithClaimedDisconnectPrivate(t *testing.T) {
	p := start(t, 'S', hooks{}, "/a")
	a := p.join(t, "/a").(*namespaceConn)
	p.conn().drainTimeout = 50 * time.Millisecond
	p.stall(t, p.nc, 0)
	a.outMu.Lock()
	unlock := sync.OnceFunc(a.outMu.Unlock)
	defer unlock()
	go a.Disconnect()
	require.Eventually(t, func() bool {
		_, exists := p.conn().namespaces.Get("/a")
		return !exists
	}, time.Second, time.Millisecond)
	require.NoError(t, p.nc.Close())
	recv(t, p.fc.closed, "drain deadline while a control is still pending")
	unlock()
	for i := 0; i < 2; i++ {
		recv(t, p.discs, "namespace disconnect")
	}
	require.Empty(t, p.errs)
}

func TestBackpressurePeerCloseReportsOncePrivate(t *testing.T) {
	p := newPeer(t, 'S', hooks{})
	f := &gatedFinishConn{fakeConn: p.fc, entered: make(chan struct{}), release: make(chan struct{}), writeFailed: make(chan struct{})}
	release := sync.OnceFunc(func() { close(f.release) })
	defer release()
	p.serveConn(f)
	p.nc = recv(t, p.conns, "root OnConnect")
	require.Equal(t, "0", recv(t, p.fc.out, "root CONNECT"))
	closeAtEnd(t, p.fc, p.conn())
	p.stall(t, p.nc, 0)
	close(p.fc.peerGone)
	require.ErrorIs(t, recv(t, p.errs, "read error report").err, io.EOF)
	recv(t, f.entered, "engine Close entered")
	recv(t, f.writeFailed, "writer observed engine close")
	recv(t, p.conn().discard, "library close discards queue")
	require.Empty(t, p.errs, "writer failure during read finalization must not report again")
	release()
	p.disconnected(t, "/")
}

func TestBackpressureOverflowInOnConnectPrivate(t *testing.T) {
	for _, connectErr := range []error{nil, errors.New("refused"), ErrWriteBufferFull} {
		cc, late := (*Connection)(nil), []bool(nil) // late, per report: made after the discard, which precedes OnDisconnect
		p := newPeer(t, 'S', hooks{
			connect: func(c Conn) error {
				cc = c.(*namespaceConn).Connection
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

// A failed connect's close starts after root OnError returns, so an Emit during that call overflows.
func TestBackpressureConnectFailureReportsOverflowFromOnErrorPrivate(t *testing.T) {
	refused := errors.New("refused")
	for _, late := range []func(*peer, Conn){
		func(_ *peer, c Conn) { c.Emit("late") },
		func(p *peer, _ Conn) { p.broadcastRoom("r", "late") },
		func(_ *peer, c Conn) { <-inBackground(func() { c.Emit("late") }) },
	} {
		p, kept, discarded := (*peer)(nil), Conn(nil), []bool(nil) // late is dropped after the overflow report
		p = newPeer(t, 'S', hooks{
			connect: func(c Conn) error { kept = c; c.Join("r"); flood(c, defaultWriteBufferSize); return refused },
			onError: func(Conn, error) { discarded = append(discarded, isDone(kept.(*namespaceConn).discard)); late(p, kept) },
		})
		p.connect(t)
		require.False(t, isDone(p.fc.closed))
		require.Equal(t, []error{refused}, drain(p.nilErrs), "one rejection report")
		require.Equal(t, []bool{false}, discarded, "reported before close")
		p.conn().stop()
		p.disconnected(t)
		require.Empty(t, drain(p.errs))
		require.Empty(t, drain(p.fc.out), "a packet was written")
	}
}

func TestVolatileDropsQueuedPacketPrivate(t *testing.T) {
	p := start(t, 'S', hooks{})
	p.fc.hold.Store(true)
	p.nc.Emit("blocked")
	recv(t, p.fc.held, "held writer")
	p.nc.Emit("queued")
	require.NoError(t, p.nc.Volatile().Emit("dropped", func() {}))
	require.Zero(t, ackEntries(p.nc), "dropped volatile emit registers no ACK")
	p.fc.hold.Store(false)
	p.fc.release <- struct{}{}
	expectBeforeBarrier(t, p, "2[\"blocked\"]\n", "2[\"queued\"]\n")
	require.False(t, isDone(p.conn().closing))
}

func TestVolatileConcurrentAdmissionPrivate(t *testing.T) {
	for _, mode := range []string{"normal competitor", "volatile competitor"} {
		t.Run(mode, func(t *testing.T) {
			p := newPeer(t, 'S', hooks{setup: func(s *peer) { s.limits.writeBufferSize = 1 }})
			gate := &volatileBarrierConn{fakeConn: p.fc, entered: make(chan struct{}, 2), resume: make(chan struct{})}
			p.serveConn(gate)
			p.nc = recv(t, p.conns, "root connect")
			require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
			closeAtEnd(t, p.fc, p.conn())
			p.fc.hold.Store(true)
			p.nc.Emit("inflight")
			recv(t, p.fc.held, "held writer")
			done := make(chan error, 2)
			emit := func() { done <- p.nc.Volatile().Emit("volatile", func() {}) }
			go emit()
			recv(t, gate.entered, "first volatile readiness check")
			if mode == "normal competitor" {
				p.nc.Emit("normal")
			} else {
				go emit()
				recv(t, gate.entered, "second volatile readiness check")
			}
			close(gate.resume)
			require.NoError(t, recv(t, done, "volatile emit"))
			wantACKs := 0
			if mode == "volatile competitor" {
				require.NoError(t, recv(t, done, "second volatile emit"))
				wantACKs = 1
			}
			require.False(t, isDone(p.conn().closing), "a losing volatile packet must drop without overflow")
			require.Equal(t, wantACKs, ackEntries(p.nc), "only an admitted packet registers an ACK")
			p.fc.hold.Store(false)
			p.fc.release <- struct{}{}
			require.Equal(t, "2[\"inflight\"]\n", recv(t, p.fc.out, "inflight packet"))
			want := "2[\"normal\"]\n"
			if mode == "volatile competitor" {
				want = "21[\"volatile\"]\n"
			}
			require.Equal(t, want, recv(t, p.fc.out, "admitted packet"))
			expectBeforeBarrier(t, p)
			require.Empty(t, p.errs)
		})
	}
}

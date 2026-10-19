package socketio

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

func ackEntries(c Conn) int {
	count := 0
	c.(*namespaceConn).ack.Range(func(_, _ interface{}) bool { count++; return true })
	return count
}

// connectionPeer exercises queue and lifecycle state over a fake engine.
// It starts the transport connection directly, without the HTTP server.
type connectionHooks struct {
	setup      func(*connectionPeer)
	connect    func(Conn) error
	disconnect func(Conn)
	onError    func(Conn, error)
	events     map[string]interface{}
}
type connectionPeer struct {
	fc       *fakeConn
	nc       Conn
	handlers *namespaceRegistry
	limits   struct {
		writeBufferSize int
		drainTimeout    time.Duration
	}
	connection *connection
	conns      chan Conn
	errs       chan nsErr
	nilErrs    chan error
	discs      chan string
}

func newConnectionPeer(t *testing.T, _ byte, h connectionHooks, nsps ...string) *connectionPeer {
	p := &connectionPeer{fc: newFakeConn(t), handlers: newNamespaceRegistry(), conns: make(chan Conn, 8), errs: make(chan nsErr, 128), nilErrs: make(chan error, 8), discs: make(chan string, 8)}
	p.limits.writeBufferSize, p.limits.drainTimeout = defaultWriteBufferSize, defaultDrainTimeout
	if h.setup != nil {
		h.setup(p)
	}
	for _, nsp := range append([]string{"/"}, nsps...) {
		nh := newNamespaceHandler()
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
func (p *connectionPeer) serveConn(engine engineio.Conn) {
	p.connection = newConnection(engine, p.handlers, p.limits.writeBufferSize, p.limits.drainTimeout, slog.Default())
	if p.connection.Start(nil) {
		go p.connection.Read()
	}
}
func startConnectionPeer(t *testing.T, side byte, h connectionHooks, nsps ...string) *connectionPeer {
	return newConnectionPeer(t, side, h, nsps...).connect(t)
}
func (p *connectionPeer) connect(t *testing.T) *connectionPeer {
	p.serveConn(p.fc)
	p.nc = recv(t, p.conns, "root OnConnect")
	if p.conn().rootRejected != nil {
		data, _ := json.Marshal(p.conn().rootRejected.Error())
		require.Equal(t, "4"+string(data)+"\n", recv(t, p.fc.out, "root rejection ERROR"))
	} else if !isDone(p.conn().done) || p.conn().draining {
		require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
	}
	closeAtEnd(t, p.fc, (p.conn()).done)
	return p
}
func (p *connectionPeer) conn() *connection { return p.nc.(*namespaceConn).connection }
func (p *connectionPeer) send(t *testing.T, frame string) {
	t.Helper()
	select {
	case p.fc.reads <- frame:
	case <-time.After(waitFor):
		t.Fatalf("timed out sending %q", frame)
	}
}
func (p *connectionPeer) join(t *testing.T, nsp string) Conn {
	p.send(t, "0"+nsp)
	recv(t, p.fc.out, "namespace CONNECT")
	return recv(t, p.conns, "namespace OnConnect")
}
func (p *connectionPeer) stall(t *testing.T, nc Conn, n int) {
	p.fc.hold.Store(true)
	nc.Emit("first")
	recv(t, p.fc.held, "held writer")
	flood(nc, n)
}
func (p *connectionPeer) disconnected(t *testing.T, nsps ...string) {
	recv(t, p.conn().done, "close completion")
	require.ElementsMatch(t, nsps, drain(p.discs))
}
func (p *connectionPeer) broadcastRoom(room, event string) {
	nh, _ := p.handlers.Get(rootNamespace)
	nh.Adapter().Send(room, event)
}
func expectConnectionBeforeBarrier(t *testing.T, p *connectionPeer, packets ...string) {
	p.nc.Emit("barrier")
	for _, packet := range packets {
		require.Equal(t, packet, recv(t, p.fc.out, "packet"))
	}
	require.Equal(t, ev("barrier"), recv(t, p.fc.out, "barrier"))
}

func TestConnectionCloseControlsIncludeClaimedNamespaces(t *testing.T) {
	p := startConnectionPeer(t, 'S', connectionHooks{setup: func(s *connectionPeer) { s.limits.writeBufferSize = 1 }}, "/a", "/b")
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

func TestConnectionCloseWaitsForClaimedDisconnectPackets(t *testing.T) {
	p := startConnectionPeer(t, 'S', connectionHooks{
		setup: func(s *connectionPeer) { s.limits.writeBufferSize = 1 },
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

func TestConnectionCloseDeadlineWithClaimedDisconnect(t *testing.T) {
	p := startConnectionPeer(t, 'S', connectionHooks{}, "/a")
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

func TestConnectionBackpressurePeerCloseReportsOnce(t *testing.T) {
	p := newConnectionPeer(t, 'S', connectionHooks{})
	f := &gatedFinishConn{fakeConn: p.fc, entered: make(chan struct{}), release: make(chan struct{}), writeFailed: make(chan struct{})}
	release := sync.OnceFunc(func() { close(f.release) })
	defer release()
	p.serveConn(f)
	p.nc = recv(t, p.conns, "root OnConnect")
	require.Equal(t, "0", recv(t, p.fc.out, "root CONNECT"))
	closeAtEnd(t, p.fc, (p.conn()).done)
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

func TestConnectionBackpressureOverflowInOnConnect(t *testing.T) {
	for _, connectErr := range []error{nil, errors.New("refused"), ErrWriteBufferFull} {
		cc, late := (*connection)(nil), []bool(nil) // late, per report: made after the discard, which precedes OnDisconnect
		p := newConnectionPeer(t, 'S', connectionHooks{
			connect: func(c Conn) error {
				cc = c.(*namespaceConn).connection
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
func TestConnectionBackpressureConnectFailureReportsOverflowFromOnError(t *testing.T) {
	refused := errors.New("refused")
	for _, late := range []func(*connectionPeer, Conn){
		func(_ *connectionPeer, c Conn) { c.Emit("late") },
		func(p *connectionPeer, _ Conn) { p.broadcastRoom("r", "late") },
		func(_ *connectionPeer, c Conn) { <-inBackground(func() { c.Emit("late") }) },
	} {
		p, kept, discarded := (*connectionPeer)(nil), Conn(nil), []bool(nil) // late is dropped after the overflow report
		p = newConnectionPeer(t, 'S', connectionHooks{
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

func TestConnectionVolatileDropsQueuedPacket(t *testing.T) {
	p := startConnectionPeer(t, 'S', connectionHooks{})
	p.fc.hold.Store(true)
	p.nc.Emit("blocked")
	recv(t, p.fc.held, "held writer")
	p.nc.Emit("queued")
	require.NoError(t, p.nc.Volatile().Emit("dropped", func() {}))
	require.Zero(t, ackEntries(p.nc), "dropped volatile emit registers no ACK")
	p.fc.hold.Store(false)
	p.fc.release <- struct{}{}
	expectConnectionBeforeBarrier(t, p, "2[\"blocked\"]\n", "2[\"queued\"]\n")
	require.False(t, isDone(p.conn().closing))
}

func TestConnectionVolatileConcurrentAdmission(t *testing.T) {
	for _, mode := range []string{"normal competitor", "volatile competitor"} {
		t.Run(mode, func(t *testing.T) {
			p := newConnectionPeer(t, 'S', connectionHooks{setup: func(s *connectionPeer) { s.limits.writeBufferSize = 1 }})
			gate := &volatileBarrierConn{fakeConn: p.fc, entered: make(chan struct{}, 2), resume: make(chan struct{})}
			p.serveConn(gate)
			p.nc = recv(t, p.conns, "root connect")
			require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
			closeAtEnd(t, p.fc, (p.conn()).done)
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
			expectConnectionBeforeBarrier(t, p)
			require.Empty(t, p.errs)
		})
	}
}

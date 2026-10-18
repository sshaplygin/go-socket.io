package client

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

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/parser"
)

// fakeConn is a client Engine.IO connection driven by the test. The peer sends frames on reads (unbuffered: a
// send returns once the previous frame is handled); each written frame goes to out. While hold is
// set, NextWriter signals held and blocks until a send on release, its close, or Close; after
// Close, or with failWrite, it fails.
type fakeConn struct {
	engineclient.Conn               // methods the library does not call
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
func closeAtEnd(t *testing.T, f *fakeConn, c *conn) {
	t.Cleanup(func() {
		_ = f.Close()
		select {
		case <-c.done:
		case <-time.After(waitFor):
		}
	})
}

func (f *fakeConn) ID() string { return fmt.Sprintf("%p", f) }

func (f *fakeConn) ReadyState() engineclient.ReadyState { return engineclient.StateOpen }

func (f *fakeConn) Transport() string { return "fake" }

func (f *fakeConn) Upgraded() bool { return false }

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

func (p *peer) send(t *testing.T, frame string) {
	t.Helper()
	select {
	case p.fc.reads <- frame:
	case <-time.After(waitFor):
		t.Fatalf("timed out sending %q", frame)
	}
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

const waitFor = 5 * time.Second

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

type hooks struct {
	connect    func(Conn) error
	disconnect func(Conn)
	onError    func(Conn, error)
	events     map[string]interface{}
}
type peer struct {
	fc      *fakeConn
	cl      *Client
	nc      Conn
	emit    func(string, ...interface{})
	conns   chan Conn
	errs    chan nsErr
	nilErrs chan error
	discs   chan string
	extra   map[string]Conn
	nsps    []string
}

func newPeer(t *testing.T, side byte, h hooks, nsps ...string) *peer {
	p := &peer{fc: newFakeConn(t), conns: make(chan Conn, 8), errs: make(chan nsErr, 128), nilErrs: make(chan error, 8), discs: make(chan string, 8), extra: make(map[string]Conn), nsps: nsps}
	cl, err := New("http://127.0.0.1/", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	cl.dial = func(string) (engineclient.Conn, error) { return p.fc, nil }
	p.cl = cl
	for _, namespace := range append([]string{cl.namespace}, nsps...) {
		cl.Socket(namespace)
		nh := cl.createNamespace(fmtNS(namespace))
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
func start(t *testing.T, side byte, h hooks, nsps ...string) *peer {
	return newPeer(t, side, h, nsps...).connect(t)
}
func (p *peer) connect(t *testing.T) *peer {
	t.Helper()
	require.NoError(t, p.cl.Connect())
	for _, namespace := range p.nsps {
		require.Equal(t, "0"+namespace+",", recv(t, p.fc.out, "namespace CONNECT"))
	}
	p.send(t, "0")
	p.nc = recv(t, p.conns, "root OnConnect")
	for _, namespace := range p.nsps {
		p.send(t, "0"+namespace+",")
		p.extra[namespace] = recv(t, p.conns, "namespace OnConnect")
	}
	closeAtEnd(t, p.fc, p.conn())
	p.emit = p.cl.Emit
	return p
}

// These tests exercise the connection queue's close/drain barrier directly.
// Public Client.Close also sends namespace DISCONNECT, covered by the C10 suite.
func (p *peer) Close() error { return p.conn().Close() }
func (p *peer) conn() *conn  { return p.cl.conn }
func (p *peer) sub(t *testing.T) (Conn, string, []string) {
	if len(p.nsps) != 0 {
		namespace := p.nsps[len(p.nsps)-1]
		return p.extra[namespace], "1" + namespace + ",", []string{"/"}
	}
	return p.nc, "1", nil
}

// Covers 1B-T2 (C).
func TestBackpressureCloseDeliversQueue(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
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

// Covers 1B-T3 (C).
func TestBackpressureCloseWritesOnDisconnectEmits(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
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

// Covers 1B-T4 (C).
func TestBackpressureCloseDropsLateAndOverflowingEmits(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
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
// Covers 1B-T5 (C).
func TestBackpressureCloseStopsDispatch(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
		var ran atomic.Int32
		p := start(t, side, hooks{events: map[string]interface{}{"ev": func(Conn) { ran.Add(1) }}}, "/a")
		p.stall(t, p.nc, 0) // keeps the drain running
		require.NoError(t, p.Close())

		for _, frame := range []string{`2["ev"]`, map[byte]string{'S': "0/a", 'C': "0"}[side], "0/none", `2["ev"]`} {
			p.send(t, frame)
		}
		close(p.fc.release)
		recv(t, p.fc.closed, "engine.io close after the drain")
		p.disconnected(t, "/", "/a")
		require.Zero(t, ran.Load(), "an event handler ran")
		require.Empty(t, drain(p.conns), "OnConnect ran")
	})
}

// Covers 1B-T6 (C).
func TestBackpressureDrainDeadline(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
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
// Covers 1B-T7 (C).
// Covers 1B-T8 (C).
// Covers 1B-T9 (C).
// Covers 1B-T12 (C).
// Covers 1B-T13 (C).
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
	sides(t, "C", func(t *testing.T, side byte) {
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

// Covers 1B-T10 (C).
func TestBackpressureEncodeErrorRecoversAfterReport(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
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

// Covers 1B-T11 (C).
func TestBackpressureConnectFailureDiscards(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
		var p *peer
		reportsAtDisconnect := make(chan int, 1)
		p = newPeer(t, side, hooks{
			connect:    func(c Conn) error { c.Emit("q"); return nil },
			disconnect: func(Conn) { reportsAtDisconnect <- len(p.errs) },
		})

		p.fc.failWrite.Store(true) // the first event from OnConnect fails to encode
		// V1-10 returns after OPEN, before the server's root CONNECT.
		require.NoError(t, p.cl.Connect())
		p.send(t, "0")

		recv(t, p.fc.closed, "engine.io close of the failed connection")
		p.disconnected(t, "/")
		require.Equal(t, 1, recv(t, reportsAtDisconnect, "OnDisconnect"), "reports before OnDisconnect")
		require.Len(t, drain(p.errs), 1, "connect error reports")
		require.Empty(t, drain(p.fc.out), "a packet was written")
	})
}

// Covers 1B-T14 (C).
func TestBackpressureNamespaceDisconnectRacesClose(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
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

// Covers 1B-T15 (C).
// Covers 1B-T16 (C).
func TestBackpressureOverflow(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
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

// An encode error in an already admitted packet is still reported to its
// namespace when the peer disconnects that namespace before the write starts.
func TestBackpressureReportForDisconnectedNamespace(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
		p := start(t, side, hooks{}, "/a")
		nc, frame, others := p.sub(t)
		p.stall(t, nc, 0)
		nc.Emit("bad", make(chan int)) // admitted before the namespace disconnects
		p.send(t, frame)
		require.Equal(t, nc.Namespace(), recv(t, p.discs, "OnDisconnect on the DISCONNECT"))
		close(p.fc.release)
		got := recv(t, p.errs, "the report")
		require.Equal(t, nc.Namespace(), got.nsp)
		require.NotErrorIs(t, got.err, ErrWriteBufferFull)
		require.False(t, isDone(p.fc.closed))
		require.NoError(t, p.Close())
		p.disconnected(t, others...)
		require.Empty(t, drain(p.errs), "a second report")
	})
}

// Covers 1B-T21 (C).
func TestBackpressureOverflowInOnError(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
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

// Covers 1B-T22 (C).
// Covers 1B-T23 (C).
// Covers 1B-T24 (C).
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
	sides(t, "C", func(t *testing.T, side byte) {
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

// Covers 1B-T25 (C).
func TestBackpressureNamespaceDisconnectKeepsSession(t *testing.T) {
	sides(t, "C", func(t *testing.T, side byte) {
		p := start(t, side, hooks{events: map[string]interface{}{"echo": func(Conn) string { return "ok" }}}, "/a")
		nc, frame, _ := p.sub(t)
		p.stall(t, p.nc, 0)
		p.send(t, frame)
		require.Equal(t, nc.Namespace(), recv(t, p.discs, "OnDisconnect of "+nc.Namespace()))
		close(p.fc.release)
		require.Equal(t, ev("first"), recv(t, p.fc.out, "the packet queued before the DISCONNECT"))

		p.send(t, `2["echo"]`)
		require.False(t, isDone(p.fc.closed), "the session was closed")
		require.Empty(t, drain(p.discs))
	})
}

// Covers 1B-T27 (C).
func TestBackpressureLibraryPacketOverflow(t *testing.T) {
	for _, tc := range []struct {
		side  byte
		frame string
		nsps  []string // the last one overflows
	}{{'C', `21["echo"]`, []string{"/", "/a"}}} {
		p := start(t, tc.side, hooks{events: map[string]interface{}{"echo": func(Conn) string { return "ok" }}}, "/a")
		p.stall(t, p.nc, defaultWriteBufferSize)
		p.send(t, tc.frame) // the ACK or CONNECT reply finds the queue full
		recv(t, p.fc.closed, "engine.io close on overflow")
		p.overflowReported(t, "/")
		p.disconnected(t, tc.nsps...) // the read goroutine ran them, so it did not block
	}
}

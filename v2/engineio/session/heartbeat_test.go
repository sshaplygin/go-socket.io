package session

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/internal/logtest"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
)

// peerConn is a transport.Conn driven by a test that plays the peer: NextReader
// returns the packets sent on in, the packets written through NextWriter arrive on
// out, and Close ends both with io.EOF.
type peerConn struct {
	scriptConn
	in   chan scriptedPacket
	out  chan writtenPacket
	done chan struct{}
	once sync.Once
}

func newPeerConn() *peerConn {
	return &peerConn{
		in:   make(chan scriptedPacket),
		out:  make(chan writtenPacket, 16),
		done: make(chan struct{}),
	}
}

func (c *peerConn) NextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	select {
	case p := <-c.in:
		return p.ft, p.pt, io.NopCloser(strings.NewReader(p.data)), nil
	case <-c.done:
		return 0, 0, nil, io.EOF
	}
}

func (c *peerConn) NextWriter(ft frame.Type, pt packet.Type) (io.WriteCloser, error) {
	select {
	case <-c.done:
		return nil, io.EOF
	default:
		return &peerWriter{c: c, ft: ft, pt: pt}, nil
	}
}

func (c *peerConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

// send delivers p to the session's reader, or fails the test if the session does not
// read it in time.
func (c *peerConn) send(t *testing.T, pt packet.Type) {
	t.Helper()
	select {
	case c.in <- scriptedPacket{ft: frame.String, pt: pt}:
	case <-c.done:
		t.Fatalf("connection closed before %v was read", pt)
	case <-time.After(5 * time.Second):
		t.Fatalf("%v was not read", pt)
	}
}

// receive returns the next packet the session wrote, or fails the test.
func (c *peerConn) receive(t *testing.T) writtenPacket {
	t.Helper()
	select {
	case p := <-c.out:
		return p
	case <-time.After(5 * time.Second):
		t.Fatal("no packet written")
		return writtenPacket{}
	}
}

// waitClosed fails the test unless the connection is closed in time.
func (c *peerConn) waitClosed(t *testing.T) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("connection not closed")
	}
}

type peerWriter struct {
	c   *peerConn
	ft  frame.Type
	pt  packet.Type
	buf bytes.Buffer
}

func (w *peerWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *peerWriter) Close() error {
	select {
	case w.c.out <- writtenPacket{ft: w.ft, pt: w.pt, data: w.buf.String()}:
		return nil
	case <-w.c.done:
		return io.EOF
	}
}

// openSession creates a session over conn with the given heartbeat, opens it and
// consumes its OPEN packet. The session logs to rec.
func openSession(t *testing.T, conn *peerConn, interval, timeout time.Duration, rec *logtest.Recorder) *Session {
	t.Helper()
	s, err := New(conn, "sid1", "websocket", transport.ConnParameters{
		PingInterval: interval,
		PingTimeout:  timeout,
	}, slog.New(rec))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	opened := make(chan error, 1)
	go func() { opened <- s.InitSession() }()
	require.Equal(t, packet.OPEN, conn.receive(t).pt)
	require.NoError(t, <-opened)
	return s
}

// readUntilError calls NextReader until it fails and returns that error, as the one
// reader of an application does.
func readUntilError(s *Session) <-chan error {
	errs := make(chan error, 1)
	go func() {
		for {
			_, r, err := s.NextReader()
			if err != nil {
				errs <- err
				return
			}
			_ = r.Close()
		}
	}()
	return errs
}

// waitErr returns the error of readUntilError, or fails the test if the reader goes on.
func waitErr(t *testing.T, errs <-chan error) error {
	t.Helper()
	select {
	case err := <-errs:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("NextReader did not fail")
		return nil
	}
}

// closeRecord returns the only "engineio: session close" record.
func closeRecord(t *testing.T, rec *logtest.Recorder) map[string]string {
	t.Helper()
	got := rec.Find("engineio: session close")
	require.Len(t, got, 1)
	return got[0]
}

// TestSessionHeartbeat: the server sends PING (`2`, no data) PingInterval after the
// handshake and again PingInterval after each PONG; while PONGs answer, the session
// stays open past several PingTimeouts. When a PING goes unanswered for PingTimeout,
// the session closes with reason "ping timeout".
func TestSessionHeartbeat(t *testing.T) {
	const interval, timeout = 20 * time.Millisecond, 300 * time.Millisecond
	rec := logtest.NewRecorder()
	conn := newPeerConn()
	s := openSession(t, conn, interval, timeout, rec)
	errs := readUntilError(s)

	start := time.Now()
	for i := 0; i < 3; i++ {
		ping := conn.receive(t)
		require.Equal(t, writtenPacket{ft: frame.String, pt: packet.PING}, ping)
		conn.send(t, packet.PONG)
	}
	require.GreaterOrEqual(t, time.Since(start), 2*interval, "a PING comes PingInterval after the previous PONG")
	require.Empty(t, rec.Find("engineio: session close"), "answered PINGs keep the session open")

	lastPong := time.Now()
	require.Equal(t, packet.PING, conn.receive(t).pt)
	conn.waitClosed(t)
	require.GreaterOrEqual(t, time.Since(lastPong), interval+timeout)
	require.Error(t, waitErr(t, errs))
	require.Equal(t, "ping timeout", closeRecord(t, rec)["reason"])
}

// TestSessionPingTimeoutWithoutReader: the heartbeat does not depend on the application
// calling NextReader. With no reader at all the server still sends its PING, and the
// missing PONG still closes the session after PingTimeout.
func TestSessionPingTimeoutWithoutReader(t *testing.T) {
	rec := logtest.NewRecorder()
	conn := newPeerConn()
	openSession(t, conn, 10*time.Millisecond, 20*time.Millisecond, rec)

	require.Equal(t, packet.PING, conn.receive(t).pt)
	conn.waitClosed(t)
	require.Eventually(t, func() bool { return len(rec.Find("engineio: session close")) == 1 },
		5*time.Second, time.Millisecond)
	require.Equal(t, "ping timeout", closeRecord(t, rec)["reason"])
}

// TestSessionRejectsClientPing: in Engine.IO v4 only the server sends PING. A PING from
// the client is not answered: NextReader fails and the session closes as a transport
// error, as the Node server does ("invalid heartbeat direction").
func TestSessionRejectsClientPing(t *testing.T) {
	rec := logtest.NewRecorder()
	conn := newPeerConn()
	s := openSession(t, conn, time.Hour, time.Hour, rec)
	errs := readUntilError(s)

	conn.send(t, packet.PING)
	err := waitErr(t, errs)
	require.ErrorContains(t, err, "invalid heartbeat direction")
	conn.waitClosed(t)
	select {
	case p := <-conn.out:
		t.Fatalf("a client PING was answered with %v", p)
	default:
	}
	m := closeRecord(t, rec)
	require.Equal(t, "transport error", m["reason"])
	require.Equal(t, err.Error(), m["err"])
}

// TestSessionCloseStopsHeartbeat: once the session is closed it sends no PING and its
// timers do not close it a second time. Close races the ping timeout here; either way
// there is exactly one close record.
func TestSessionCloseStopsHeartbeat(t *testing.T) {
	t.Run("before the first PING", func(t *testing.T) {
		rec := logtest.NewRecorder()
		conn := newPeerConn()
		s := openSession(t, conn, 5*time.Millisecond, 5*time.Millisecond, rec)
		require.NoError(t, s.Close())
		time.Sleep(50 * time.Millisecond) // ten intervals: no PING may follow the close
		select {
		case p := <-conn.out:
			t.Fatalf("closed session wrote %v", p)
		default:
		}
		require.Equal(t, "forced close", closeRecord(t, rec)["reason"])
	})

	t.Run("concurrent with the ping timeout", func(t *testing.T) {
		rec := logtest.NewRecorder()
		conn := newPeerConn()
		s := openSession(t, conn, time.Millisecond, time.Millisecond, rec)
		time.Sleep(2 * time.Millisecond) // the closes meet the PING and its timeout
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _ = s.Close() }()
		}
		wg.Wait()
		conn.waitClosed(t)
		time.Sleep(20 * time.Millisecond) // a late timer would add a second record
		reason := closeRecord(t, rec)["reason"]
		require.Contains(t, []string{"ping timeout", "forced close"}, reason)
	})
}

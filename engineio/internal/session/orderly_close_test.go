package session

import (
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

type orderlyConn struct {
	scriptConn
	entered chan struct{}
	allow   chan struct{}
	done    chan struct{}
	once    sync.Once
	closes  atomic.Int32
}

func newOrderlyConn() *orderlyConn {
	return &orderlyConn{entered: make(chan struct{}), allow: make(chan struct{}), done: make(chan struct{})}
}

func (c *orderlyConn) CloseGracefully() error {
	close(c.entered)
	select {
	case <-c.allow:
		w, err := c.scriptConn.NextWriter(eioparser.String, eioparser.CLOSE)
		if err != nil {
			return err
		}
		return w.Close()
	case <-c.done:
		return io.EOF
	}
}

func (c *orderlyConn) Close() error {
	c.once.Do(func() {
		c.closes.Add(1)
		_ = c.scriptConn.Close()
		close(c.done)
	})
	return nil
}

func waitClose(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close did not complete")
	}
}

func TestSessionDonePrecedesTransportDrain(t *testing.T) {
	c := newOrderlyConn()
	s := newTestSession(t, c, "polling")
	t.Cleanup(func() { _ = s.Abort() })
	signal, ok := interface{}(s).(interface{ Done() <-chan struct{} })
	require.True(t, ok, "built-in session must expose close-start notification")
	done := signal.Done()
	closed := s.Closed()
	var callbacks atomic.Int32
	var callbackBeforeClosed, transportClosedInCallback bool
	s.SetCloseCallback(func() {
		callbacks.Add(1)
		transportClosedInCallback = c.isClosed()
		select {
		case <-closed:
		default:
			callbackBeforeClosed = true
		}
	})
	select {
	case <-done:
		t.Fatal("session is still open")
	default:
	}
	require.NoError(t, s.Init())
	require.NoError(t, s.Close())
	waitClose(t, done)
	waitClose(t, c.entered)
	select {
	case <-c.done:
		t.Fatal("transport should still be draining")
	default:
	}
	require.Equal(t, done, signal.Done())
	select {
	case <-closed:
		t.Fatal("Closed signaled before transport drain")
	default:
	}
	close(c.allow)
	waitClose(t, closed)
	require.True(t, transportClosedInCallback, "registry cleanup follows transport shutdown")
	require.True(t, callbackBeforeClosed, "Closed publishes completed registry cleanup")
	require.Equal(t, closed, s.Closed())
	require.NoError(t, s.Abort())
	require.Equal(t, int32(1), callbacks.Load(), "repeated close must not repeat registry cleanup")
}

func TestOrderlyCloseDrainsAcceptedWriter(t *testing.T) {
	c := newOrderlyConn()
	s := newTestSession(t, c, "polling")
	t.Cleanup(func() { _ = s.Abort() })
	require.Equal(t, 30*time.Second, s.closeTimeout)
	require.NoError(t, s.Init())
	w, err := s.NextWriter(eioparser.String)
	require.NoError(t, err)
	_, err = w.Write([]byte("buffered"))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	require.Equal(t, StateClosing, s.ReadyState())
	_, err = s.NextWriter(eioparser.String)
	require.Error(t, err, "no new frame is admitted after close starts")
	select {
	case <-c.entered:
		t.Fatal("CLOSE passed an unclosed application writer")
	default:
	}
	require.NoError(t, w.Close())
	waitClose(t, c.entered)
	require.False(t, c.isClosed())
	require.NoError(t, s.Close(), "repeated orderly close returns without waiting for a poll")
	close(c.allow)
	waitClose(t, s.Closed())
	require.Equal(t, StateClosed, s.ReadyState())
	require.Equal(t, int32(1), c.closes.Load())
	writes := c.written()
	require.Len(t, writes, 3)
	require.Equal(t, eioparser.OPEN, writes[0].pt)
	require.Equal(t, eioparser.MESSAGE, writes[1].pt)
	require.Equal(t, "buffered", writes[1].data)
	require.Equal(t, eioparser.CLOSE, writes[2].pt)
}

func TestOrderlyCloseDeadline(t *testing.T) {
	for _, unclosed := range []bool{false, true} {
		t.Run(map[bool]string{false: "no poll", true: "unclosed writer"}[unclosed], func(t *testing.T) {
			c := newOrderlyConn()
			s := newTestSession(t, c, "polling")
			s.closeTimeout = 20 * time.Millisecond
			require.NoError(t, s.Init())
			var w io.WriteCloser
			if unclosed {
				var err error
				w, err = s.NextWriter(eioparser.String)
				require.NoError(t, err)
			}
			require.NoError(t, s.Close())
			waitClose(t, s.Closed())
			require.Equal(t, StateClosed, s.ReadyState())
			require.Equal(t, ReasonForcedClose, s.closeError().Reason)
			if w != nil {
				require.NoError(t, w.Close())
			}
		})
	}
}

func TestAbortInterruptsOrderlyClose(t *testing.T) {
	for _, abort := range []struct {
		name  string
		close func(*Session) error
	}{
		{"root abort", (*Session).Abort}, {"shutdown", (*Session).Shutdown},
	} {
		t.Run(abort.name, func(t *testing.T) {
			c := newOrderlyConn()
			s := newTestSession(t, c, "polling")
			require.NoError(t, s.Init())
			require.NoError(t, s.Close())
			waitClose(t, c.entered)
			done := make(chan struct{})
			go func() { _ = abort.close(s); close(done) }()
			waitClose(t, done)
			waitClose(t, s.Closed())
			require.Equal(t, StateClosed, s.ReadyState())
			require.Equal(t, ReasonForcedClose, s.closeError().Reason, "first cause is retained")
			require.Equal(t, int32(1), c.closes.Load())
		})
	}
}

func TestFailedSessionDoesNotDrain(t *testing.T) {
	c := newOrderlyConn()
	s := newTestSession(t, c, "polling")
	require.NoError(t, s.Init())
	var errors atomic.Int32
	s.SetHooks(&Hooks{Error: func(*Session, error) { errors.Add(1) }})
	_ = s.fail(io.ErrUnexpectedEOF)
	require.NoError(t, s.Close())
	require.NoError(t, s.Close())
	require.Equal(t, ReasonTransportError, s.closeError().Reason)
	require.ErrorIs(t, s.closeError(), io.ErrUnexpectedEOF)
	require.Equal(t, int32(1), errors.Load())
	select {
	case <-c.entered:
		t.Fatal("a failed transport attempted an orderly close")
	default:
	}
}

func TestOrderlyCloseStopsUpgradeProbe(t *testing.T) {
	c := newOrderlyConn()
	s := newTestSession(t, c, "polling")
	t.Cleanup(func() { _ = s.Abort() })
	require.NoError(t, s.Init())
	probe := newClosingConn()
	s.Upgrade("websocket", probe)
	select {
	case <-probe.entered:
	case <-time.After(time.Second):
		t.Fatal("upgrade probe did not start")
	}
	require.NoError(t, s.Close())
	waitClose(t, probe.done)
	waitClose(t, c.entered)
	require.False(t, s.Upgraded())
	require.Equal(t, "polling", s.Transport())
	require.NoError(t, s.Abort())
	waitClose(t, s.Closed())
}

func TestAbortKeepsErrorBeforeTransportClose(t *testing.T) {
	for _, abort := range []struct {
		name  string
		close func(*Session) error
	}{{"root abort", (*Session).Abort}, {"shutdown", (*Session).Shutdown}} {
		t.Run(abort.name, func(t *testing.T) {
			c := &scriptConn{}
			s := newTestSession(t, c, "polling")
			require.NoError(t, s.Init())
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			var calls atomic.Int32
			s.SetHooks(&Hooks{Error: func(*Session, error) { calls.Add(1); close(entered); <-release }})
			s.observe(reasonTransportError, io.ErrUnexpectedEOF)
			done := make(chan error, 2)
			go func() { done <- s.Close() }()
			waitClose(t, entered)
			attempted := make(chan struct{})
			go func() { close(attempted); done <- abort.close(s) }()
			waitClose(t, attempted)
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("repeated close waited for Error hook")
			}
			require.False(t, c.isClosed())
			unblock()

			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("close did not return after Error")
			}

			require.True(t, c.isClosed())
			require.Equal(t, int32(1), calls.Load())
			require.Equal(t, ReasonTransportError, s.closeError().Reason)
		})
	}
}

type peerClosingConn struct {
	*orderlyConn
	reading, sendClose chan struct{}
}

func (c *peerClosingConn) NextReader() (eioparser.FrameType, eioparser.PacketType, io.ReadCloser, error) {
	close(c.reading)
	<-c.sendClose
	return eioparser.String, eioparser.CLOSE, io.NopCloser(strings.NewReader("")), nil
}

func TestPeerCloseInterruptsOrderlyClose(t *testing.T) {
	c := &peerClosingConn{orderlyConn: newOrderlyConn(), reading: make(chan struct{}), sendClose: make(chan struct{})}
	s := newTestSession(t, c, "polling")
	t.Cleanup(func() { _ = s.Abort() })
	require.NoError(t, s.Init())
	read := make(chan error, 1)
	go func() { _, _, err := s.NextReader(); read <- err }()
	waitClose(t, c.reading)
	require.NoError(t, s.Close())
	waitClose(t, c.entered)
	close(c.sendClose)
	waitClose(t, c.done)
	waitClose(t, s.Closed())
	select {
	case err := <-read:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("peer close did not release the reader")
	}
	require.Equal(t, StateClosed, s.ReadyState())
	require.Equal(t, ReasonForcedClose, s.closeError().Reason, "first cause is preserved")
}

func TestReaderWaitsForOrderlyTransportClose(t *testing.T) {
	c := newOrderlyConn()
	s := newTestSession(t, c, "polling")
	t.Cleanup(func() { _ = s.Abort() })
	require.NoError(t, s.Init())
	require.NoError(t, s.Close())
	waitClose(t, c.entered)
	read := make(chan error, 1)
	go func() { _, _, err := s.NextReader(); read <- err }()
	select {
	case err := <-read:
		t.Fatalf("reader ended before orderly transport close: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(c.allow)
	select {
	case err := <-read:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("reader did not end after transport close")
	}
	require.Equal(t, StateClosed, s.ReadyState())
}

// drainingConn is an orderlyConn whose poll handler never returns on its own,
// as a response write without deadline support to a client that never reads.
type drainingConn struct {
	*orderlyConn
	drained chan struct{}
	aborts  atomic.Int32
	readErr error
}

func (c *drainingConn) Drained() <-chan struct{} { return c.drained }

func (c *drainingConn) Abort() error {
	c.aborts.Add(1)
	return c.Close()
}

// NextReader hands out a frame whose read fails with readErr, if set.
func (c *drainingConn) NextReader() (eioparser.FrameType, eioparser.PacketType, io.ReadCloser, error) {
	if c.readErr != nil {
		return eioparser.String, eioparser.MESSAGE, io.NopCloser(failingReader{c.readErr}), nil
	}
	return c.orderlyConn.NextReader()
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func newDrainingConn() *drainingConn {
	return &drainingConn{orderlyConn: newOrderlyConn(), drained: make(chan struct{})}
}

// The orderly close waits for the response carrying CLOSE only until its timer,
// Abort or Shutdown: a write the transport cannot expire does not keep the
// session open.
func TestOrderlyCloseDrainBound(t *testing.T) {
	for name, end := range map[string]func(*Session) error{
		"timer":    func(*Session) error { return nil },
		"abort":    (*Session).Abort,
		"shutdown": (*Session).Shutdown,
	} {
		t.Run(name, func(t *testing.T) {
			c := newDrainingConn()
			s := newTestSession(t, c, "polling")
			s.closeTimeout = time.Hour
			if name == "timer" {
				s.closeTimeout = 20 * time.Millisecond
			}
			require.NoError(t, s.Init())
			close(c.allow)
			require.NoError(t, s.Close())
			waitClose(t, c.done) // CLOSE written, transport closed, drain pending
			ended := make(chan error, 1)
			go func() { ended <- end(s) }()
			waitClose(t, s.Closed())
			require.NoError(t, <-ended)
			require.Equal(t, ReasonForcedClose, s.closeError().Reason)
			require.Equal(t, int32(1), c.aborts.Load())
			require.Equal(t, eioparser.CLOSE, c.written()[len(c.written())-1].pt)
		})
	}
}

// An open frame reader that fails while the session closes on Close returns
// its own error only once the close has finished, so that the caller's cleanup
// cannot abort the CLOSE still being delivered.
func TestFrameReaderFailureWaitsForClose(t *testing.T) {
	cause := errors.New("body read expired")
	c := newDrainingConn()
	c.readErr = cause
	s := newTestSession(t, c, "polling")
	s.closeTimeout = time.Hour
	require.NoError(t, s.Init())
	_, r, err := s.NextReader()
	require.NoError(t, err)
	close(c.allow)
	require.NoError(t, s.Close())
	waitClose(t, c.done)
	read := make(chan error, 1)
	go func() { _, err := r.Read(make([]byte, 1)); read <- err }()
	select {
	case err := <-read:
		t.Fatalf("the read returned %v before the close finished", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(c.drained)
	waitClose(t, s.Closed())
	require.ErrorIs(t, waitErr(t, read), cause)
}

func waitErr(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(time.Second):
		t.Fatal("no result")
		return nil
	}
}

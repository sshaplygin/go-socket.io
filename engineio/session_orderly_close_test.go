package engineio

import (
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

func TestOrderlyCloseDrainsAcceptedWriter(t *testing.T) {
	c := newOrderlyConn()
	s := newTestSession(t, c, "polling")
	t.Cleanup(func() { _ = s.abort() })
	require.Equal(t, 30*time.Second, s.closeTimeout)
	require.NoError(t, s.initSession())
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
	waitClose(t, s.closeDone)
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
			require.NoError(t, s.initSession())
			var w io.WriteCloser
			if unclosed {
				var err error
				w, err = s.NextWriter(eioparser.String)
				require.NoError(t, err)
			}
			require.NoError(t, s.Close())
			waitClose(t, s.closeDone)
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
		close func(*session) error
	}{
		{"root abort", (*session).abort}, {"shutdown", (*session).shutdown},
	} {
		t.Run(abort.name, func(t *testing.T) {
			c := newOrderlyConn()
			s := newTestSession(t, c, "polling")
			require.NoError(t, s.initSession())
			require.NoError(t, s.Close())
			waitClose(t, c.entered)
			done := make(chan struct{})
			go func() { _ = abort.close(s); close(done) }()
			waitClose(t, done)
			waitClose(t, s.closeDone)
			require.Equal(t, StateClosed, s.ReadyState())
			require.Equal(t, ReasonForcedClose, s.closeError().Reason, "first cause is retained")
			require.Equal(t, int32(1), c.closes.Load())
		})
	}
}

func TestFailedSessionDoesNotDrain(t *testing.T) {
	c := newOrderlyConn()
	s := newTestSession(t, c, "polling")
	require.NoError(t, s.initSession())
	var errors atomic.Int32
	s.setHooks(&sessionHooks{Error: func(*session, error) { errors.Add(1) }})
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
	t.Cleanup(func() { _ = s.abort() })
	require.NoError(t, s.initSession())
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
	require.NoError(t, s.abort())
	waitClose(t, s.closeDone)
}

func TestAbortKeepsErrorBeforeTransportClose(t *testing.T) {
	for _, abort := range []struct {
		name  string
		close func(*session) error
	}{{"root abort", (*session).abort}, {"shutdown", (*session).shutdown}} {
		t.Run(abort.name, func(t *testing.T) {
			c := &scriptConn{}
			s := newTestSession(t, c, "polling")
			require.NoError(t, s.initSession())
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			var calls atomic.Int32
			s.setHooks(&sessionHooks{Error: func(*session, error) { calls.Add(1); close(entered); <-release }})
			s.observe(reasonTransportError, io.ErrUnexpectedEOF)
			done := make(chan error, 2)
			go func() { done <- s.Close() }()
			waitClose(t, entered)
			attempted := make(chan struct{})
			go func() { close(attempted); done <- abort.close(s) }()
			waitClose(t, attempted)
			select {
			case err := <-done:
				t.Fatalf("close passed the blocked Error hook: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			require.False(t, c.isClosed())
			unblock()
			for i := 0; i < 2; i++ {
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-time.After(time.Second):
					t.Fatal("close did not return after Error")
				}
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
	t.Cleanup(func() { _ = s.abort() })
	require.NoError(t, s.initSession())
	read := make(chan error, 1)
	go func() { _, _, err := s.NextReader(); read <- err }()
	waitClose(t, c.reading)
	require.NoError(t, s.Close())
	waitClose(t, c.entered)
	close(c.sendClose)
	waitClose(t, c.done)
	waitClose(t, s.closeDone)
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
	t.Cleanup(func() { _ = s.abort() })
	require.NoError(t, s.initSession())
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

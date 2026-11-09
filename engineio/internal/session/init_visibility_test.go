package session

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// openHeldConn delivers the OPEN packet like scriptConn, then keeps the OPEN
// writer's Close from returning until release is called. It models the window
// in which the client already has the handshake while Init is still running.
type openHeldConn struct {
	scriptConn
	delivered chan struct{} // closed once the OPEN packet has been handed to the client
	release   chan struct{}
	once      sync.Once
}

func newOpenHeldConn() *openHeldConn {
	return &openHeldConn{delivered: make(chan struct{}), release: make(chan struct{})}
}

func (c *openHeldConn) unblock() { c.once.Do(func() { close(c.release) }) }

func (c *openHeldConn) NextWriter(ft eioparser.FrameType, pt eioparser.PacketType) (io.WriteCloser, error) {
	w, err := c.scriptConn.NextWriter(ft, pt)
	if err != nil || pt != eioparser.OPEN {
		return w, err
	}
	return &openHeldWriter{WriteCloser: w, c: c}, nil
}

type openHeldWriter struct {
	io.WriteCloser
	c *openHeldConn
}

func (w *openHeldWriter) Close() error {
	err := w.WriteCloser.Close()
	close(w.c.delivered)
	select {
	case <-w.c.release:
	case <-time.After(5 * time.Second):
	}
	return err
}

func startHeldInit(t *testing.T) (*Session, *openHeldConn, <-chan error) {
	t.Helper()
	c := newOpenHeldConn()
	t.Cleanup(c.unblock)
	s := newTestSession(t, c, "polling")
	initErr := make(chan error, 1)
	go func() { initErr <- s.Init() }()
	select {
	case <-c.delivered:
	case <-time.After(5 * time.Second):
		t.Fatal("OPEN packet was not delivered")
	}
	return s, c, initErr
}

func awaitInit(t *testing.T, initErr <-chan error) error {
	t.Helper()
	select {
	case err := <-initErr:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Init did not return")
		return nil
	}
}

// A client that has the OPEN packet may ask the server for its sessions at once.
func TestSessionIsOpenOnceOpenPacketIsDelivered(t *testing.T) {
	s, c, initErr := startHeldInit(t)
	require.Equal(t, StateOpen, s.ReadyState())
	c.unblock()
	require.NoError(t, awaitInit(t, initErr))
	require.Equal(t, StateOpen, s.ReadyState())
}

// The session may be visible as open while Init still owns the connection's
// writer; no other packet may overtake OPEN.
func TestSessionPacketsFollowOpenPacket(t *testing.T) {
	s, c, initErr := startHeldInit(t)
	written := make(chan error, 1)
	go func() {
		w, err := s.NextWriter(eioparser.String)
		if err == nil {
			_, err = w.Write([]byte("hello"))
		}
		if err == nil {
			err = w.Close()
		}
		written <- err
	}()
	select {
	case err := <-written:
		t.Fatalf("a packet was written before Init released the OPEN packet: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	c.unblock()
	require.NoError(t, awaitInit(t, initErr))
	require.NoError(t, <-written)
	got := c.written()
	require.Len(t, got, 2)
	require.Equal(t, eioparser.OPEN, got[0].pt)
	require.Equal(t, eioparser.MESSAGE, got[1].pt)
	require.Equal(t, "hello", got[1].data)
}

// A close that starts after OPEN was delivered but before Init returns ends the
// session: Init reports it, the session does not return to open and the
// transport closes once.
func TestSessionCloseDuringInitAfterOpenDelivered(t *testing.T) {
	s, c, initErr := startHeldInit(t)
	require.Equal(t, StateOpen, s.ReadyState())
	require.NoError(t, s.Close())
	require.True(t, c.isClosed())
	require.Equal(t, StateClosed, s.ReadyState())
	c.unblock()
	require.ErrorIs(t, awaitInit(t, initErr), io.ErrClosedPipe)
	require.Equal(t, StateClosed, s.ReadyState())
	require.Equal(t, ReasonForcedClose, s.closeError().Reason)
}

// A transport failure while OPEN is being delivered is reported by Init and
// leaves the session closed, never open.
func TestSessionOpenWriteFailureLeavesSessionClosed(t *testing.T) {
	want := io.ErrShortWrite
	c := &scriptConn{wCloseErr: want}
	s := newTestSession(t, c, "polling")
	require.ErrorIs(t, s.Init(), want)
	require.Equal(t, StateClosed, s.ReadyState())
	require.True(t, c.isClosed())
}

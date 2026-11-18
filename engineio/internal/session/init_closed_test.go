package session

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// blockedWriteConn hands out an OPEN writer whose Write waits for the transport
// to be closed and then fails as a closed pipe does, the way a response write
// ends when the session is closed under it.
type blockedWriteConn struct {
	scriptConn
	writing chan struct{} // closed when Write has started
	done    chan struct{} // closed with the transport
}

func newBlockedWriteConn() *blockedWriteConn {
	return &blockedWriteConn{writing: make(chan struct{}), done: make(chan struct{})}
}

func (c *blockedWriteConn) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return c.scriptConn.Close()
}

func (c *blockedWriteConn) NextWriter(eioparser.FrameType, eioparser.PacketType) (io.WriteCloser, error) {
	return &blockedWriter{c: c}, nil
}

type blockedWriter struct{ c *blockedWriteConn }

func (w *blockedWriter) Write([]byte) (int, error) {
	close(w.c.writing)
	select {
	case <-w.c.done:
	case <-time.After(5 * time.Second):
	}
	return 0, io.ErrClosedPipe
}

func (w *blockedWriter) Close() error { return nil }

// A close that comes from outside while the OPEN packet is being written makes the
// write fail; Init reports the close, not the write failure as a refusal.
func TestInitReportsCloseDuringOpenWrite(t *testing.T) {
	c := newBlockedWriteConn()
	s := newTestSession(t, c, "polling")
	initErr := make(chan error, 1)
	go func() { initErr <- s.Init() }()
	select {
	case <-c.writing:
	case <-time.After(5 * time.Second):
		t.Fatal("the OPEN packet was not being written")
	}
	_ = s.Close()
	err := awaitInit(t, initErr)
	require.ErrorIs(t, err, ErrClosedDuringInit)
	require.ErrorIs(t, err, io.ErrClosedPipe)
	require.Equal(t, StateClosed, s.ReadyState())
	require.Equal(t, ReasonForcedClose, s.closeError().Reason)
}

// A session closed before Init gets its writer has nothing to refuse.
func TestInitOnClosedSessionReportsClose(t *testing.T) {
	for name, closeSession := range map[string]func(*Session) error{
		"close":    (*Session).Close,
		"abort":    (*Session).Abort,
		"shutdown": (*Session).Shutdown,
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestSession(t, &scriptConn{}, "polling")
			require.NoError(t, closeSession(s))
			require.ErrorIs(t, s.Init(), ErrClosedDuringInit)
			require.Equal(t, StateClosed, s.ReadyState())
		})
	}
}

// A failure of the handshake itself, on a session nobody had closed, is not a close
// from outside: Init returns it as it is and the session does not stay open.
func TestInitReportsOwnWriteFailure(t *testing.T) {
	boom := errors.New("write failed")
	for name, c := range map[string]*scriptConn{
		"NextWriter":   {writerErr: boom},
		"writer Close": {wCloseErr: boom},
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestSession(t, c, "polling")
			err := s.Init()
			require.ErrorIs(t, err, boom)
			require.NotErrorIs(t, err, ErrClosedDuringInit)
			require.Equal(t, StateClosed, s.ReadyState())
			require.True(t, c.isClosed())
		})
	}
}

// failingWriteConn's OPEN writer fails its Write at once, on a healthy session.
type failingWriteConn struct {
	scriptConn
	err error
}

func (c *failingWriteConn) NextWriter(eioparser.FrameType, eioparser.PacketType) (io.WriteCloser, error) {
	return failingWriter{c.err}, nil
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
func (w failingWriter) Close() error              { return nil }

func TestInitReportsOwnWriteFailureOfPacketBody(t *testing.T) {
	boom := errors.New("write failed")
	c := &failingWriteConn{err: boom}
	s := newTestSession(t, c, "polling")
	err := s.Init()
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, ErrClosedDuringInit)
	require.Equal(t, StateClosed, s.ReadyState())
	require.True(t, c.isClosed())
}

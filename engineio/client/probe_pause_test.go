package client

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/stretchr/testify/require"
)

type pauseProbeConn struct{ transport.Conn }

func (pauseProbeConn) NextReader() (eioparser.FrameType, eioparser.PacketType, io.ReadCloser, error) {
	return eioparser.String, eioparser.PONG, io.NopCloser(strings.NewReader("probe")), nil
}
func (pauseProbeConn) NextWriter(eioparser.FrameType, eioparser.PacketType) (io.WriteCloser, error) {
	return clientFrameFailureIO{}, nil
}
func (pauseProbeConn) SetReadDeadline(time.Time) error  { return nil }
func (pauseProbeConn) SetWriteDeadline(time.Time) error { return nil }
func (pauseProbeConn) Close() error                     { return nil }

type countedPauseConn struct {
	pauseProbeConn
	resumes atomic.Int32
	release chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (c *countedPauseConn) Pause()  { close(c.entered); <-c.release }
func (c *countedPauseConn) Resume() { c.resumes.Add(1); c.once.Do(func() { close(c.release) }) }
func (c *countedPauseConn) PauseContext(ctx context.Context) error {
	close(c.entered)
	<-ctx.Done()
	return ctx.Err()
}

type pauseCandidate struct{ transport.Transport }

func (pauseCandidate) Name() string { return "websocket" }
func (pauseCandidate) Dial(*url.URL, http.Header) (transport.Conn, error) {
	return pauseProbeConn{}, nil
}

func TestProbeCancellationResumesOnce(t *testing.T) {
	old := &countedPauseConn{release: make(chan struct{}), entered: make(chan struct{})}
	c := &client{conn: old}
	c.initialize()
	result := make(chan error, 1)
	go func() { result <- c.probeTransport(time.Now().Add(5*time.Second), url.URL{}, nil, pauseCandidate{}) }()
	select {
	case <-old.entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start its pause")
	}
	require.NoError(t, c.Close())
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("probe did not cancel")
	}
	require.Equal(t, int32(1), old.resumes.Load())
	require.Same(t, old, c.connection())
	require.False(t, c.Upgraded())
}

// Hide PauseContext to exercise upgrades through legacy custom transports.
type legacyPauseConn struct {
	transport.Conn
	pauser *countedPauseConn
}

func (c legacyPauseConn) Pause()  { c.pauser.Pause() }
func (c legacyPauseConn) Resume() { c.pauser.Resume() }

func TestProbeCancellationSupportsLegacyPauser(t *testing.T) {
	underlying := &countedPauseConn{release: make(chan struct{}), entered: make(chan struct{})}
	old := legacyPauseConn{Conn: underlying, pauser: underlying}
	c := &client{conn: old}
	c.initialize()
	result := make(chan error, 1)
	go func() { result <- c.probeTransport(time.Now().Add(5*time.Second), url.URL{}, nil, pauseCandidate{}) }()
	select {
	case <-underlying.entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start its pause")
	}
	require.NoError(t, c.Close())
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("probe did not cancel")
	}
	require.GreaterOrEqual(t, underlying.resumes.Load(), int32(1))
	require.Equal(t, old, c.connection())
}

// joiningPauseConn holds cancellation cleanup until the test releases it.
type joiningPauseConn struct {
	countedPauseConn
	canceled chan struct{}
	finish   chan struct{}
	returned chan struct{}
	resumed  chan bool
}

func (c *joiningPauseConn) PauseContext(ctx context.Context) error {
	defer close(c.returned)
	close(c.entered)
	<-ctx.Done()
	close(c.canceled)
	<-c.finish
	return ctx.Err()
}

func (c *joiningPauseConn) Resume() {
	select {
	case <-c.returned:
		c.resumed <- true
	default:
		c.resumed <- false
	}
	c.countedPauseConn.Resume()
}

func TestProbePauseWaitsForCancellationCleanup(t *testing.T) {
	old := &joiningPauseConn{
		countedPauseConn: countedPauseConn{release: make(chan struct{}), entered: make(chan struct{})},
		canceled:         make(chan struct{}), finish: make(chan struct{}),
		returned: make(chan struct{}), resumed: make(chan bool, 2),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var release sync.Once
	finish := func() { release.Do(func() { close(old.finish) }) }
	defer finish()
	pause, err := startProbePause(ctx, old)
	require.NoError(t, err)
	cancel()
	select {
	case <-old.canceled:
	case <-time.After(time.Second):
		t.Fatal("pause did not observe cancellation")
	}
	done := make(chan struct{})
	go func() { pause.resume(); close(done) }()
	t.Cleanup(func() {
		finish()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("rollback did not finish")
		}
	})
	select {
	case <-old.resumed:
		t.Fatal("Resume ran before pause cancellation cleanup finished")
	case <-time.After(25 * time.Millisecond):
	}
	finish()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rollback did not resume the transport")
	}
	select {
	case afterReturn := <-old.resumed:
		require.True(t, afterReturn, "Resume must follow PauseContext returning")
	case <-time.After(time.Second):
		t.Fatal("rollback completed without calling Resume")
	}
	require.ErrorIs(t, pause.err, context.Canceled)
	require.Equal(t, int32(1), old.resumes.Load())
}

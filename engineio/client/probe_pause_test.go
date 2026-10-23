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

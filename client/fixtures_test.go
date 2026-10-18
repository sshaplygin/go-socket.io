package client_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

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

// rawClient is an engine.io connection that speaks socket.io packets as text.
type rawClient struct {
	t    *testing.T
	conn interface {
		NextReader() (eioparser.FrameType, io.ReadCloser, error)
		NextWriter(eioparser.FrameType) (io.WriteCloser, error)
		Close() error
		ID() string
	}
}

func (c *rawClient) send(pkt string) {
	c.t.Helper()
	w, err := c.conn.NextWriter(eioparser.String)
	require.NoError(c.t, err)
	_, err = w.Write([]byte(pkt))
	require.NoError(c.t, err)
	require.NoError(c.t, w.Close())
}

// shortPingOptions returns engine.io options with a short pingTimeout, so a raw
// engine.io client (which never sends CLOSE) does not hold teardown. The last session
// of such a test closes by ping timeout around or after the end of the test, so its
// records go to a discarding logger: with the default one they would reach the
// slog.Default capture of whichever test runs next.
func shortPingOptions() []engineio.Option {
	return []engineio.Option{engineio.WithPingTimeout(500 * time.Millisecond), engineio.WithPingInterval(200 * time.Millisecond), engineio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))}
}

// newTestServer starts srv behind httptest with shortPingOptions.
func newTestServer(t *testing.T, setup func(*socketio.Server)) *httptest.Server {
	t.Helper()
	srv := socketio.NewServer(socketio.WithEngineOptions(shortPingOptions()...))
	setup(srv)
	go func() { _ = srv.Serve() }()
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		_ = srv.Close()
		ts.Close()
	})
	return ts
}

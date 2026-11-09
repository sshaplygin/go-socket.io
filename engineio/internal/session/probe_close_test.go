package session

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// gatedProbe is a server-side websocket upgrade probe whose upgrade goroutine
// stops inside SetReadLimit, the first thing Session.upgradingAt does with the
// probe once it is published as the session's candidate. The window between
// the publication and the end of the configuration is the one a Close,
// Shutdown or Abort of the session can fall into.
type gatedProbe struct {
	transport.Conn
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	aborts  atomic.Int32
}

// open lets the upgrade goroutine finish configuring the probe.
func (p *gatedProbe) open() { p.once.Do(func() { close(p.release) }) }

func (p *gatedProbe) SetReadLimit(n int64) {
	close(p.entered)
	<-p.release
	if c, ok := p.Conn.(interface{ SetReadLimit(int64) }); ok {
		c.SetReadLimit(n)
	}
}

func (p *gatedProbe) Abort() error {
	p.aborts.Add(1)
	if c, ok := p.Conn.(interface{ Abort() error }); ok {
		return c.Abort()
	}
	return nil
}

// newGatedProbe returns a session whose upgrade goroutine is blocked configuring
// a real websocket probe, and the client end of the probe.
func newGatedProbe(t *testing.T) (*Session, *gatedProbe, *gorilla.Conn) {
	t.Helper()
	accepted := make(chan transport.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Default.Accept(w, r)
		if err == nil {
			accepted <- conn
		}
	}))
	t.Cleanup(srv.Close)
	peer, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	var conn transport.Conn
	select {
	case conn = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("no probe connection")
	}
	probe := &gatedProbe{Conn: conn, entered: make(chan struct{}), release: make(chan struct{})}
	s := newTestSession(t, &scriptConn{}, "polling")
	t.Cleanup(func() {
		probe.open()
		_ = s.Abort()
		if c, ok := conn.(interface{ Abort() error }); ok {
			_ = c.Abort()
		}
	})
	s.Upgrade("websocket", probe)
	select {
	case <-probe.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the upgrade did not start")
	}
	return s, probe, peer
}

var probeClosers = []struct {
	name string
	do   func(*Session) error
}{
	{"Close", (*Session).Close},
	{"Shutdown", (*Session).Shutdown},
	{"Abort", (*Session).Abort},
}

// A probe is not an established transport: when the session closes while an
// upgrade is in progress, the probe is dropped without a websocket close frame,
// so that its peer sees an abnormal closure, and without waiting for a reply.
func TestSessionCloseDropsUpgradeProbeWithoutCloseFrame(t *testing.T) {
	for _, closer := range probeClosers {
		t.Run(closer.name, func(t *testing.T) {
			s, probe, peer := newGatedProbe(t)
			done := make(chan error, 1)
			go func() { done <- closer.do(s) }()
			require.NoError(t, peer.SetReadDeadline(time.Now().Add(10*time.Second)))
			_, _, err := peer.ReadMessage()
			var closeErr *gorilla.CloseError
			require.ErrorAs(t, err, &closeErr, "the probe peer must see the connection end")
			require.Equal(t, gorilla.CloseAbnormalClosure, closeErr.Code, "a probe must not receive a close frame: %v", err)
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("close did not return")
			}
			require.Positive(t, probe.aborts.Load(), "the probe must be aborted")
		})
	}
}

// Closing the session must not read the probe's frames while the upgrade
// goroutine still configures the same connection: gorilla's SetReadLimit is not
// safe against a concurrent reader. The probe's peer sends a frame so that a
// reader of the probe processes it (and reads the limit) during the close. The
// race detector reports the access when a reader runs.
func TestSessionCloseDoesNotReadProbeWhileUpgradeConfiguresIt(t *testing.T) {
	for _, closer := range probeClosers {
		t.Run(closer.name, func(t *testing.T) {
			s, probe, peer := newGatedProbe(t)
			require.NoError(t, peer.WriteMessage(gorilla.TextMessage, []byte("2probe")))
			done := make(chan error, 1)
			go func() { done <- closer.do(s) }()
			// Let a reader of the probe, if the close starts one, process the
			// frame, then finish the configuration. Nothing here may order the
			// two: the race detector pairs accesses that no synchronization
			// orders, and a signal from the closer would be one. The close
			// that waits for a reply to its frame (up to 250 ms) is still
			// running when the configuration ends.
			time.Sleep(100 * time.Millisecond)
			probe.open()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("close did not return")
			}
			// The probe has ended: the peer reads a failure, not a timeout.
			require.NoError(t, peer.SetReadDeadline(time.Now().Add(10*time.Second)))
			_, _, err := peer.ReadMessage()
			var netErr interface{ Timeout() bool }
			require.False(t, errors.As(err, &netErr) && netErr.Timeout(), "the probe connection stayed open: %v", err)
		})
	}
}

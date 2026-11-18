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

// acceptProbe returns a server-side websocket connection, as the engine server
// hands it to Session.Upgrade, and the client end of it.
func acceptProbe(t *testing.T) (transport.Conn, *gorilla.Conn) {
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
	select {
	case conn := <-accepted:
		return conn, peer
	case <-time.After(5 * time.Second):
		t.Fatal("no probe connection")
		return nil, nil
	}
}

// newGatedProbe returns a session whose upgrade goroutine is blocked configuring
// a real websocket probe, and the client end of the probe.
func newGatedProbe(t *testing.T) (*Session, *gatedProbe, *gorilla.Conn) {
	t.Helper()
	conn, peer := acceptProbe(t)
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

// blockingPause is a polling stand-in whose Pause, the step between the client's
// UPGRADE and the switch of the session's transport, can be held.
type blockingPause struct {
	pausableConn
	entered chan struct{}
	release chan struct{}
}

func (c *blockingPause) Pause() {
	close(c.entered)
	<-c.release
	c.pausableConn.Pause()
}

// upgradeWindow is an upgrade of a session from polling to a real websocket that
// the test steps through: the peer plays the client's probe (2probe, 3probe, 5)
// and the upgrade goroutine is held at one step.
type upgradeWindow struct {
	s    *Session
	peer *gorilla.Conn
	held chan struct{} // closed when the upgrade goroutine reaches the held step
	gate chan struct{} // closed by open to let it continue
	open func()
}

// newUpgradeWindow holds the upgrade at step. "switched": inside Hooks.Upgrade,
// where the session's connection is already the former probe, which s.probe
// still names until the upgrade goroutine returns. "paused": inside the polling
// connection's Pause, after UPGRADE and before the switch. "open": the upgrade
// has finished and nothing is held.
func newUpgradeWindow(t *testing.T, step string) *upgradeWindow {
	t.Helper()
	conn, peer := acceptProbe(t)
	w := &upgradeWindow{peer: peer, held: make(chan struct{}), gate: make(chan struct{})}
	w.open = sync.OnceFunc(func() { close(w.gate) })
	var old transport.Conn = &pausableConn{}
	if step == "paused" {
		old = &blockingPause{entered: w.held, release: w.gate}
	}
	w.s = newTestSession(t, old, "polling")
	t.Cleanup(func() {
		w.open()
		_ = w.s.Abort()
		if c, ok := conn.(interface{ Abort() error }); ok {
			_ = c.Abort()
		}
	})
	if step == "switched" {
		w.s.SetHooks(&Hooks{Upgrade: func(*Session, string) { close(w.held); <-w.gate }})
	}
	w.s.Upgrade("websocket", conn)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(10*time.Second)))
	require.NoError(t, peer.WriteMessage(gorilla.TextMessage, []byte("2probe")))
	_, msg, err := peer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "3probe", string(msg))
	require.NoError(t, peer.WriteMessage(gorilla.TextMessage, []byte("5")))
	if step == "open" {
		require.Eventually(t, func() bool {
			w.s.upgradeLocker.RLock()
			defer w.s.upgradeLocker.RUnlock()
			return w.s.upgraded && w.s.probe == nil
		}, 10*time.Second, time.Millisecond, "the upgrade did not finish")
		return w
	}
	select {
	case <-w.held:
	case <-time.After(10 * time.Second):
		t.Fatalf("the upgrade did not reach %s", step)
	}
	return w
}

// closeCode closes the session with closer and returns the close code the probing
// client reads from the websocket.
func (w *upgradeWindow) closeCode(t *testing.T, closer func(*Session) error) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- closer(w.s) }()
	_, _, err := w.peer.ReadMessage()
	var closeErr *gorilla.CloseError
	require.ErrorAs(t, err, &closeErr, "the websocket must end with a close")
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("close did not return")
	}
	return closeErr.Code
}

// Once the session has switched to the websocket it is the established
// transport, also while the upgrade goroutine has not returned yet (it still
// runs Hooks.Upgrade, setDeadline and the polling connection's Close): Close and
// Shutdown send the close frame 1000, Abort drops the connection.
func TestSessionCloseAfterSwitchSendsCloseFrame(t *testing.T) {
	for _, step := range []string{"switched", "open"} {
		for _, closer := range probeClosers {
			want := gorilla.CloseNormalClosure
			if closer.name == "Abort" {
				want = gorilla.CloseAbnormalClosure
			}
			t.Run(step+"/"+closer.name, func(t *testing.T) {
				w := newUpgradeWindow(t, step)
				require.True(t, w.s.Upgraded())
				require.Equal(t, want, w.closeCode(t, closer.do))
			})
		}
	}
}

// Between the client's UPGRADE and the switch the websocket is still a probe: a
// close drops it without a close frame, and the upgrade goroutine, released
// afterwards, finds the closed session and leaves the websocket closed.
func TestSessionCloseBetweenUpgradeAndSwitchDropsProbe(t *testing.T) {
	for _, closer := range probeClosers {
		t.Run(closer.name, func(t *testing.T) {
			w := newUpgradeWindow(t, "paused")
			require.False(t, w.s.Upgraded())
			require.Equal(t, gorilla.CloseAbnormalClosure, w.closeCode(t, closer.do))
			w.open()
			require.Eventually(t, func() bool {
				w.s.upgradeLocker.RLock()
				defer w.s.upgradeLocker.RUnlock()
				return w.s.probe == nil
			}, 10*time.Second, time.Millisecond, "the upgrade goroutine did not return")
			require.False(t, w.s.Upgraded(), "a closed session must not switch")
		})
	}
}

// A probe that has not sent UPGRADE within the upgrade timeout is closed by the
// upgrade goroutine's timer through Close, which sends the close frame: the
// session is open, so the probing client is told that the probe ended in order,
// and polling continues.
func TestUpgradeTimeoutClosesProbeWithCloseFrame(t *testing.T) {
	conn, peer := acceptProbe(t)
	s := newTestSession(t, &pausableConn{}, "polling")
	s.SetLimits(1<<20, 50*time.Millisecond)
	t.Cleanup(func() { _ = s.Abort() })
	s.Upgrade("websocket", conn)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(10*time.Second)))
	_, _, err := peer.ReadMessage()
	var closeErr *gorilla.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, gorilla.CloseNormalClosure, closeErr.Code)
	require.False(t, s.Upgraded())
	select {
	case <-s.Closed():
		t.Fatal("an expired probe must not close the session")
	default:
	}
}

// A close of the session while the expiry timer closes the probe aborts it from
// another goroutine. Which close the client sees depends on the winner (1000
// from the timer, 1006 from the abort); the two must neither race nor hang.
func TestUpgradeTimeoutRacesSessionClose(t *testing.T) {
	for _, closer := range probeClosers {
		t.Run(closer.name, func(t *testing.T) {
			for i := 0; i < 12; i++ {
				conn, peer := acceptProbe(t)
				s := newTestSession(t, &pausableConn{}, "polling")
				s.SetLimits(1<<20, 20*time.Millisecond)
				s.Upgrade("websocket", conn)
				time.Sleep(time.Duration(i) * 3 * time.Millisecond)
				require.NoError(t, closer.do(s))
				require.NoError(t, peer.SetReadDeadline(time.Now().Add(10*time.Second)))
				_, _, err := peer.ReadMessage()
				var closeErr *gorilla.CloseError
				require.ErrorAs(t, err, &closeErr)
				require.Contains(t, []int{gorilla.CloseNormalClosure, gorilla.CloseAbnormalClosure}, closeErr.Code)
				require.NoError(t, s.Abort())
			}
		})
	}
}

// A second probe is rejected without protocol I/O as long as the first upgrade
// goroutine has not returned, also after it switched the session to its probe:
// s.probe names the first probe until then.
func TestSessionRejectsSecondProbeAfterSwitchUntilUpgradeReturns(t *testing.T) {
	w := newUpgradeWindow(t, "switched")
	second := newClosingConn()
	w.s.Upgrade("websocket", second)
	select {
	case <-second.done:
	case <-time.After(5 * time.Second):
		t.Fatal("a second probe was not closed")
	}
	select {
	case <-second.entered:
		t.Fatal("a second probe entered protocol I/O")
	default:
	}
	w.open()
}

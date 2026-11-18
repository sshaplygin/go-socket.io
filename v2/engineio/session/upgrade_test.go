package session

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/internal/logtest"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
)

// pausablePeer is a peerConn that counts Pause and Resume, as the polling transport
// is paused for an upgrade.
type pausablePeer struct{ *peerConn }

func (c pausablePeer) Pause() {
	c.mu.Lock()
	c.paused++
	c.mu.Unlock()
}

func (c pausablePeer) Resume() {
	c.mu.Lock()
	c.resumed++
	c.mu.Unlock()
}

// probe sends the upgrade probe `2probe` on next and checks the answer `3probe`.
func probe(t *testing.T, next *peerConn) {
	t.Helper()
	select {
	case next.in <- scriptedPacket{ft: frame.String, pt: packet.PING, data: "probe"}:
	case <-time.After(5 * time.Second):
		t.Fatal("probe not read")
	}
	require.Equal(t, writtenPacket{ft: frame.String, pt: packet.PONG, data: "probe"}, next.receive(t))
}

// TestSessionUpgradeAnswersPollWithNoop: once the probe is answered, the session
// writes a NOOP (`6`) to the old transport, before the client sends UPGRADE. On
// polling it answers the pending poll, which a client waits for before it pauses
// polling and sends UPGRADE (Engine.IO v4 upgrade sequence).
func TestSessionUpgradeAnswersPollWithNoop(t *testing.T) {
	old := pausablePeer{newPeerConn()}
	s := newTestSession(t, old, "polling")
	next := newPeerConn()
	done := make(chan struct{})
	go func() { defer close(done); s.upgrading("websocket", next) }()

	probe(t, next)
	require.Equal(t, writtenPacket{ft: frame.String, pt: packet.NOOP}, old.receive(t),
		"the NOOP must come before UPGRADE")

	next.send(t, packet.UPGRADE)
	<-done
	require.Equal(t, "websocket", s.Transport())
}

// TestSessionUpgradePausesOnUpgradePacket: the old transport is paused when UPGRADE
// (`5`) arrives, as the Node server pauses only then, not when the probe is answered.
func TestSessionUpgradePausesOnUpgradePacket(t *testing.T) {
	old := pausablePeer{newPeerConn()}
	s := newTestSession(t, old, "polling")
	next := newPeerConn()
	done := make(chan struct{})
	go func() { defer close(done); s.upgrading("websocket", next) }()

	probe(t, next)
	require.Equal(t, packet.NOOP, old.receive(t).pt)
	old.mu.Lock()
	paused := old.paused
	old.mu.Unlock()
	require.Zero(t, paused, "the probe does not pause the old transport")

	next.send(t, packet.UPGRADE)
	<-done
	require.Equal(t, "websocket", s.Transport())
	require.Equal(t, 1, old.paused)
	require.Equal(t, 0, old.resumed)
}

// TestSessionPingTimeoutDuringUpgrade: a ping timeout between the probe and UPGRADE
// closes the session once, closes the websocket candidate that waits for UPGRADE, and
// the session is not switched to it. The upgrade starts before the heartbeat here, so
// the probe is answered before the timeout can fire.
func TestSessionPingTimeoutDuringUpgrade(t *testing.T) {
	rec := logtest.NewRecorder()
	old := pausablePeer{newPeerConn()}
	s, err := New(old, "sid1", "polling", transport.ConnParameters{
		PingInterval: 5 * time.Millisecond, PingTimeout: 10 * time.Millisecond,
	}, slog.New(rec))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	next := newPeerConn()
	done := make(chan struct{})
	go func() { defer close(done); s.upgrading("websocket", next) }()
	probe(t, next)

	go func() { _ = s.InitSession() }() // starts the heartbeat; nobody answers its PING
	next.waitClosed(t)
	<-done
	old.waitClosed(t)
	require.Equal(t, "polling", s.Transport())
	require.Equal(t, "ping timeout", closeRecord(t, rec)["reason"])
}

// TestSessionUpgradeDefaultWindow: an upgrade started without the reservation of
// engineio.Server, so without a timeout, gets the 10 s default window instead of an
// expired one: its probe is answered and UPGRADE switches the session.
func TestSessionUpgradeDefaultWindow(t *testing.T) {
	s := newTestSession(t, pausablePeer{newPeerConn()}, "polling")
	next := newPeerConn()
	s.Upgrade("websocket", next)
	probe(t, next)
	next.send(t, packet.UPGRADE)
	require.Eventually(t, func() bool { return s.Transport() == "websocket" }, 5*time.Second, time.Millisecond)
}

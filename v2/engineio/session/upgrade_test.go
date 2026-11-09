package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
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

package session

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Covers G29
func TestConcurrentUpgradeProbeClosesOnlySecond(t *testing.T) {
	s := newTestSession(t, newOrderlyConn(), "polling")
	t.Cleanup(func() { _ = s.Abort() })
	first := newClosingConn()
	second := newClosingConn()
	s.Upgrade("websocket", first)
	select {
	case <-first.entered:
	case <-time.After(time.Second):
		t.Fatal("first probe did not start")
	}
	s.Upgrade("websocket", second)
	select {
	case <-second.done:
	case <-time.After(time.Second):
		t.Fatal("concurrent probe was not closed")
	}
	select {
	case <-second.entered:
		t.Fatal("second probe entered protocol I/O")
	default:
	}
	select {
	case <-first.done:
		t.Fatal("first probe was displaced")
	default:
	}
	s.upgradeLocker.RLock()
	require.Equal(t, first, s.probe)
	s.upgradeLocker.RUnlock()
	require.NoError(t, s.Abort())
	waitClose(t, first.done)
}

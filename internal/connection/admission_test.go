package connection

import (
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
	namespaceImpl "github.com/sshaplygin/go-socket.io/internal/namespace"
)

// These are the resolved values of every public option vector in
// TestOptionsReachConnections. The root test verifies resolution and wire admission;
// this owner-local case retains the original private storage assertions.
func TestResolvedConnectionLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  int
		drain time.Duration
	}{
		{"nil options", 64, time.Minute},
		{"zero", 64, time.Minute},
		{"heartbeat only", 64, time.Minute},
		{"negative", 64, time.Minute},
		{"custom", 5, 3 * time.Second},
		{"ordered", 5, 3 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(newFakeConn(t), namespaceImpl.NewRegistry(), tc.size, tc.drain, slog.Default())
			require.Equal(t, tc.size, cap(c.writeChan), "queue size")
			require.Equal(t, tc.drain, c.drainTimeout, "drain deadline")
		})
	}
}

// The real Server callback/wire vectors remain in TestBroadcastCallback. Keep
// their exact per-socket ACK-map assertion at the connection owner as well.
func TestLegacyBroadcastAckState(t *testing.T) {
	b := adapter.NewMemory()
	var peers []*peer
	for i := 0; i < 3; i++ {
		p := newPeer(t, 'S', hooks{})
		h, _ := p.handlers.Get(rootNamespace)
		h.SetAdapter(b, nil)
		p.connect(t)
		b.Join("room", p.nc)
		peers = append(peers, p)
	}
	var callbacks atomic.Int32
	callback := func() { callbacks.Add(1) }
	for index, send := range []func(){
		func() { b.Send("room", "ack", callback) },
		func() { b.SendAll("ack", callback) },
	} {
		send()
		for _, p := range peers {
			packet := recv(t, p.fc.out, "legacy callback packet")
			require.Contains(t, packet, "[\"ack\"]")
			idText := strings.TrimPrefix(strings.SplitN(packet, "[", 2)[0], "2")
			id, err := strconv.ParseUint(idText, 10, 64)
			require.NoError(t, err)
			_, registered := p.nc.(*namespaceConn).ack.Load(id)
			require.True(t, registered, "each socket registers its own callback")
			p.send(t, "3"+idText+"[]")
		}
		require.Eventually(t, func() bool { return callbacks.Load() == int32((index+1)*len(peers)) }, time.Second, time.Millisecond)
	}
}

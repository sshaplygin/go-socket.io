package client

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// expectBeforeBarrier proves absence as well as delivery without read timeouts.
func expectBeforeBarrier(t *testing.T, p *peer, packets ...string) {
	t.Helper()
	p.nc.Emit("barrier")
	for _, packet := range packets {
		require.Equal(t, packet, recv(t, p.fc.out, "broadcast packet"))
	}
	require.Equal(t, "2[\"barrier\"]\n", recv(t, p.fc.out, "barrier"))
}

func TestClientSendAcknowledgement(t *testing.T) {
	p := start(t, 'C', hooks{})
	answers := make(chan string, 1)
	p.nc.Send("hello", func(value string) { answers <- value })
	require.Equal(t, "21[\"message\",\"hello\"]\n", recv(t, p.fc.out, "message event"))
	p.send(t, "31[\"received\"]")
	require.Equal(t, "received", recv(t, answers, "message ACK"))
	p.nc.Volatile().Send("best effort")
	require.Equal(t, "2[\"message\",\"best effort\"]\n", recv(t, p.fc.out, "volatile message"))
}

package connection

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// Covers G21
func TestClientAutomaticAcknowledgementRequiresRequestAndListener(t *testing.T) {
	p := start(t, 'C', hooks{events: map[string]interface{}{
		"value":   func(Conn) string { return "value" },
		"barrier": func(Conn) string { return "barrier" },
	}})
	p.send(t, `2["value"]`)
	p.send(t, `27["unknown"]`)
	p.send(t, `299["barrier"]`)
	require.Equal(t, "399[\"barrier\"]\n", recv(t, p.fc.out, "only requested handled ACK"))
	p.cl.Socket("/").Once("once", func(Conn) string { return "once" })
	p.send(t, `28["once"]`)
	require.Equal(t, "38[\"once\"]\n", recv(t, p.fc.out, "once ACK"))
	p.send(t, `28["once"]`)
	p.send(t, `299["barrier"]`)
	require.Equal(t, "399[\"barrier\"]\n", recv(t, p.fc.out, "consumed once listener must not ACK"))
}

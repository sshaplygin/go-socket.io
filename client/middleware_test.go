package client

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientEventMiddlewareIsNoop(t *testing.T) {
	p := start(t, 'C', hooks{events: map[string]interface{}{"echo": func(_ Conn, n int) int { return n }}})
	p.nc.Use(func(string, []interface{}) error { t.Error("client middleware ran"); return nil })
	p.send(t, `27["echo",42]`)
	require.Equal(t, "37[42]\n", recv(t, p.fc.out, "client ACK"))
}

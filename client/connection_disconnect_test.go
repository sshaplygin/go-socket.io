package client

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientDisconnectLastNamespace(t *testing.T) {
	p := start(t, 'C', hooks{})
	require.Equal(t, p.fc.ID(), p.nc.ID())
	p.nc.SetContext("local")
	require.Equal(t, "local", p.nc.Context())
	p.nc.Disconnect()
	require.Equal(t, "1", recv(t, p.fc.out, "namespace DISCONNECT"))
	recv(t, p.fc.closed, "last socket closes the engine")
	p.disconnected(t, "/")
	require.False(t, p.nc.Connected())
}

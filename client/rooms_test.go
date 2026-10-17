package client

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientRoomsAndContext(t *testing.T) {
	p := start(t, 'C', hooks{})
	require.Equal(t, p.fc.ID(), p.nc.ID())
	require.ElementsMatch(t, []string{p.nc.ID()}, p.nc.Rooms())
	p.nc.Join("one", "two", "one")
	require.ElementsMatch(t, []string{p.nc.ID(), "one", "two"}, p.nc.Rooms())
	p.nc.Leave("one")
	p.nc.Leave("missing")
	require.ElementsMatch(t, []string{p.nc.ID(), "two"}, p.nc.Rooms())
	p.nc.SetContext("local")
	require.Equal(t, "local", p.nc.Context())
	p.nc.LeaveAll()
	require.Empty(t, p.nc.Rooms())
	p.nc.Join("after leave-all")
	p.nc.Disconnect()
	require.Equal(t, "1", recv(t, p.fc.out, "namespace DISCONNECT"))
	recv(t, p.fc.closed, "last socket closes the engine")
	p.disconnected(t, "/")
	require.Empty(t, p.nc.Rooms())
}

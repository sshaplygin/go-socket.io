package namespace

import (
	"testing"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/stretchr/testify/require"
)

type lookupAdapter struct {
	*adapter.Memory
	lookup func(string) adapter.Socket
}

func (a *lookupAdapter) BindSocketLookup(lookup func(string) adapter.Socket) { a.lookup = lookup }

type lookupSocket struct {
	Conn
	id string
}

func (s *lookupSocket) ID() string                                            { return s.id }
func (*lookupSocket) EmitWithOptions(adapter.Options, string, ...interface{}) {}

// Covers A8
func TestAdapterLookupSurvivesLeavingAllRooms(t *testing.T) {
	h := NewHandler()
	a := &lookupAdapter{Memory: adapter.NewMemory()}
	h.SetAdapter(a, nil)
	require.NotNil(t, a.lookup, "remote commands need the connected registry, not the room index")
	c := &lookupSocket{id: "socket"}
	h.AddSocket(c)
	a.Join("room", c)
	a.LeaveAll(c)
	require.Empty(t, a.IDs())
	require.Same(t, c, a.lookup(c.ID()))
	h.RemoveSocket(c)
	require.Nil(t, a.lookup(c.ID()))
}

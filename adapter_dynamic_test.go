package socketio

import (
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

type observedAdapter struct {
	*broadcast
	mu           sync.Mutex
	joined       []string
	sends, lists int
}

func (b *observedAdapter) Join(room string, c Conn) {
	b.mu.Lock()
	b.joined = append(b.joined, room)
	b.mu.Unlock()
	b.broadcast.Join(room, c)
}
func (b *observedAdapter) SendTo(opts BroadcastOptions, event string, args ...interface{}) error {
	b.mu.Lock()
	b.sends++
	b.mu.Unlock()
	return b.broadcast.SendTo(opts, event, args...)
}
func (b *observedAdapter) IDs(rooms ...string) []string {
	b.mu.Lock()
	b.lists++
	b.mu.Unlock()
	return b.broadcast.IDs(rooms...)
}

// Covers S5
func TestSetAdapter(t *testing.T) {
	b := &observedAdapter{broadcast: newBroadcast()}
	var srv *Server
	var constructions []string
	ts := newTestServer(t, func(s *Server) {
		srv = s
		s.SetAdapter(func(nsp string) (Broadcast, error) { constructions = append(constructions, nsp); return b, nil })
		s.OnConnect("/", func(c Conn) error { c.Join("room"); return nil })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	n := srv.Of("/")
	require.NoError(t, n.To("room").Emit("adapter"))
	require.Equal(t, "2[\"adapter\"]\n", c.read())
	ids := n.SocketIDs("room")
	require.Len(t, ids, 1)
	require.Equal(t, []string{""}, constructions)
	b.mu.Lock()
	require.Equal(t, 1, b.sends)
	require.Equal(t, 1, b.lists)
	require.Equal(t, []string{"room", ids[0]}, b.joined)
	b.mu.Unlock()

	failure := errors.New("factory refused")
	failedCount := 0
	s := NewServer(nil)
	defer func() { _ = s.Close() }()
	s.SetAdapter(func(string) (Broadcast, error) { failedCount++; return nil, failure })
	require.Nil(t, s.Of("/failed").Adapter())
	require.ErrorIs(t, s.Serve(), failure)
	// A failed factory is called once; another registration retains its error.
	s.Of("/failed").OnEvent("event", func(Conn) {})
	require.Nil(t, s.Of("/failed").Adapter())
	require.Equal(t, 1, failedCount)
}

// Covers N5
func TestNamespaceAdapter(t *testing.T) {
	s := NewServer(nil)
	defer func() { _ = s.Close() }()
	first := newBroadcast()
	s.SetAdapter(func(string) (Broadcast, error) { return first, nil })
	n := s.Of("/first")
	require.Same(t, first, n.Adapter())
	r := miniredis.RunT(t)
	ok, err := s.Adapter(&RedisAdapterOptions{Addr: r.Addr()})
	require.True(t, ok)
	require.NoError(t, err)
	require.IsType(t, &redisBroadcast{}, s.Of("/redis").Adapter())
	last := newBroadcast()
	s.SetAdapter(func(string) (Broadcast, error) { return last, nil })
	require.Same(t, last, s.Of("/last").Adapter())
	require.Same(t, first, n.Adapter(), "factories do not replace existing adapters")
	s.SetAdapter(nil)
	require.IsType(t, &broadcast{}, s.Of("/memory").Adapter())
}

// Covers A2
func TestAdapterJoinRooms(t *testing.T) {
	b := &observedAdapter{broadcast: newBroadcast()}
	p := start(t, 'S', hooks{setup: func(s *Server) { s.SetAdapter(func(string) (Broadcast, error) { return b, nil }) }, connect: func(c Conn) error { c.Join("one", "two", "three"); return nil }})
	b.mu.Lock()
	require.Equal(t, []string{"one", "two", "three", p.nc.ID()}, b.joined)
	b.mu.Unlock()
	require.ElementsMatch(t, []string{"one", "two", "three", p.nc.ID()}, p.nc.Rooms())
	require.Equal(t, []string{p.nc.ID()}, b.IDs("one", "two"))
}

// Covers S9
func TestDynamicNamespaces(t *testing.T) {
	var srv *Server
	var parent *ServerNamespace
	seen := make(chan Conn, 4)
	queries := make(chan url.Values, 4)
	ts := newTestServer(t, func(s *Server) {
		srv = s
		s.OnConnect("/", func(Conn) error { return nil })
		s.OfFunc(func(_ string, query url.Values) bool { query.Set("mutated", "yes"); return false })
		parent = s.OfFunc(func(name string, query url.Values) bool {
			if !strings.HasPrefix(name, "/team/") {
				return false
			}
			queries <- query
			return true
		})
		parent.Use(func(c Conn) error { c.SetContext("accepted"); return nil })
		parent.OnConnect(func(c Conn) error { c.Join("room"); seen <- c; return nil })
		parent.OnEvent("where", func(c Conn) string { return c.Namespace() })
		parent.Once("inherited", func(c Conn) { c.Emit("inherited", c.Namespace()) })
	})
	a, b := dialRaw(t, ts.URL), dialRaw(t, ts.URL)
	require.Equal(t, "0", a.read())
	require.Equal(t, "0", b.read())
	a.send("0/team/one?token=first,")
	require.Equal(t, "0/team/one,", a.read())
	first := recv(t, seen, "first child socket")
	require.Equal(t, "accepted", first.Context())
	query := recv(t, queries, "match query")
	require.Equal(t, "first", query.Get("token"))
	require.Empty(t, query.Get("mutated"))
	child := srv.Of("/team/one")
	adapter := child.Adapter()
	// Registrations made now are copied into later children, not existing ones.
	parent.OnEvent("later", func(Conn) string { return "new child" })
	b.send("0/team/two,")
	require.Equal(t, "0/team/two,", b.read())
	second := recv(t, seen, "second child socket")
	recv(t, queries, "second match")
	require.NotSame(t, adapter, srv.Of("/team/two").Adapter())
	require.Nil(t, parent.Adapter())
	for i, c := range []*rawClient{a, b} {
		name := []string{"/team/one", "/team/two"}[i]
		c.send("2" + name + ",5[\"inherited\"]")
		require.Equal(t, "2"+name+",[\"inherited\",\""+name+"\"]\n", c.read())
		require.Equal(t, "3"+name+",5[]\n", c.read())
		c.send("2" + name + ",6[\"inherited\"]")
		require.Equal(t, "3"+name+",6[]\n", c.read(), "Once is copied independently into each child")
	}
	a.send(`2/team/one,1["where"]`)
	require.Equal(t, "3/team/one,1[\"/team/one\"]\n", a.read())
	a.send(`2/team/one,2["later"]`)
	require.Equal(t, "3/team/one,2[]\n", a.read())
	b.send(`2/team/two,3["later"]`)
	require.Equal(t, "3/team/two,3[\"new child\"]\n", b.read())
	require.NoError(t, parent.To("room").Local().Emit("both"))
	require.Equal(t, "2/team/one,[\"both\"]\n", a.read())
	require.Equal(t, "2/team/two,[\"both\"]\n", b.read())
	require.NoError(t, parent.Except(first.ID()).Send("second"))
	require.Equal(t, "2/team/two,[\"message\",\"second\"]\n", b.read())
	a.send(`2/team/one,4["where"]`)
	require.Equal(t, "3/team/one,4[\"/team/one\"]\n", a.read(), "excluded child has no extra packet")
	require.ElementsMatch(t, []string{first.ID(), second.ID()}, parent.SocketIDs())
	require.Len(t, parent.Sockets(), 2)
	require.ErrorIs(t, parent.Emit("no ack", func() {}), ErrBroadcastAck)
	// A repeated CONNECT reuses the existing child without another match call.
	a.send("1/team/one,")
	a.send("0/team/one,")
	require.Equal(t, "0/team/one,", a.read())
	recv(t, seen, "reconnected child")
	require.Same(t, adapter, srv.Of("/team/one").Adapter())
	require.Empty(t, queries)
	a.send("0/rejected,")
	require.Equal(t, "4/rejected,\"Invalid namespace\"\n", a.read())
	a.send(`21["unhandled"]`)
	require.Equal(t, "31[]\n", a.read(), "root remains usable")
}

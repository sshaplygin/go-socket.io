package socketio

import (
	"errors"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
	redisadapter "github.com/sshaplygin/go-socket.io/adapter/redis"
)

type observedAdapter struct {
	*adapter.Memory
	mu           sync.Mutex
	joined       []string
	sends, lists int
}

func (b *observedAdapter) Join(room string, c adapter.Socket) {
	b.mu.Lock()
	b.joined = append(b.joined, room)
	b.mu.Unlock()
	b.Memory.Join(room, c)
}
func (b *observedAdapter) SendTo(opts adapter.Options, event string, args ...interface{}) error {
	b.mu.Lock()
	b.sends++
	b.mu.Unlock()
	return b.Memory.SendTo(opts, event, args...)
}
func (b *observedAdapter) IDs(rooms ...string) []string {
	b.mu.Lock()
	b.lists++
	b.mu.Unlock()
	return b.Memory.IDs(rooms...)
}

// Covers S5
func TestSetAdapter(t *testing.T) {
	b := &observedAdapter{Memory: adapter.NewMemory()}
	var srv *Server
	var constructions []string
	ts := newTestServer(t, func(s *Server) {
		srv = s
		s.SetAdapter(func(nsp string) (adapter.Adapter, error) { constructions = append(constructions, nsp); return b, nil })
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
	s := NewServer()
	defer func() { _ = s.Close() }()
	s.SetAdapter(func(string) (adapter.Adapter, error) { failedCount++; return nil, failure })
	require.Nil(t, s.Of("/failed").Adapter())
	require.ErrorIs(t, s.Serve(), failure)
	// A failed factory is called once; another registration retains its error.
	s.Of("/failed").OnEvent("event", func(Conn) {})
	require.Nil(t, s.Of("/failed").Adapter())
	require.Equal(t, 1, failedCount)
}

// Covers N5
func TestNamespaceAdapter(t *testing.T) {
	s := NewServer()
	defer func() { _ = s.Close() }()
	first := adapter.NewMemory()
	s.SetAdapter(func(string) (adapter.Adapter, error) { return first, nil })
	n := s.Of("/first")
	require.Same(t, first, n.Adapter())
	r := miniredis.RunT(t)
	factory := redisadapter.New(redisadapter.WithAddr(r.Addr()))
	var installed adapter.Adapter
	s.SetAdapter(func(name string) (adapter.Adapter, error) {
		bc, err := factory(name)
		installed = bc
		return bc, err
	})
	redisNamespace := s.Of("/redis")
	require.NotNil(t, installed)
	require.Same(t, installed, redisNamespace.Adapter())
	require.Implements(t, (*io.Closer)(nil), installed)
	last := adapter.NewMemory()
	s.SetAdapter(func(string) (adapter.Adapter, error) { return last, nil })
	require.Same(t, last, s.Of("/last").Adapter())
	require.Same(t, first, n.Adapter(), "factories do not replace existing adapters")
	s.SetAdapter(nil)
	require.IsType(t, &adapter.Memory{}, s.Of("/memory").Adapter())
}

// Covers A2
func TestAdapterJoinRooms(t *testing.T) {
	b := &observedAdapter{Memory: adapter.NewMemory()}
	p := start(t, 'S', hooks{setup: func(s *Server) { s.SetAdapter(func(string) (adapter.Adapter, error) { return b, nil }) }, connect: func(c Conn) error { c.Join("one", "two", "three"); return nil }})
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

type parentErrorAdapter struct {
	*adapter.Memory
	send func(adapter.Options, string, ...interface{}) error
}

func (b *parentErrorAdapter) SendTo(opts adapter.Options, event string, args ...interface{}) error {
	return b.send(opts, event, args...)
}

func TestParentBroadcastError(t *testing.T) {
	s := NewServer()
	defer func() { _ = s.Close() }()
	failure := errors.New("child refused")
	var visited []string
	failAt := 2
	recipients := make(map[string]*redisTestConn)
	s.SetAdapter(func(name string) (adapter.Adapter, error) {
		memory := adapter.NewMemory()
		target := newRedisTestConn(name)
		memory.Join("room", target)
		recipients[name] = target
		return &parentErrorAdapter{Memory: memory, send: func(opts adapter.Options, event string, args ...interface{}) error {
			visited = append(visited, name)
			if len(visited) == failAt {
				return failure
			}
			return memory.SendTo(opts, event, args...)
		}}, nil
	})
	parent := s.OfFunc(func(string, url.Values) bool { return true })
	for _, name := range []string{"/one", "/two", "/three"} {
		require.NotNil(t, s.resolveNamespace(name, nil))
	}

	// Reject callbacks before calling any child adapter, regardless of its errors.
	require.ErrorIs(t, parent.To("room").Emit("callback", func() {}), ErrBroadcastAck)
	require.Empty(t, visited)
	for _, target := range recipients {
		require.Empty(t, target.events)
	}

	// Whichever child is visited first delivers; the next fails and stops fan-out.
	// The test deliberately makes no promise about the order of child snapshots.
	require.ErrorIs(t, parent.To("room").Send("value"), failure)
	require.Len(t, visited, 2)
	expectEvent(t, recipients[visited[0]], "message [value]")
	for _, target := range recipients {
		require.Empty(t, target.events, "the failing and unvisited children deliver nothing")
	}

	visited = nil
	failAt = 1
	require.ErrorIs(t, parent.Emit("first child fails"), failure)
	require.Len(t, visited, 1)
	for _, target := range recipients {
		require.Empty(t, target.events)
	}

	// An ordinary namespace still preserves the no-delivery-on-error guarantee.
	visited = nil
	require.ErrorIs(t, s.Of("/one").Emit("single child fails"), failure)
	require.Equal(t, []string{"/one"}, visited)
	for _, target := range recipients {
		require.Empty(t, target.events)
	}
}

package socketio

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// flagTarget records whether an adapter used the public single-socket Emitter.
type flagTarget struct {
	*redisTestConn
	writable bool
	volatile int
}

func (c *flagTarget) Volatile() Emitter { c.volatile++; return flagEmission{target: c} }

type flagEmission struct {
	Emitter
	target *flagTarget
}

func (e flagEmission) Emit(event string, args ...interface{}) error {
	if e.target.writable {
		e.target.Emit(event, args...)
	}
	return nil
}

func flagSocket(id string, writable bool) *flagTarget {
	return &flagTarget{redisTestConn: newRedisTestConn(id), writable: writable}
}

type exceptAdapter struct{ Broadcast }

func (exceptAdapter) SendTo(BroadcastOptions, string, ...interface{}) error {
	return errors.Join(errors.New("adapter refused"), ErrExceptUnsupported)
}

// Covers A4
func TestBroadcastSendTo(t *testing.T) {
	b := newBroadcast()
	a, c, d := flagSocket("a", true), flagSocket("c", false), flagSocket("d", true)
	b.Join("one", a)
	b.Join("two", a)
	b.Join("two", c)
	b.Join("two", d)
	require.NoError(t, b.SendTo(BroadcastOptions{Rooms: []string{"one", "two", "one"}, Except: []string{"d"}, Volatile: true, Local: true}, "news", 7))
	expectEvent(t, a.redisTestConn, "news [7]")
	require.Empty(t, a.events)
	require.Empty(t, c.events)
	require.Empty(t, d.events)
	require.Equal(t, 1, a.volatile)
	require.Equal(t, 1, c.volatile)
	require.Zero(t, d.volatile)
	// Delivery releases the adapter lock before invoking application code.
	a.onEmit = func() { b.LeaveAll(a) }
	require.NoError(t, b.SendTo(BroadcastOptions{Rooms: []string{"one"}}, "leave"))
	require.Empty(t, b.Rooms(a))
	p := start(t, 'S', hooks{})
	p.nc.(*namespaceConn).broadcast = exceptAdapter{Broadcast: nopBroadcast{}}
	require.ErrorIs(t, p.nc.Broadcast().Emit("nothing"), ErrExceptUnsupported)
	require.Empty(t, p.fc.out)
}

// Covers A5
func TestBroadcastIDs(t *testing.T) {
	b := newBroadcast()
	a, c := newRedisTestConn("a"), newRedisTestConn("c")
	b.Join("one", a)
	b.Join("two", a)
	b.Join("two", c)
	require.ElementsMatch(t, []string{"a", "c"}, b.IDs())
	require.ElementsMatch(t, []string{"a", "c"}, b.IDs("one", "two", "one"))
	require.Empty(t, b.IDs("missing"))
	ids := b.IDs("one")
	ids[0] = "changed"
	require.Equal(t, []string{"a"}, b.IDs("one"))
}

// Covers A1
func TestBroadcastRoomsMaps(t *testing.T) {
	b := newBroadcast()
	a, c := newRedisTestConn("a"), newRedisTestConn("c")
	check := func(want []string) {
		t.Helper()
		require.ElementsMatch(t, want, b.Rooms(a))
		for _, room := range want {
			require.Equal(t, 1, b.Len(room))
			require.Equal(t, []string{"a"}, b.IDs(room))
		}
		b.lock.RLock()
		defer b.lock.RUnlock()
		for room, members := range b.rooms {
			for id := range members {
				require.Contains(t, b.sids[id], room)
			}
		}
		for id, rooms := range b.sids {
			for room := range rooms {
				require.Contains(t, b.rooms[room], id)
			}
		}
	}
	for _, room := range []string{"one", "two", "three"} {
		b.Join(room, a)
	}
	b.Join("two", a)
	check([]string{"one", "two", "three"})
	require.ElementsMatch(t, []string{"one", "two", "three"}, b.AllRooms())
	b.Leave("two", a)
	check([]string{"one", "three"})
	b.Join("other", c)
	b.LeaveAll(a)
	check(nil)
	require.Equal(t, []string{"other"}, b.AllRooms())
	require.Equal(t, []string{"c"}, b.IDs())
	b.Clear("other")
	require.Empty(t, b.sids)
	require.Empty(t, b.rooms)
	require.Empty(t, b.IDs())
	b.Join("last", a)
	b.Leave("last", a)
	require.Empty(t, b.sids)
	require.Empty(t, b.rooms)
}

// Covers K11
func TestConnJoinRooms(t *testing.T) {
	p := start(t, 'S', hooks{})
	p.nc.Join("one", "two", "one", "three")
	require.ElementsMatch(t, []string{p.nc.ID(), "one", "two", "three"}, p.nc.Rooms())
	for _, room := range []string{"one", "two", "three"} {
		require.Equal(t, 1, p.srv.RoomLen("/", room))
	}
}

func broadcastPeers(t *testing.T, h hooks) (*broadcast, []*peer) {
	t.Helper()
	b := newBroadcast()
	var peers []*peer
	for i := 0; i < 3; i++ {
		p := start(t, 'S', h)
		p.nc.(*namespaceConn).broadcast = b
		b.Join(p.nc.ID(), p.nc)
		peers = append(peers, p)
	}
	return b, peers
}

// expectBeforeBarrier proves absence as well as delivery without read timeouts.
func expectBeforeBarrier(t *testing.T, p *peer, packets ...string) {
	t.Helper()
	p.nc.Emit("barrier")
	for _, packet := range packets {
		require.Equal(t, packet, recv(t, p.fc.out, "broadcast packet"))
	}
	require.Equal(t, "2[\"barrier\"]\n", recv(t, p.fc.out, "barrier"))
}

// Covers K14
func TestBroadcastExceptSender(t *testing.T) {
	for _, mode := range []string{"To", "Broadcast", "Volatile.To", "Volatile.Except", "Volatile.Local"} {
		t.Run(mode, func(t *testing.T) {
			var excluded string
			done := make(chan error, 1)
			b, peers := broadcastPeers(t, hooks{events: map[string]interface{}{"go": func(c Conn) {
				var e Emitter
				switch mode {
				case "To":
					e = c.To("room")
				case "Broadcast":
					e = c.Broadcast()
				case "Volatile.To":
					e = c.Volatile().To("room")
				case "Volatile.Except":
					e = c.Volatile().Except(excluded)
				case "Volatile.Local":
					e = c.Volatile().Local()
				}
				done <- e.Emit("news")
			}}})
			excluded = peers[2].nc.ID()
			b.Join("room", peers[0].nc)
			b.Join("room", peers[1].nc)
			peers[0].send(t, `2["go"]`)
			require.NoError(t, recv(t, done, "handler broadcast"))
			for i, p := range peers {
				gets := i == 1 || mode == "Broadcast" && i == 2
				if mode == "Volatile.Local" {
					gets = i == 0
				}
				if gets {
					expectBeforeBarrier(t, p, "2[\"news\"]\n")
				} else {
					expectBeforeBarrier(t, p)
				}
			}
		})
	}

}

// Covers S15
func TestNamespaceSocketIDs(t *testing.T) {
	b, peers := broadcastPeers(t, hooks{})
	n := peers[0].srv.Of("/")
	peers[0].srv.getNamespace("/").broadcast = b
	b.Join("room", peers[1].nc)
	b.Join("room", peers[2].nc)
	require.ElementsMatch(t, []string{peers[0].nc.ID(), peers[1].nc.ID(), peers[2].nc.ID()}, n.SocketIDs())
	require.ElementsMatch(t, []string{peers[1].nc.ID(), peers[2].nc.ID()}, n.SocketIDs("room"))
	ids := n.SocketIDs("room")
	ids[0] = "changed"
	require.NotContains(t, n.SocketIDs("room"), "changed")
}

// Covers N3
func TestServerNamespaceMethods(t *testing.T) {
	connected := make(chan Conn, 2)
	var srv *Server
	ts := newTestServer(t, func(s *Server) {
		srv = s
		s.OnConnect("/", func(Conn) error { return nil })
		s.Of("/chat").OnConnect(func(c Conn) error { c.Join("room"); connected <- c; return nil })
	})
	clients := []*rawClient{dialRaw(t, ts.URL), dialRaw(t, ts.URL)}
	var ids []string
	for _, c := range clients {
		require.Equal(t, "0", c.read())
		c.send("0/chat,")
		require.Equal(t, "0/chat,", c.read())
		ids = append(ids, recv(t, connected, "chat socket").ID())
	}
	n := srv.Of("/chat")
	require.ElementsMatch(t, ids, n.SocketIDs("room"))
	require.Equal(t, 2, srv.RoomLen("/chat", "room"))
	for _, step := range []struct {
		emit func()
		want string
	}{
		{func() { require.NoError(t, n.To("room").Emit("event")) }, "2/chat,[\"event\"]\n"},
		{func() { require.True(t, srv.BroadcastToRoom("/chat", "room", "event")) }, "2/chat,[\"event\"]\n"},
		{func() { require.NoError(t, n.Emit("event")) }, "2/chat,[\"event\"]\n"},
		{func() { require.True(t, srv.BroadcastToNamespace("/chat", "event")) }, "2/chat,[\"event\"]\n"},
		{func() { require.NoError(t, n.Send("hello")) }, "2/chat,[\"message\",\"hello\"]\n"},
	} {
		step.emit()
		for _, c := range clients {
			require.Equal(t, step.want, c.read())
		}
	}
}

// Covers S13
func TestRoomUnion(t *testing.T) {
	b, peers := broadcastPeers(t, hooks{})
	peers[0].srv.getNamespace("/").broadcast = b
	b.Join("one", peers[1].nc)
	b.Join("two", peers[1].nc)
	b.Join("two", peers[2].nc)
	base := peers[0].srv.Of("/").To("one")
	union := base.To("two")
	require.NoError(t, union.Emit("union"))
	expectBeforeBarrier(t, peers[0])
	expectBeforeBarrier(t, peers[1], "2[\"union\"]\n")
	expectBeforeBarrier(t, peers[2], "2[\"union\"]\n")
	require.NoError(t, base.Emit("original"))
	expectBeforeBarrier(t, peers[1], "2[\"original\"]\n")
	expectBeforeBarrier(t, peers[2])
	// Appending to one descendant must not change a sibling or the reusable base.
	left, right := base.Except(peers[1].nc.ID()), base.Except(peers[2].nc.ID())
	require.NoError(t, left.Emit("excluded"))
	require.NoError(t, right.Emit("kept"))
	expectBeforeBarrier(t, peers[1], "2[\"kept\"]\n")
}

// Covers N7
func TestBroadcastCallback(t *testing.T) {
	b, peers := broadcastPeers(t, hooks{})
	peers[0].srv.getNamespace("/").broadcast = b
	n := peers[0].srv.Of("/")
	var callbacks atomic.Int32
	callback := func() { callbacks.Add(1) }
	require.ErrorIs(t, n.Emit("ack", callback), ErrBroadcastAck)
	require.ErrorIs(t, n.Send(callback), ErrBroadcastAck)
	require.ErrorIs(t, n.To("room").Emit("ack", callback), ErrBroadcastAck)
	require.ErrorIs(t, peers[0].nc.Broadcast().Send(callback), ErrBroadcastAck)
	for _, p := range peers {
		expectBeforeBarrier(t, p)
		b.Join("room", p.nc)
	}
	for index, send := range []func(){
		func() { peers[0].srv.BroadcastToRoom("/", "room", "ack", callback) },
		func() { peers[0].srv.BroadcastToNamespace("/", "ack", callback) },
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

// Covers S21
func TestServerBroadcastFlags(t *testing.T) {
	s := NewServer(nil)
	defer func() { _ = s.Close() }()
	n := s.Of("/chat")
	b := s.getNamespace("/chat").broadcast
	a, c := flagSocket("a", true), flagSocket("c", false)
	b.Join("room", a)
	b.Join("room", c)
	require.NoError(t, n.Volatile().Local().Emit("flags"))
	expectEvent(t, a.redisTestConn, "flags []")
	require.Empty(t, c.events)
	require.Equal(t, 1, a.volatile)
	require.Equal(t, 1, c.volatile)
	require.NoError(t, n.Local().Emit("local"))
	expectEvent(t, a.redisTestConn, "local []")
	expectEvent(t, c.redisTestConn, "local []")
}

// Covers K16
func TestVolatileEmit(t *testing.T) {
	connected := make(chan Conn, 2)
	ts := newTestServer(t, func(s *Server) { s.OnConnect("/", func(c Conn) error { connected <- c; return nil }) })
	get := func(target string) string {
		t.Helper()
		response, err := http.Get(target)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		require.Equal(t, http.StatusOK, response.StatusCode)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		return string(body)
	}
	body := get(ts.URL + "/?EIO=3&transport=polling&b64=1")
	var params struct {
		SID string `json:"sid"`
	}
	require.NoError(t, json.Unmarshal([]byte(body[strings.Index(body, "{"):]), &params))
	poll := recv(t, connected, "polling socket")
	target := ts.URL + "/?EIO=3&transport=polling&b64=1&sid=" + params.SID
	require.Equal(t, "2:40", get(target))
	require.Eventually(t, func() bool { return !poll.(*namespaceConn).Conn.(interface{ Writable() bool }).Writable() }, time.Second, time.Millisecond)
	require.NoError(t, poll.Volatile().Emit("dropped"))
	poll.Emit("kept")
	require.Equal(t, "11:42[\"kept\"]\n", get(target))
	w, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	defer func() { _ = w.Close() }()
	_, _, err = w.ReadMessage()
	require.NoError(t, err)
	_, packet, err := w.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "40", string(packet))
	ws := recv(t, connected, "websocket socket")
	require.Eventually(t, func() bool { return ws.(*namespaceConn).Conn.(interface{ Writable() bool }).Writable() }, time.Second, time.Millisecond)
	require.NoError(t, ws.Volatile().Emit("delivered"))
	require.NoError(t, w.SetReadDeadline(time.Now().Add(waitFor)))
	_, packet, err = w.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "42[\"delivered\"]\n", string(packet))
}

func isolatedRedis(t *testing.T) (*redisBroadcast, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	pool := &redis.Pool{Dial: func() (redis.Conn, error) { calls.Add(1); return nil, errors.New("unexpected redis I/O") }}
	t.Cleanup(func() { _ = pool.Close() })
	return &redisBroadcast{nsp: "/", uid: "local", rooms: make(map[string]map[string]Conn), pub: pool}, &calls
}

// Covers A4
func TestRedisBroadcastExceptUnsupported(t *testing.T) {
	b, calls := isolatedRedis(t)
	c := newRedisTestConn("c")
	b.Join("r", c)
	for _, local := range []bool{false, true} {
		err := b.SendTo(BroadcastOptions{Except: []string{"other"}, Local: local}, "nothing")
		require.ErrorIs(t, err, ErrExceptUnsupported)
		require.NotEqual(t, ErrExceptUnsupported, err)
	}
	require.Zero(t, calls.Load())
	require.Empty(t, c.events)
}

func TestRedisBroadcastDropsUnknownOpts(t *testing.T) {
	b, calls := isolatedRedis(t)
	c := newRedisTestConn("c")
	b.Join("r", c)
	require.Error(t, b.onMessage("socket.io#/#remote", []byte(`{"opts":["r","news",{"except":["c"]}],"args":[]}`)))
	require.Empty(t, c.events)
	require.Zero(t, calls.Load())
	require.NoError(t, b.onMessage("socket.io#/#remote", []byte(`{"opts":["r","news"],"args":[]}`)))
	expectEvent(t, c, "news []")
}

func TestRedisBroadcastDropsUnknownKeys(t *testing.T) {
	b, _ := isolatedRedis(t)
	c := newRedisTestConn("c")
	b.Join("r", c)
	require.Error(t, b.onMessage("socket.io#/#remote", []byte(`{"opts":["r","news"],"args":[],"except":["c"]}`)))
	require.Empty(t, c.events)
}

func TestRedisBroadcastLocalFlag(t *testing.T) {
	b, calls := isolatedRedis(t)
	c := newRedisTestConn("c")
	b.Join("r", c)
	b.Join("s", c)
	require.NoError(t, b.SendTo(BroadcastOptions{Rooms: []string{"r", "s"}, Local: true}, "local"))
	expectEvent(t, c, "local []")
	require.Empty(t, c.events)
	require.Zero(t, calls.Load())
	require.Equal(t, []string{"c"}, b.IDs("r", "s"))
}

func TestRedisBroadcastVolatileFlag(t *testing.T) {
	b, calls := isolatedRedis(t)
	a, c := flagSocket("a", true), flagSocket("c", false)
	b.Join("r", a)
	b.Join("r", c)
	require.NoError(t, b.SendTo(BroadcastOptions{Volatile: true}, "volatile"))
	expectEvent(t, a.redisTestConn, "volatile []")
	require.Empty(t, c.events)
	require.Equal(t, 1, a.volatile)
	require.Equal(t, 1, c.volatile)
	require.Equal(t, int32(1), calls.Load())
}

func TestVolatileDropsQueuedPacket(t *testing.T) {
	p := start(t, 'S', hooks{})
	p.fc.hold.Store(true)
	p.nc.Emit("blocked")
	recv(t, p.fc.held, "held writer")
	p.nc.Emit("queued")
	require.NoError(t, p.nc.Volatile().Emit("dropped", func() {}))
	require.Zero(t, ackEntries(p.nc), "dropped volatile emit registers no ACK")
	p.fc.hold.Store(false)
	p.fc.release <- struct{}{}
	expectBeforeBarrier(t, p, "2[\"blocked\"]\n", "2[\"queued\"]\n")
	require.False(t, isDone(p.conn().closing))
}

func ackEntries(c Conn) int {
	count := 0
	c.(*namespaceConn).ack.Range(func(_, _ interface{}) bool { count++; return true })
	return count
}

type volatileBarrierConn struct {
	*fakeConn
	entered chan struct{}
	resume  chan struct{}
}

func (c *volatileBarrierConn) Writable() bool {
	c.entered <- struct{}{}
	<-c.resume
	return true
}

func TestVolatileConcurrentAdmission(t *testing.T) {
	for _, mode := range []string{"normal competitor", "volatile competitor"} {
		t.Run(mode, func(t *testing.T) {
			p := newPeer(t, 'S', hooks{setup: func(s *Server) { s.limits.writeBufferSize = 1 }})
			gate := &volatileBarrierConn{fakeConn: p.fc, entered: make(chan struct{}, 2), resume: make(chan struct{})}
			p.srv.serveConn(gate)
			p.nc = recv(t, p.conns, "root connect")
			require.Equal(t, "0", recv(t, p.fc.out, "CONNECT"))
			closeAtEnd(t, p.fc, p.conn())
			p.fc.hold.Store(true)
			p.nc.Emit("inflight")
			recv(t, p.fc.held, "held writer")
			done := make(chan error, 2)
			emit := func() { done <- p.nc.Volatile().Emit("volatile", func() {}) }
			go emit()
			recv(t, gate.entered, "first volatile readiness check")
			if mode == "normal competitor" {
				p.nc.Emit("normal")
			} else {
				go emit()
				recv(t, gate.entered, "second volatile readiness check")
			}
			close(gate.resume)
			require.NoError(t, recv(t, done, "volatile emit"))
			wantACKs := 0
			if mode == "volatile competitor" {
				require.NoError(t, recv(t, done, "second volatile emit"))
				wantACKs = 1
			}
			require.False(t, isDone(p.conn().closing), "a losing volatile packet must drop without overflow")
			require.Equal(t, wantACKs, ackEntries(p.nc), "only an admitted packet registers an ACK")
			p.fc.hold.Store(false)
			p.fc.release <- struct{}{}
			require.Equal(t, "2[\"inflight\"]\n", recv(t, p.fc.out, "inflight packet"))
			want := "2[\"normal\"]\n"
			if mode == "volatile competitor" {
				want = "21[\"volatile\"]\n"
			}
			require.Equal(t, want, recv(t, p.fc.out, "admitted packet"))
			expectBeforeBarrier(t, p)
			require.Empty(t, p.errs)
		})
	}
}

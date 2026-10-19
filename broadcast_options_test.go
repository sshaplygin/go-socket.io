package socketio

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/adapter"
)

// flagTarget records whether an adapter used the public single-socket Emitter.
type flagTarget struct {
	*redisTestConn
	writable bool
	volatile int
}

func (c *flagTarget) EmitWithOptions(opts adapter.Options, event string, args ...interface{}) {
	if opts.Volatile {
		c.volatile++
		if !c.writable {
			return
		}
	}
	c.Emit(event, args...)
}

func flagSocket(id string, writable bool) *flagTarget {
	return &flagTarget{redisTestConn: newRedisTestConn(id), writable: writable}
}

type exceptAdapter struct{ adapter.Adapter }

func (exceptAdapter) SendTo(adapter.Options, string, ...interface{}) error {
	return errors.Join(errors.New("adapter refused"), adapter.ErrExceptUnsupported)
}

func TestConnJoinRooms(t *testing.T) {
	p := start(t, 'S', hooks{})
	p.nc.Join("one", "two", "one", "three")
	require.ElementsMatch(t, []string{p.nc.ID(), "one", "two", "three"}, p.nc.Rooms())
	for _, room := range []string{"one", "two", "three"} {
		require.Equal(t, 1, p.srv.RoomLen("/", room))
	}
}

func broadcastPeers(t *testing.T, h hooks) (*adapter.Memory, []*peer) {
	t.Helper()
	b := adapter.NewMemory()
	previousSetup := h.setup
	h.setup = func(s *Server) {
		if previousSetup != nil {
			previousSetup(s)
		}
		s.SetAdapter(func(string) (adapter.Adapter, error) { return b, nil })
	}
	var peers []*peer
	for i := 0; i < 3; i++ {
		p := start(t, 'S', h)
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

func TestNamespaceSocketIDs(t *testing.T) {
	b, peers := broadcastPeers(t, hooks{})
	n := peers[0].srv.Of("/")
	b.Join("room", peers[1].nc)
	b.Join("room", peers[2].nc)
	require.ElementsMatch(t, []string{peers[0].nc.ID(), peers[1].nc.ID(), peers[2].nc.ID()}, n.SocketIDs())
	require.ElementsMatch(t, []string{peers[1].nc.ID(), peers[2].nc.ID()}, n.SocketIDs("room"))
	ids := n.SocketIDs("room")
	ids[0] = "changed"
	require.NotContains(t, n.SocketIDs("room"), "changed")
}

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

func TestRoomUnion(t *testing.T) {
	b, peers := broadcastPeers(t, hooks{})
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

func TestBroadcastCallback(t *testing.T) {
	b, peers := broadcastPeers(t, hooks{})
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
			require.NotZero(t, id, "each callback packet has an acknowledgement ID")
			p.send(t, "3"+idText+"[]")
		}
		require.Eventually(t, func() bool { return callbacks.Load() == int32((index+1)*len(peers)) }, time.Second, time.Millisecond)
	}
}

func TestServerBroadcastFlags(t *testing.T) {
	s := NewServer()
	defer func() { _ = s.Close() }()
	n := s.Of("/chat")
	b := s.getNamespace("/chat").Adapter()
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

func TestVolatileEmit(t *testing.T) {
	connected := make(chan Conn, 2)
	// Hold namespace admission so the OPEN response is isolated from CONNECT.
	// Polling otherwise correctly batches both packets into the initial response.
	admitted := make(chan struct{})
	release := sync.OnceFunc(func() { close(admitted) })
	defer release()
	ts := newTestServer(t, func(s *Server) { s.OnConnect("/", func(c Conn) error { connected <- c; <-admitted; return nil }) })
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
	release()
	target := ts.URL + "/?EIO=3&transport=polling&b64=1&sid=" + params.SID
	require.Equal(t, "2:40", get(target))
	require.Eventually(t, func() bool { return !poll.EngineConn().(interface{ Writable() bool }).Writable() }, time.Second, time.Millisecond)
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
	require.Eventually(t, func() bool { return ws.EngineConn().(interface{ Writable() bool }).Writable() }, time.Second, time.Millisecond)
	require.NoError(t, ws.Volatile().Emit("delivered"))
	require.NoError(t, w.SetReadDeadline(time.Now().Add(waitFor)))
	_, packet, err = w.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "42[\"delivered\"]\n", string(packet))
}

func TestVolatileDropsQueuedPacket(t *testing.T) {
	p := start(t, 'S', hooks{})
	p.fc.hold.Store(true)
	p.nc.Emit("blocked")
	recv(t, p.fc.held, "held writer")
	p.nc.Emit("queued")
	require.NoError(t, p.nc.Volatile().Emit("dropped", func() {}))
	// Exact ACK-map non-admission is retained by the owner-local regression.
	p.fc.hold.Store(false)
	p.fc.release <- struct{}{}
	expectBeforeBarrier(t, p, "2[\"blocked\"]\n", "2[\"queued\"]\n")
	assertNextAckID(t, p, 1)
	require.False(t, !p.nc.Connected())
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
			closeAtEnd(t, p.fc, p.completion())
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
			require.False(t, !p.nc.Connected(), "a losing volatile packet must drop without overflow")
			// Exact ACK-map admission is retained by the owner-local regression.
			p.fc.hold.Store(false)
			p.fc.release <- struct{}{}
			require.Equal(t, "2[\"inflight\"]\n", recv(t, p.fc.out, "inflight packet"))
			want := "2[\"normal\"]\n"
			if mode == "volatile competitor" {
				want = "21[\"volatile\"]\n"
			}
			require.Equal(t, want, recv(t, p.fc.out, "admitted packet"))
			expectBeforeBarrier(t, p)
			assertNextAckID(t, p, uint64(wantACKs+1))
			require.Empty(t, p.errs)
		})
	}
}

func TestBroadcastAdapterError(t *testing.T) {
	p := start(t, 'S', hooks{setup: func(s *Server) {
		s.SetAdapter(func(string) (adapter.Adapter, error) { return exceptAdapter{Adapter: unavailableAdapter{}}, nil })
	}})
	require.ErrorIs(t, p.nc.Broadcast().Emit("nothing"), adapter.ErrExceptUnsupported)
	require.Empty(t, p.fc.out)
}

func assertNextAckID(t *testing.T, p *peer, id uint64) {
	ack := make(chan struct{}, 1)
	p.nc.Emit("verify ACK", func() { ack <- struct{}{} })
	require.Equal(t, fmt.Sprintf("2%d[\"verify ACK\"]\n", id), recv(t, p.fc.out, "next acknowledgement ID"))
	p.send(t, fmt.Sprintf("3%d[]", id))
	recv(t, ack, "registered acknowledgement callback")
}

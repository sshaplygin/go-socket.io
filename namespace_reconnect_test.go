package socketio

import (
	"errors"
	"github.com/sshaplygin/go-socket.io/adapter"
	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type reentrantRoomAdapter struct {
	*adapter.Memory
	t         *testing.T
	onJoin    func(Conn)
	returned  chan struct{}
	automatic bool
}

func (a *reentrantRoomAdapter) Join(room string, socket adapter.Socket) {
	a.Memory.Join(room, socket)
	if !a.automatic && room != "trigger" {
		return
	}
	// Bound a regression so the adapter eventually releases its caller and the
	// blocked callback can finish, rather than hanging the test's cleanup.
	go func() {
		defer close(a.returned)
		a.onJoin(socket.(Conn))
	}()
	select {
	case <-a.returned:
	case <-time.After(time.Second):
		a.t.Error("adapter Join callback blocked on its socket")
	}
}

func TestAdapterJoinCanReenterSocket(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		for _, operation := range []string{"Rooms", "Emit", "Disconnect", "Close"} {
			t.Run(map[bool]string{false: "explicit", true: "automatic"}[automatic]+"/"+operation, func(t *testing.T) {
				observed := make(chan []string, 1)
				closed := make(chan error, 1)
				b := &reentrantRoomAdapter{Memory: adapter.NewMemory(), t: t, returned: make(chan struct{}), automatic: automatic}
				b.onJoin = func(c Conn) {
					if operation != "Rooms" {
						c.Emit("joined")
					}
					switch operation {
					case "Rooms":
						observed <- c.Rooms()
					case "Disconnect":
						c.Disconnect()
					case "Close":
						closed <- c.Close()
					}
				}
				p := newPeer(t, 'S', hooks{
					setup: func(s *Server) { s.SetAdapter(func(string) (adapter.Adapter, error) { return b, nil }) },
					disconnect: func(c Conn) {
						if operation == "Close" {
							c.Emit("goodbye")
						}
					},
				})
				p.srv.serveConn(p.fc)
				p.nc = recv(t, p.conns, "root OnConnect")
				closeAtEnd(t, p.fc, p.completion())
				require.Equal(t, "0", recv(t, p.fc.out, "CONNECT before adapter emissions"))
				if !automatic {
					p.nc.Join("trigger")
				}
				recv(t, b.returned, "adapter callback return")
				if operation == "Rooms" {
					want := []string{p.nc.ID()}
					if !automatic {
						want = append(want, "trigger")
					}
					require.ElementsMatch(t, want, recv(t, observed, "rooms inside adapter"))
					return
				}
				require.Equal(t, ev("joined"), recv(t, p.fc.out, "adapter emission after CONNECT"))
				if operation == "Emit" {
					return
				}
				if operation == "Close" {
					require.NoError(t, recv(t, closed, "reentrant Close"))
					p.disconnected(t, "/")
				} else {
					require.Equal(t, "/", recv(t, p.discs, "namespace disconnect callback"))
				}
				require.Equal(t, "1", recv(t, p.fc.out, "DISCONNECT after adapter emission"))
				if operation == "Close" {
					require.Equal(t, ev("goodbye"), recv(t, p.fc.out, "graceful callback emit"))
				}
				require.Empty(t, b.AllRooms(), "retirement removes the joined room")
			})
		}
	}
}

func TestOldDisconnectPreservesNewRooms(t *testing.T) {
	connected := make(chan Conn, 2)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	releaseOld := sync.OnceFunc(func() { close(release) })
	defer releaseOld()
	s, url := defectServer(t, func(s *Server) {
		s.OnEvent("/", "alive", func(Conn) string { return "alive" })
		s.OnConnect("/chat", func(c Conn) error { connected <- c; return nil })
		s.Of("/chat").On("disconnecting", func(Conn, string) { entered <- struct{}{}; <-release })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	old := recv(t, connected, "old namespace")
	done := make(chan struct{})
	go func() { old.Disconnect(); close(done) }()
	recv(t, entered, "old disconnecting callback")
	require.Equal(t, "1/chat,", c.read())
	c.send("0/chat,")
	c.send(`27["alive"]`)
	require.Equal(t, "37[\"alive\"]\n", c.read(), "retiring namespace does not stall the reader")
	releaseOld()
	recv(t, done, "old disconnect completed")
	require.Equal(t, "0/chat,", c.read())
	next := recv(t, connected, "new namespace")
	require.Equal(t, 1, s.RoomLen("/chat", next.ID()), "old LeaveAll removed newly accepted socket's ID room")
}

func TestRetainedServerConnCannotAffectReplacement(t *testing.T) {
	connected := make(chan Conn, 2)
	s, url := defectServer(t, func(s *Server) {
		s.OnConnect("/chat", func(c Conn) error { connected <- c; return nil })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	old := recv(t, connected, "old namespace")
	old.Disconnect()
	require.Equal(t, "1/chat,", c.read())
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	next := recv(t, connected, "replacement namespace")
	require.Equal(t, old.ID(), next.ID(), "namespace reconnect reuses the wire ID")
	require.False(t, old.Connected())
	next.Join("live")
	old.Leave("live")
	require.Equal(t, 1, s.RoomLen("/chat", "live"))
	old.LeaveAll()
	require.ElementsMatch(t, []string{next.ID(), "live"}, next.Rooms())
	old.Join("stale")
	require.Empty(t, old.Rooms())
	require.Zero(t, s.RoomLen("/chat", "stale"))
	old.Emit("stale")
	old.Send("stale")
	require.NoError(t, old.Compress(false).Emit("stale"))
	require.NoError(t, old.Volatile().Emit("stale"))
	next.Emit("barrier")
	require.Equal(t, "2/chat,[\"barrier\"]\n", c.read(), "retained sockets cannot enqueue for their replacement")
}

func TestRetiredNamespaceReconnectSerializesAdmission(t *testing.T) {
	for _, reject := range []bool{false, true} {
		name := "accepted"
		if reject {
			name = "rejected"
		}
		t.Run(name, func(t *testing.T) {
			connected := make(chan Conn, 1)
			disconnecting, admitting := make(chan struct{}), make(chan struct{})
			releaseOld, releaseNew := make(chan struct{}), make(chan struct{})
			finishOld := sync.OnceFunc(func() { close(releaseOld) })
			finishNew := sync.OnceFunc(func() { close(releaseNew) })
			defer finishOld()
			defer finishNew()
			var admissions atomic.Int32
			events := make(chan struct{}, 2)
			_, url := defectServer(t, func(s *Server) {
				s.OnEvent("/", "barrier", func(Conn) string { return "barrier" })
				s.OnConnect("/chat", func(c Conn) error {
					if admissions.Add(1) == 1 {
						connected <- c
						return nil
					}
					close(admitting)
					<-releaseNew
					if reject {
						return errors.New("replacement rejected")
					}
					return nil
				})
				s.Of("/chat").On("disconnecting", func(Conn, string) {
					if admissions.Load() == 1 {
						close(disconnecting)
						<-releaseOld
					}
				})
				s.OnEvent("/chat", "probe", func(_ Conn, value *parser.Buffer) string { events <- struct{}{}; return string(value.Data) })
			})
			wire, err := (&engineclient.Dialer{Transports: []transport.Transport{websocket.Default}}).Dial(url, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = wire.Close() })
			c := &rawClient{t: t, conn: wire}
			require.Equal(t, "0", c.read())
			c.send("0/chat,")
			require.Equal(t, "0/chat,", c.read())
			old := recv(t, connected, "initial namespace")
			done := make(chan struct{})
			go func() { old.Disconnect(); close(done) }()
			recv(t, disconnecting, "old cleanup entered")
			require.Equal(t, "1/chat,", c.read())
			c.send("0/chat,")
			c.send(`27["barrier"]`)
			require.Equal(t, "37[\"barrier\"]\n", c.read(), "old cleanup must not block the reader")
			finishOld()
			recv(t, admitting, "replacement admission")
			c.send(`51-/chat,8["probe",{"_placeholder":true,"num":0}]`)
			attachment, err := c.conn.NextWriter(eioparser.Binary)
			require.NoError(t, err)
			_, err = attachment.Write([]byte("probe"))
			require.NoError(t, err)
			require.NoError(t, attachment.Close())
			c.send(`29["barrier"]`)
			select {
			case <-events:
				t.Error("event ran while replacement OnConnect was blocked")
			case <-time.After(100 * time.Millisecond):
			}
			finishNew()
			recv(t, done, "old disconnect returned")
			if reject {
				require.Equal(t, "4/chat,\"replacement rejected\"\n", c.read())
			} else {
				require.Equal(t, "0/chat,", c.read())
				require.Equal(t, "3/chat,8[\"probe\"]\n", c.read())
			}
			require.Equal(t, "39[\"barrier\"]\n", c.read())
			if reject {
				require.Empty(t, events, "rejected namespace must never dispatch the waiting event")
			} else {
				recv(t, events, "event after admission")
			}
		})
	}
}

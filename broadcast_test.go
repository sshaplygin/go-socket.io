package socketio

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// emitConn is a Conn for broadcast tests: ID and Emit only. Emit calls onEmit.
type emitConn struct {
	Conn
	id     string
	onEmit func(event string)
}

func (c *emitConn) ID() string { return c.id }

func (c *emitConn) Emit(event string, _ ...interface{}) { c.onEmit(event) }

// TestBroadcastDoesNotHoldLockWhileEmitting checks that a recipient whose Emit
// blocks does not stop other connections from joining or leaving, as
// conn.Close does with LeaveAll, and that one whose Emit leaves all rooms does
// not deadlock Send or SendAll.
func TestBroadcastDoesNotHoldLockWhileEmitting(t *testing.T) {
	bc := newBroadcast()
	entered, release := make(chan struct{}), make(chan struct{})
	slow := &emitConn{id: "slow", onEmit: func(string) {
		entered <- struct{}{}
		<-release
	}}
	other := &emitConn{id: "other", onEmit: func(string) {}}
	bc.Join("r", slow)

	sent := inBackground(func() { bc.Send("r", "msg") })
	recv(t, entered, "Emit on the slow member")
	recv(t, inBackground(func() {
		bc.Join("r", other)
		bc.LeaveAll(other)
	}), "Join and LeaveAll during a blocked Emit")
	close(release)
	recv(t, sent, "Send")
	bc.LeaveAll(slow)

	var events []string
	leaver := &emitConn{id: "leaver"}
	leaver.onEmit = func(event string) {
		events = append(events, event)
		bc.LeaveAll(leaver)
	}
	for _, send := range []func(){
		func() { bc.Send("a", "send") },
		func() { bc.SendAll("send-all") },
	} {
		bc.Join("a", leaver)
		recv(t, inBackground(send), "a broadcast whose recipient leaves")
	}
	require.Equal(t, []string{"send", "send-all"}, events)
}

// TestBroadcastForEachCallbackChangesRooms checks that the ForEach callback
// may join and leave rooms, and that it visits the members present when
// ForEach started.
func TestBroadcastForEachCallbackChangesRooms(t *testing.T) {
	bc := newBroadcast()
	a := &emitConn{id: "a"}
	b := &emitConn{id: "b"}
	bc.Join("r", a)
	bc.Join("r", b)

	var visited []string
	recv(t, inBackground(func() {
		bc.ForEach("r", func(c Conn) {
			visited = append(visited, c.ID())
			bc.Leave("r", c)
			bc.Join("moved", c)
		})
	}), "ForEach whose callback changes rooms")

	require.ElementsMatch(t, []string{"a", "b"}, visited)
	require.Equal(t, 0, bc.Len("r"))
	require.Equal(t, 2, bc.Len("moved"))
}

// TestBroadcastSendAllOneCopyPerRoom pins that SendAll emits once per room
// membership, which TestLifecycleRootNamespace relies on.
func TestBroadcastSendAllOneCopyPerRoom(t *testing.T) {
	bc := newBroadcast()
	var events []string
	c := &emitConn{id: "c", onEmit: func(event string) { events = append(events, event) }}
	bc.Join("r1", c)
	bc.Join("r2", c)

	bc.SendAll("msg")
	require.Equal(t, []string{"msg", "msg"}, events)
}

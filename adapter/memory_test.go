package adapter

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// recordingSocket is a Socket that records the events emitted to it. The embedded
// Socket is nil; methods used for delivery are implemented below.
type recordingSocket struct {
	Socket
	id     string
	events chan string
	onEmit func()
}

func newRecordingSocket(id string) *recordingSocket {
	return &recordingSocket{id: id, events: make(chan string, 64)}
}

func (c *recordingSocket) ID() string { return c.id }

func (c *recordingSocket) Emit(event string, v ...interface{}) {
	if c.onEmit != nil {
		c.onEmit()
	}
	c.events <- fmt.Sprintf("%s %v", event, v)
}

func expectEvent(t *testing.T, c *recordingSocket, want string) {
	t.Helper()
	select {
	case got := <-c.events:
		require.Equal(t, want, got)
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: %q not delivered", c.id, want)
	}
}
func (c *recordingSocket) EmitWithOptions(_ Options, event string, args ...interface{}) {
	c.Emit(event, args...)
}

// flagTarget records whether an adapter used the public single-socket Emitter.
type flagTarget struct {
	*recordingSocket
	writable bool
	volatile int
}

func (c *flagTarget) EmitWithOptions(opts Options, event string, args ...interface{}) {
	if opts.Volatile {
		c.volatile++
		if !c.writable {
			return
		}
	}
	c.Emit(event, args...)
}

func flagSocket(id string, writable bool) *flagTarget {
	return &flagTarget{recordingSocket: newRecordingSocket(id), writable: writable}
}

func TestBroadcastSendTo(t *testing.T) {
	b := NewMemory()
	a, c, d := flagSocket("a", true), flagSocket("c", false), flagSocket("d", true)
	b.Join("one", a)
	b.Join("two", a)
	b.Join("two", c)
	b.Join("two", d)
	require.NoError(t, b.SendTo(Options{Rooms: []string{"one", "two", "one"}, Except: []string{"d"}, Volatile: true, Local: true}, "news", 7))
	expectEvent(t, a.recordingSocket, "news [7]")
	require.Empty(t, a.events)
	require.Empty(t, c.events)
	require.Empty(t, d.events)
	require.Equal(t, 1, a.volatile)
	require.Equal(t, 1, c.volatile)
	require.Zero(t, d.volatile)
	// Delivery releases the adapter lock before invoking application code.
	a.onEmit = func() { b.LeaveAll(a) }
	require.NoError(t, b.SendTo(Options{Rooms: []string{"one"}}, "leave"))
	require.Empty(t, b.Rooms(a))

}

func TestBroadcastIDs(t *testing.T) {
	b := NewMemory()
	a, c := newRecordingSocket("a"), newRecordingSocket("c")
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

func TestBroadcastRoomsMaps(t *testing.T) {
	b := NewMemory()
	a, c := newRecordingSocket("a"), newRecordingSocket("c")
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

// preparingSocket shares a packet prepared by one socket of its concrete type.
type preparingSocket struct {
	*recordingSocket
	prepares *int
	err      error
	decline  bool
}

func (c preparingSocket) PrepareEmit(opts Options, event string, args ...interface{}) (interface{}, error) {
	*c.prepares++
	return fmt.Sprintf("%s %v volatile=%t", event, args, opts.Volatile), c.err
}

func (c preparingSocket) EmitPrepared(_ Options, packet interface{}) bool {
	if c.decline {
		return false
	}
	c.events <- packet.(string)
	return true
}

// failingSocket is a second implementation whose packets must not mix with preparingSocket's.
type failingSocket struct{ preparingSocket }

// decliningSocket prepares successfully but refuses to deliver the packet.
type decliningSocket struct{ preparingSocket }

func TestBroadcastPreparesOncePerImplementation(t *testing.T) {
	b := NewMemory()
	var prepared, failed, declined int
	errPrepare := errors.New("prepare failed")
	var shared []*recordingSocket
	for _, id := range []string{"a", "b", "c"} {
		c := preparingSocket{recordingSocket: newRecordingSocket(id), prepares: &prepared}
		b.Join("room", c)
		shared = append(shared, c.recordingSocket)
	}
	var fallback []*recordingSocket
	for _, id := range []string{"d", "e"} {
		c := failingSocket{preparingSocket{recordingSocket: newRecordingSocket(id), prepares: &failed, err: errPrepare}}
		b.Join("room", c)
		fallback = append(fallback, c.recordingSocket)
	}
	c := decliningSocket{preparingSocket{recordingSocket: newRecordingSocket("g"), prepares: &declined, decline: true}}
	b.Join("room", c)
	fallback = append(fallback, c.recordingSocket)
	plain := newRecordingSocket("f")
	b.Join("room", plain)

	require.NoError(t, b.SendTo(Options{Rooms: []string{"room"}, Volatile: true}, "news", 7))
	for _, c := range shared {
		expectEvent(t, c, "news [7] volatile=true")
	}
	// A failed preparation is not retried per recipient, and a declined packet
	// is not lost: each such recipient uses its own emit.
	for _, c := range append(fallback, plain) {
		expectEvent(t, c, "news [7]")
	}
	require.Equal(t, 1, prepared)
	require.Equal(t, 1, failed)
	require.Equal(t, 1, declined)
	for _, c := range append(append(shared, fallback...), plain) {
		require.Empty(t, c.events)
	}
}

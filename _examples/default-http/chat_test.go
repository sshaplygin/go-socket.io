package main

import (
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
)

type chatEvent struct {
	name string
	data map[string]interface{}
}

type chatClient struct {
	*socketio.Client
	events chan chatEvent
}

// newChatServer serves the chat the way main does, without the page.
func newChatServer(t *testing.T) (*socketio.Server, *httptest.Server) {
	t.Helper()

	// The Go client sends no close packet, so the server detects a closed client only when its
	// pings stop; keep the timeouts short.
	server := socketio.NewServer(&engineio.Options{PingInterval: 200 * time.Millisecond, PingTimeout: 2 * time.Second})
	registerChat(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve()
	}()

	ts := httptest.NewServer(server)
	t.Cleanup(func() {
		_ = server.Close()
		<-done
		ts.CloseClientConnections() // the long polls of closed clients are still open
		ts.Close()
	})

	return server, ts
}

// connectChat connects a client that records every chat event it receives.
func connectChat(t *testing.T, url string) *chatClient {
	t.Helper()

	cl, err := socketio.NewClient(url, nil)
	if err != nil {
		t.Fatal(err)
	}

	c := &chatClient{Client: cl, events: make(chan chatEvent, 64)}
	for _, name := range []string{"login", "user joined", "user left", "new message", "typing", "stop typing"} {
		name := name
		cl.OnEvent(name, func(_ socketio.Conn, data map[string]interface{}) {
			c.events <- chatEvent{name: name, data: data}
		})
	}

	if err := cl.Connect(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cl.Close() })

	return c
}

// expect fails the test unless the client's next event is the named one and its payload
// has the given key and value pairs.
func (c *chatClient) expect(t *testing.T, name string, kv ...interface{}) {
	t.Helper()

	select {
	case ev := <-c.events:
		if ev.name != name {
			t.Fatalf("got event %q %v, want %q", ev.name, ev.data, name)
		}
		for i := 0; i < len(kv); i += 2 {
			if ev.data[kv[i].(string)] != kv[i+1] {
				t.Fatalf("%q payload %v: %v is not %v", name, ev.data, kv[i], kv[i+1])
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no %q event within 5s", name)
	}
}

// login returns the payload of the next "login" event, skipping the other users' joins.
func (c *chatClient) login(t *testing.T) map[string]interface{} {
	t.Helper()

	for {
		select {
		case ev := <-c.events:
			if ev.name == "login" {
				return ev.data
			}
		case <-time.After(5 * time.Second):
			t.Error("no login event within 5s")
			return map[string]interface{}{"numUsers": 0.0}
		}
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestChat(t *testing.T) {
	server, ts := newChatServer(t)

	a := connectChat(t, ts.URL)
	b := connectChat(t, ts.URL)
	waitFor(t, "two connections in the room", func() bool { return server.RoomLen("/", chatRoom) == 2 })

	a.Emit("add user", "alice")
	a.expect(t, "login", "numUsers", 1.0)
	b.expect(t, "user joined", "username", "alice", "numUsers", 1.0)

	b.Emit("add user", "bob")
	b.expect(t, "login", "numUsers", 2.0)
	a.expect(t, "user joined", "username", "bob", "numUsers", 2.0)

	// A second "add user" is ignored: no announcement, and the name stays.
	a.Emit("add user", "mallory")
	a.Emit("typing")
	b.expect(t, "typing", "username", "alice")
	a.Emit("stop typing")
	b.expect(t, "stop typing", "username", "alice")

	// The sender gets no copy of its own message: the first one A receives is Bob's.
	a.Emit("new message", "hello")
	b.expect(t, "new message", "username", "alice", "message", "hello")
	b.Emit("new message", "hi")
	a.expect(t, "new message", "username", "bob", "message", "hi")

	// A connection that never added a name does not count and leaves silently.
	c := connectChat(t, ts.URL)
	waitFor(t, "three connections in the room", func() bool { return server.RoomLen("/", chatRoom) == 3 })
	_ = c.Close()
	waitFor(t, "the unnamed connection to leave", func() bool { return server.RoomLen("/", chatRoom) == 2 })

	_ = b.Close()
	a.expect(t, "user left", "username", "bob", "numUsers", 1.0)
}

// TestChatNumUsersConcurrent adds users from many connections at once: every login must
// report a different count, and the counter must come back to zero. Run it with -race.
func TestChatNumUsersConcurrent(t *testing.T) {
	const n = 8

	server, ts := newChatServer(t)

	clients := make([]*chatClient, n)
	for i := range clients {
		clients[i] = connectChat(t, ts.URL)
	}
	waitFor(t, "all connections in the room", func() bool { return server.RoomLen("/", chatRoom) == n })

	counts := make([]int, n)
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Emit("add user", "user")
			counts[i] = int(c.login(t)["numUsers"].(float64))
		}()
	}
	wg.Wait()

	sort.Ints(counts)
	for i, got := range counts {
		if got != i+1 {
			t.Fatalf("login counts = %v, want 1..%d", counts, n)
		}
	}

	// Every disconnect decrements: after all of them the next user is the first again.
	for _, c := range clients {
		_ = c.Close()
	}
	waitFor(t, "an empty room", func() bool { return server.RoomLen("/", chatRoom) <= 0 })

	again := connectChat(t, ts.URL)
	again.Emit("add user", "again")
	if got := again.login(t)["numUsers"]; got != 1.0 {
		t.Fatalf("login numUsers after all users left = %v, want 1", got)
	}
}

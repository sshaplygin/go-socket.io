package socketio

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// Covers K26
func TestLateAck(t *testing.T) {
	pending := make(chan AckFunc, 4)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnEvent("/", "later", func(_ Conn, n int, ack AckFunc) string { pending <- ack; return "ignored" })
		s.OnEvent("/", "barrier", func(Conn) string { return "ready" })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send(`27["later",41]`)
	ack := recv(t, pending, "deferred ACK")
	c.send(`28["barrier"]`)
	require.Equal(t, "38[\"ready\"]\n", c.read(), "handler return and automatic empty ACK were suppressed")
	done := make(chan struct{})
	go func() { ack(42, nil); ack("duplicate"); close(done) }()
	recv(t, done, "late ACK goroutine")
	require.Equal(t, "37[42,null]\n", c.read())
	c.send(`29["barrier"]`)
	require.Equal(t, "39[\"ready\"]\n", c.read(), "ACK is sent once")
	c.send(`2["later",3]`)
	ack = recv(t, pending, "unrequested ACK")
	ack("must not be sent")
	c.send(`210["barrier"]`)
	require.Equal(t, "310[\"ready\"]\n", c.read())
}

// Covers K28
func TestEventListeners(t *testing.T) {
	order := make(chan string, 32)
	var n *ServerNamespace
	ts := newTestServer(t, func(s *Server) {
		n = s.Of("/")
		n.OnConnect(func(Conn) error { return nil })
		n.OnEvent("ordered", func(Conn, int) string { order <- "first"; return "first result" })
		n.On("ordered", func(Conn, int) string { order <- "second"; return "second result" })
		n.Once("ordered", func(Conn, int) { order <- "once" })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send(`21["ordered",7]`)
	require.Equal(t, "31[\"first result\"]\n", c.read())
	for _, want := range []string{"first", "second", "once"} {
		require.Equal(t, want, recv(t, order, "listener order"))
	}
	c.send(`22["ordered",8]`)
	require.Equal(t, "32[\"first result\"]\n", c.read())
	for _, want := range []string{"first", "second"} {
		require.Equal(t, want, recv(t, order, "second listener order"))
	}
	require.Empty(t, order)
	require.Panics(t, func() { n.On("ordered", func(Conn, string) {}) })
	require.Panics(t, func() { n.Once("ordered", 42) })

	off := n.On("ordered", func(Conn, int) { order <- "removed" })
	off()
	off()
	n.OnEvent("ordered", func(Conn, string) string { order <- "replacement"; return "replacement" })
	off() // An old removal must not remove the replacement.
	c.send(`23["ordered","new type"]`)
	require.Equal(t, "33[\"replacement\"]\n", c.read())
	require.Equal(t, "replacement", recv(t, order, "replacement listener"))
	require.Empty(t, order)
	n.Off("ordered")
	c.send(`24["ordered","anything"]`)
	require.Equal(t, "34[]\n", c.read())
	require.Empty(t, order)

	// The listener set is frozen for each event, including changes made by it.
	var offSecond func()
	added := false
	n.On("snapshot", func(Conn) {
		order <- "one"
		offSecond()
		if !added {
			added = true
			n.On("snapshot", func(Conn) { order <- "late" })
		}
	})
	offSecond = n.On("snapshot", func(Conn) { order <- "two" })
	c.send(`25["snapshot"]`)
	require.Equal(t, "35[]\n", c.read())
	require.Equal(t, "one", recv(t, order, "snapshot first"))
	require.Equal(t, "two", recv(t, order, "snapshot second"))
	c.send(`26["snapshot"]`)
	require.Equal(t, "36[]\n", c.read())
	require.Equal(t, "one", recv(t, order, "next first"))
	require.Equal(t, "late", recv(t, order, "new listener"))

	// Any callback listener owns the ACK, including when an earlier listener returns.
	late := make(chan AckFunc, 1)
	n.On("mixed", func(Conn, int) int { return 99 })
	n.On("mixed", func(_ Conn, _ int, ack AckFunc) { late <- ack })
	c.send(`27["mixed",1]`)
	mixed := recv(t, late, "mixed callback")
	c.send(`28["absent"]`)
	require.Equal(t, "38[]\n", c.read(), "earlier return value is not an ACK")
	mixed("owned")
	require.Equal(t, "37[\"owned\"]\n", c.read())
}

func TestOnceAcrossConnections(t *testing.T) {
	var called atomic.Int32
	ts := newTestServer(t, func(s *Server) {
		n := s.Of("/")
		n.OnConnect(func(Conn) error { return nil })
		n.Once("once", func(Conn) { called.Add(1) })
	})
	clients := []*rawClient{dialRaw(t, ts.URL), dialRaw(t, ts.URL)}
	for _, c := range clients {
		require.Equal(t, "0", c.read())
	}
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func(c *rawClient) { defer wg.Done(); c.send(`21["once"]`); require.Equal(t, "31[]\n", c.read()) }(c)
	}
	wg.Wait()
	require.Equal(t, int32(1), called.Load())
}

func TestOnceSurvivesEarlierListenerFailure(t *testing.T) {
	h := newNamespaceHandler("", nil)
	attempts, called := 0, 0
	h.OnEvent("event", func(Conn) {
		attempts++
		if attempts == 1 {
			panic("stop dispatch")
		}
	})
	h.addListener("event", func(Conn) { called++ }, true)
	_, err := h.dispatchEvent(&namespaceConn{}, "event")
	require.Error(t, err)
	require.Zero(t, called)
	_, err = h.dispatchEvent(&namespaceConn{}, "event")
	require.NoError(t, err)
	require.Equal(t, 1, called, "a Once listener is consumed only when it is invoked")
}

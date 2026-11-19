//go:build js && wasm

package browsertest

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

// ack emits event with args and returns the argument of the server's ACK.
func ack[T any](t *testing.T, s *client.Socket, event string, args ...any) T {
	t.Helper()
	result := make(chan T, 1)
	s.Emit(event, append(args, func(value T) { result <- value })...)
	return receive(t, result, event+" ACK")
}

// echoConn returns the connection that receives the server's echo event.
func echoConn(t *testing.T, s *client.Socket) client.Conn {
	t.Helper()
	conns := make(chan client.Conn, 1)
	s.OnEvent("echo", func(c client.Conn, _ string) { conns <- c })
	s.Emit("echo", "conn")
	return receive(t, conns, "echo event")
}

func transportOf(c client.Conn) string {
	u := c.URL()
	return u.Query().Get("transport")
}

// awaitNoLibraryGoroutines waits up to bound for the goroutine gate of TestMain to pass.
func awaitNoLibraryGoroutines(t *testing.T, bound time.Duration) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for leaks := libraryGoroutines(); len(leaks) != 0; leaks = libraryGoroutines() {
		if time.Now().After(deadline) {
			t.Fatalf("library goroutines remain after %v:\n%s", bound, strings.Join(leaks, "\n\n"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// eventually waits up to bound until cond holds.
func eventually(t *testing.T, bound time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for !cond() {
		require.True(t, time.Now().Before(deadline), "timeout: %s", what)
		time.Sleep(10 * time.Millisecond)
	}
}

// wrapGlobal replaces the global function name until the test ends with the function
// that body returns. body sees the original as orig and an array made, which the
// replacement fills and the test reads.
func wrapGlobal(t *testing.T, name, body string) js.Value {
	t.Helper()
	original, made := js.Global().Get(name), js.Global().Get("Array").New()
	js.Global().Set(name, js.Global().Get("Function").New("orig", "made", body).Invoke(original, made))
	t.Cleanup(func() { js.Global().Set(name, original) })
	return made
}

func TestPollingConnectAndOrder(t *testing.T) {
	const emits = 200
	_, sockets := connect(t, "/socket.io/", []string{"/", "/admin"},
		client.WithTransports(polling.Default), client.WithWriteBufferSize(emits+1))
	for _, s := range sockets {
		require.Equal(t, "héllo "+s.Namespace(), ack[string](t, s, "echo", "héllo "+s.Namespace()))
	}
	require.Equal(t, "polling", transportOf(echoConn(t, sockets[0])))
	want := make([]string, emits)
	for i := range want {
		want[i] = fmt.Sprintf("0:%d", i)
		sockets[0].Emit("order", 0, i)
	}
	require.Equal(t, want, ack[[]string](t, sockets[0], "order-result"))
}

func TestDefaultTransportsUpgrade(t *testing.T) {
	made := wrapGlobal(t, "WebSocket", "return function(...args) { const ws = new orig(...args); made.push(ws); return ws }")
	// WithTransports without transports selects the default: polling, then websocket.
	_, sockets := connect(t, "/socket.io/", []string{"/"}, client.WithTransports())
	c := echoConn(t, sockets[0])
	eventually(t, 5*time.Second, "upgrade to websocket", func() bool { return transportOf(c) == "websocket" })
	// The only WebSocket is the upgrade probe of the session that opened on polling.
	require.Equal(t, 1, made.Length())
	probe, err := url.Parse(made.Index(0).Get("url").String())
	require.NoError(t, err)
	require.Equal(t, []string{c.ID(), "websocket"}, []string{probe.Query().Get("sid"), probe.Query().Get("transport")})
	require.Equal(t, "upgraded", ack[string](t, sockets[0], "echo", "upgraded"))
}

func TestPollingCookies(t *testing.T) {
	for name, header := range map[string]http.Header{
		"credentials include": {"js.fetch:credentials": {"include"}},
		"no credentials":      nil,
	} {
		t.Run(name, func(t *testing.T) {
			// Every response of a session sets its cookie, and the browser keeps one io
			// cookie for the host: the late CLOSE of an earlier session would replace it.
			awaitNoLibraryGoroutines(t, 5*time.Second)
			_, sockets := connect(t, "/socket.io/", []string{"/"},
				client.WithTransports(polling.Default), client.WithHeader(header))
			sid := sockets[0].ID()
			// Each ACK arrives in the response to another GET.
			for i := 0; i < 3; i++ {
				require.Equal(t, "poll", ack[string](t, sockets[0], "echo", "poll"))
			}
			// Every request after the handshake: the cookie that the handshake set, or none.
			want := ""
			if header != nil {
				want = "io=" + sid
			}
			cookies := ack[[]string](t, sockets[0], "cookies", sid)
			require.NotEmpty(t, cookies)
			for i, cookie := range cookies {
				require.Equal(t, want, cookie, "request %d", i)
			}
		})
	}
}

// Close of a polling connection aborts the fetch of its open long poll.
func TestCloseAbortsLongPoll(t *testing.T) {
	made := wrapGlobal(t, "fetch", `return function(input, init) {
		const entry = {url: String(input), state: "pending"}
		made.push(entry)
		const result = orig.call(globalThis, input, init)
		result.then(() => { entry.state = "resolved" }, (e) => { entry.state = e.name })
		return result
	}`)
	awaitNoLibraryGoroutines(t, 5*time.Second)
	u, err := url.Parse(os.Getenv("SOCKETIO_BROWSER_URL") + "/socket.io/?EIO=3")
	require.NoError(t, err)
	conn, err := polling.Default.Dial(u, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	params, err := conn.(interface {
		Open() (eioparser.ConnParameters, error)
	}).Open()
	require.NoError(t, err)
	// The handshake response also holds the CONNECT of /; the first poll follows its read.
	_, pt, r, err := conn.NextReader()
	require.NoError(t, err)
	require.Equal(t, eioparser.MESSAGE, pt)
	require.NoError(t, r.Close())
	// The server holds a poll without data for its ping interval.
	var poll js.Value
	eventually(t, 5*time.Second, "long poll", func() bool {
		for i := 0; i < made.Length(); i++ {
			if entry := made.Index(i); strings.Contains(entry.Get("url").String(), "sid="+params.SID) {
				poll = entry
				return true
			}
		}
		return false
	})
	require.Equal(t, "pending", poll.Get("state").String())
	require.NoError(t, conn.Close())
	eventually(t, time.Second, "aborted fetch", func() bool { return poll.Get("state").String() == "AbortError" })
	awaitNoLibraryGoroutines(t, time.Second)
}

func TestCloseDuringUpgrade(t *testing.T) {
	for name, tc := range map[string]struct {
		wrapper string        // the WebSocket replacement: made receives the probe
		state   int           // the probe's readyState when Close is called
		bound   time.Duration // after Close, until no library goroutine remains
	}{
		// The probe dials a server that never answers: the dial ends at HandshakeTimeout.
		"probe dial": {`return function(url, ...rest) {
			const ws = new orig(url.replace("/socket.io/", "/silent/"), ...rest); made.push(ws); return ws }`,
			0, 2 * time.Second}, // CONNECTING
		// The probe opened to /nopong/, outside the session, and sent its PING; the
		// client reads no message on it, so only the client can end the probe.
		"probe exchange": {`return function(url, ...rest) {
			const ws = new orig(url.replace("/socket.io/", "/nopong/"), ...rest), send = ws.send.bind(ws)
			Object.defineProperty(ws, "onmessage", {set() {}})
			ws.send = (data) => { if (data === "2probe") { made.push(ws) } send(data) }
			return ws }`, 1, time.Second}, // OPEN
	} {
		t.Run(name, func(t *testing.T) {
			made := wrapGlobal(t, "WebSocket", tc.wrapper)
			awaitNoLibraryGoroutines(t, 5*time.Second)
			// WithTimeout is also the HandshakeTimeout of the websocket transport.
			start := time.Now()
			cl, sockets := connect(t, "/socket.io/", []string{"/"}, client.WithTransports(), client.WithTimeout(time.Second))
			reasons := make(chan string, 1)
			sockets[0].OnDisconnect(func(_ client.Conn, reason string) { reasons <- reason })
			eventually(t, 5*time.Second, "upgrade probe", func() bool { return made.Length() == 1 })
			// The upgrade has neither switched nor given up: the probe is still in its stage,
			// and less time has passed than the 10 s default of Dialer.UpgradeTimeout, after
			// which the client gives up a probe whose browser dial is still CONNECTING.
			require.Equal(t, "polling", transportOf(echoConn(t, sockets[0])))
			require.Equal(t, tc.state, made.Index(0).Get("readyState").Int(), "probe readyState before Close")
			require.Less(t, time.Since(start), 10*time.Second)
			require.NoError(t, cl.Close())
			require.Equal(t, "io client disconnect", receive(t, reasons, "disconnect"))
			awaitNoLibraryGoroutines(t, tc.bound)
			require.Contains(t, []int{2, 3}, made.Index(0).Get("readyState").Int(), "CLOSING or CLOSED")
		})
	}
}

//go:build browsertest

// Command server is the native Socket.IO server of the browser tests. It listens
// on 127.0.0.1:0, prints SOCKETIO_BROWSER_URL and SOCKETIO_BROWSER_CLOSED_URL (the
// URL of a listener it opened and closed) and serves until its standard input
// ends: make test-browser holds the input open while the tests run. It panics on
// error.
//
// Under /socket.io/ the namespaces / and /admin answer the events below; /silent/
// holds each connection open without an answer, and /nopong/ is a websocket
// endpoint that sends OPEN (ping interval and timeout 200 ms) and 40, then answers
// nothing.
package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/parser"
)

// fixture records what the tests read back through acknowledgements.
type fixture struct {
	mu      sync.Mutex
	cookies []string              // the Cookie header of each polling request
	code    int                   // the close code of the latest server close error
	conns   map[net.Conn]struct{} // accepted TCP connections, for drop
}

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	must(closed.Close())

	f := &fixture{conns: map[net.Conn]struct{}{}}
	server := socketio.NewServer(socketio.WithEngineOptions(engineio.WithHooks(&engineio.Hooks{Error: f.closeError})))
	for _, name := range []string{"/", "/admin"} {
		f.register(server.Of(name))
	}
	go func() { must(server.Serve()) }()

	mux := http.NewServeMux()
	mux.Handle("/socket.io/", f.recordCookie(server))
	mux.HandleFunc("/silent/", silent)
	mux.HandleFunc("/nopong/", nopong)
	httpServer := &http.Server{Handler: mux}
	go func() {
		if err := httpServer.Serve(trackingListener{listener, f}); !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()
	fmt.Printf("SOCKETIO_BROWSER_URL=http://%s\nSOCKETIO_BROWSER_CLOSED_URL=http://%s\n", listener.Addr(), closed.Addr())

	_, _ = io.Copy(io.Discard, os.Stdin)
	_ = httpServer.Close()
	must(server.Close())
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

type record struct{ order []string }

func (f *fixture) register(nsp *socketio.ServerNamespace) {
	nsp.OnConnect(func(c socketio.Conn) error { c.SetContext(new(record)); return nil })
	nsp.OnEvent("echo", func(c socketio.Conn, value string) string { c.Emit("echo", value); return value })
	nsp.OnEvent("binary", func(c socketio.Conn, value *parser.Buffer) *parser.Buffer {
		c.Emit("binary", value)
		return value
	})
	nsp.OnEvent("order", func(c socketio.Conn, g, i int) {
		r := c.Context().(*record)
		r.order = append(r.order, fmt.Sprintf("%d:%d", g, i))
	})
	nsp.OnEvent("order-result", func(c socketio.Conn) []string { return c.Context().(*record).order })
	// The namespace disconnect: DISCONNECT for this namespace; the others stay connected.
	nsp.OnEvent("kick", func(c socketio.Conn) { go c.Disconnect() })
	// The engine close: the event "closing", then a websocket close frame with code 1000,
	// or CLOSE over polling. The event is written on the engine connection itself, so that
	// it precedes the close: an Emit is written later by the connection's writer.
	nsp.OnEvent("close-engine", func(c socketio.Conn) {
		go func() {
			if w, err := c.EngineConn().NextWriter(eioparser.String); err == nil {
				_, _ = io.WriteString(w, `2["closing"]`)
				_ = w.Close()
			}
			_ = c.EngineConn().Close()
		}()
	})
	nsp.OnEvent("drop", func(socketio.Conn) { f.drop() })
	nsp.OnEvent("cookies", func(socketio.Conn) []string {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]string{}, f.cookies...)
	})
	nsp.OnEvent("closecode", func(socketio.Conn) int {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.code
	})
}

func (f *fixture) recordCookie(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("transport") == "polling" {
			f.mu.Lock()
			f.cookies = append(f.cookies, r.Header.Get("Cookie"))
			f.mu.Unlock()
		}
		next.ServeHTTP(w, r)
	})
}

func (f *fixture) closeError(_ engineio.Conn, err error) {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		f.mu.Lock()
		f.code = closeErr.Code
		f.mu.Unlock()
	}
}

// drop resets every accepted TCP connection, which the peers see as a transport error.
func (f *fixture) drop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for c := range f.conns {
		if tcp, ok := c.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = c.Close()
	}
	clear(f.conns)
}

type trackingListener struct {
	net.Listener
	f *fixture
}

func (l trackingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.f.mu.Lock()
		l.f.conns[c] = struct{}{}
		l.f.mu.Unlock()
	}
	return c, err
}

// silent takes over the connection and reads it until the peer closes it.
func silent(w http.ResponseWriter, _ *http.Request) {
	c, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, c)
	_ = c.Close()
}

func nopong(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = ws.Close() }()
	open := `0{"sid":"nopong","upgrades":[],"pingInterval":200,"pingTimeout":200}`
	for _, packet := range []string{open, "40"} {
		if ws.WriteMessage(websocket.TextMessage, []byte(packet)) != nil {
			return
		}
	}
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
	}
}

package client_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

func TestClientNamespaces(t *testing.T) {
	serverConnections := make(chan socketio.Conn, 8)
	messages := make(chan string, 8)
	ts := newTestServer(t, func(srv *socketio.Server) {
		for _, namespace := range []string{"/", "/chat", "/late"} {
			srv.OnConnect(namespace, func(c socketio.Conn) error { serverConnections <- c; return nil })
			srv.OnEvent(namespace, "echo", func(c socketio.Conn, value string) string {
				c.Emit("reply", c.Namespace()+":"+value)
				return value
			})
			srv.OnEvent(namespace, "message", func(c socketio.Conn, value string) { messages <- c.Namespace() + ":" + value })
		}
	})
	cl, err := client.New(ts.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	connected := make(chan client.Conn, 8)
	replies := make(chan string, 8)
	disconnected := make(chan string, 8)
	install := func(s *client.Socket) {
		s.OnConnect(func(c client.Conn) error { connected <- c; return nil })
		s.OnEvent("reply", func(_ client.Conn, value string) { replies <- value })
		s.OnDisconnect(func(c client.Conn, reason string) { disconnected <- c.Namespace() + ":" + reason })
	}
	root, chat := cl.Socket("/"), cl.Socket("/chat")
	install(root)
	install(chat)
	require.Same(t, chat, cl.Socket("chat?ignored=second"))
	require.Equal(t, "/chat", chat.Namespace())
	require.NoError(t, cl.Connect())
	clients, servers := map[string]client.Conn{}, map[string]socketio.Conn{}
	collect := func(n int) {
		for i := 0; i < n; i++ {
			c := recv(t, connected, "namespace OnConnect")
			require.NotContains(t, clients, c.Namespace(), "duplicate OnConnect")
			clients[c.Namespace()] = c
			s := recv(t, serverConnections, "server namespace OnConnect")
			servers[s.Namespace()] = s
		}
	}
	collect(2)
	late := cl.Socket("/late")
	install(late) // registration may race with the server's CONNECT reply
	collect(1)
	for _, s := range []*client.Socket{root, chat, late} {
		s.Emit("echo", "hello")
		require.Equal(t, s.Namespace()+":hello", recv(t, replies, "server event"))
		require.Equal(t, servers["/"].EngineConn().ID(), servers[s.Namespace()].EngineConn().ID())
		prefix := ""
		if s.Namespace() != "/" {
			prefix = s.Namespace() + "#"
		}
		require.Equal(t, prefix+clients["/"].ID(), clients[s.Namespace()].ID())
	}
	chat.Send("message")
	require.Equal(t, "/chat:message", recv(t, messages, "Send as message"))
	require.NoError(t, chat.Close())
	require.Equal(t, "/chat:io client disconnect", recv(t, disconnected, "namespace close"))
	for _, s := range []*client.Socket{root, late} {
		s.Emit("echo", "still open")
		require.Equal(t, s.Namespace()+":still open", recv(t, replies, "remaining namespace"))
	}
	require.Empty(t, connected)
	require.NoError(t, cl.Close())
	require.ElementsMatch(t, []string{"/:io client disconnect", "/late:io client disconnect"},
		[]string{recv(t, disconnected, "root close"), recv(t, disconnected, "late close")})
}

func TestClientConnectTimeout(t *testing.T) {
	connected := make(chan struct{}, 2)
	real := newTestServer(t, func(srv *socketio.Server) {
		srv.OnConnect("/", func(socketio.Conn) error { return nil })
	})
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseRequest()
	var mu sync.Mutex
	first := true
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		stall := first
		if stall {
			first = false
		}
		mu.Unlock()
		if stall {
			close(entered)
			<-release // accept TCP but leave the engine OPEN unanswered
			defer close(finished)
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, real.URL+r.URL.RequestURI(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header = r.Header.Clone()
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = response.Body.Close() }()
		for key, values := range response.Header {
			w.Header()[key] = values
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(proxy.Close)
	cl, err := client.New(proxy.URL, client.WithTimeout(100*time.Millisecond), client.WithTransports([]transport.Transport{polling.Default}...))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	cl.OnConnect(func(client.Conn) error { connected <- struct{}{}; return nil })
	start := time.Now()
	require.ErrorIs(t, cl.Connect(), client.ErrConnectTimeout)
	require.Less(t, time.Since(start), 1100*time.Millisecond)
	recv(t, entered, "stalled request")
	require.Empty(t, connected)
	require.NoError(t, cl.Connect(), "a new attempt can succeed while the old request is still pending")
	recv(t, connected, "retry OnConnect")
	releaseRequest()
	recv(t, finished, "late handshake response")
	require.Empty(t, connected, "the late OPEN must not attach another connection")
}

func TestClientOptions(t *testing.T) {
	metadata := make(chan socketio.Conn, 2)
	srv := socketio.NewServer(socketio.WithEngineOptions(shortPingOptions()...))
	srv.OnConnect("/", func(socketio.Conn) error { return nil })
	srv.OnConnect("/chat", func(c socketio.Conn) error { metadata <- c; return nil })
	srv.OnConnect("/other", func(c socketio.Conn) error { metadata <- c; return nil })
	go func() { _ = srv.Serve() }()
	requests := make(chan *http.Request, 64)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		srv.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })
	query, header := url.Values{"custom": {"engine"}}, http.Header{"X-Client": {"header"}}
	opts := []client.Option{client.WithPath("/custom/engine"), client.WithQuery(query), client.WithHeader(header)}
	cl, err := client.New(ts.URL+"/chat?address=value", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	// Constructors own their options: later caller mutations cannot change requests.
	query.Set("custom", "mutated")
	header.Set("X-Client", "mutated")
	chat := cl.Socket("/chat?namespace=value")
	ready := make(chan struct{}, 1)
	chat.OnConnect(func(client.Conn) error { ready <- struct{}{}; return nil })
	require.NoError(t, cl.Connect())
	recv(t, ready, "custom path namespace")
	c := recv(t, metadata, "server metadata")
	require.Equal(t, "/custom/engine/", c.Request().URL.Path)
	require.Equal(t, "header", c.Request().Header.Get("X-Client"))
	require.Equal(t, "engine", c.Handshake().Query.Get("custom"))
	require.Equal(t, "value", c.Handshake().Query.Get("address"))
	// Socket("/chat") already exists as the default socket: its first query wins.
	require.Empty(t, c.Handshake().Query.Get("namespace"))
	other := cl.Socket("/other?namespace=value")
	other.OnConnect(func(client.Conn) error { ready <- struct{}{}; return nil })
	recv(t, ready, "namespace with its own query")
	otherConn := recv(t, metadata, "namespace query metadata")
	require.Equal(t, "value", otherConn.Handshake().Query.Get("namespace"))
	require.Equal(t, "engine", otherConn.Handshake().Query.Get("custom"))
	deadline := time.NewTimer(waitFor)
	defer deadline.Stop()
	for {
		select {
		case r := <-requests:
			require.Equal(t, "/custom/engine/", r.URL.Path)
			require.Equal(t, "engine", r.URL.Query().Get("custom"))
			require.Equal(t, "header", r.Header.Get("X-Client"))
			if r.URL.Query().Get("transport") == "websocket" {
				return
			}
		case <-deadline.C:
			t.Fatal("websocket probe did not carry the options")
		}
	}
}

func TestClientEmitBuffered(t *testing.T) {
	for _, size := range []int{64, 2} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			values := make(chan int, 8)
			ts := newTestServer(t, func(srv *socketio.Server) {
				srv.OnConnect("/", func(socketio.Conn) error { close(entered); <-release; return nil })
				srv.OnEvent("/", "number", func(_ socketio.Conn, value int) int { values <- value; return value })
			})
			var logs bytes.Buffer
			cl, err := client.New(ts.URL, client.WithWriteBufferSize(size), client.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			cl.Emit("number", 0)
			cl.Emit("number", 1)
			if size == 2 {
				cl.Emit("number", 99)
				require.Contains(t, logs.String(), "WARN")
				require.Contains(t, logs.String(), "emit buffer full")
			}
			require.NoError(t, cl.Connect())
			recv(t, entered, "server OnConnect before CONNECT reply")
			require.Empty(t, values)
			if size != 2 {
				cl.Emit("number", 2)
			}
			ready := make(chan struct{}, 1)
			cl.OnConnect(func(client.Conn) error { ready <- struct{}{}; return nil })
			once.Do(func() { close(release) })
			recv(t, ready, "CONNECT reply")
			expected := []int{0, 1}
			if size != 2 {
				expected = append(expected, 2)
			}
			for _, want := range expected {
				require.Equal(t, want, recv(t, values, "buffered event"))
			}
			acked := make(chan int, 1)
			cl.Emit("number", 3, func(value int) { acked <- value })
			require.Equal(t, 3, recv(t, acked, "later event ACK"))
			require.Equal(t, 3, recv(t, values, "later event"))
			require.Empty(t, values, "the overflow event was dropped")
		})
	}
}

func TestClientCloseSendsDisconnect(t *testing.T) {
	for _, all := range []bool{false, true} {
		name := "socket"
		if all {
			name = "client"
		}
		t.Run(name, func(t *testing.T) {
			reasons, clientReasons := make(chan string, 4), make(chan string, 4)
			ts := newTestServer(t, func(srv *socketio.Server) {
				for _, nsp := range []string{"/", "/chat"} {
					srv.OnConnect(nsp, func(socketio.Conn) error { return nil })
					srv.OnDisconnect(nsp, func(c socketio.Conn, reason string) { reasons <- c.Namespace() + ":" + reason })
					srv.OnEvent(nsp, "echo", func(_ socketio.Conn, value string) string { return value })
				}
			})
			cl, err := client.New(ts.URL)
			require.NoError(t, err)
			t.Cleanup(func() { _ = cl.Close() })
			ready := make(chan struct{}, 2)
			chat := cl.Socket("/chat")
			for _, socket := range []*client.Socket{cl.Socket("/"), chat} {
				socket.OnConnect(func(client.Conn) error { ready <- struct{}{}; return nil })
				socket.OnDisconnect(func(c client.Conn, reason string) { clientReasons <- c.Namespace() + ":" + reason })
			}
			require.NoError(t, cl.Connect())
			recv(t, ready, "first namespace")
			recv(t, ready, "second namespace")
			if all {
				require.NoError(t, cl.Close())
				require.ElementsMatch(t, []string{"/:client namespace disconnect", "/chat:client namespace disconnect"}, []string{recv(t, reasons, "first DISCONNECT"), recv(t, reasons, "second DISCONNECT")})
				require.ElementsMatch(t, []string{"/:io client disconnect", "/chat:io client disconnect"}, []string{recv(t, clientReasons, "first callback"), recv(t, clientReasons, "second callback")})
			} else {
				require.NoError(t, chat.Close())
				require.Equal(t, "/chat:client namespace disconnect", recv(t, reasons, "Socket DISCONNECT"))
				require.Equal(t, "/chat:io client disconnect", recv(t, clientReasons, "Socket callback"))
				acked := make(chan string, 1)
				cl.Emit("echo", "root survives", func(value string) { acked <- value })
				require.Equal(t, "root survives", recv(t, acked, "surviving namespace ACK"))
				require.Empty(t, reasons)
			}
		})
	}
}

func TestClientRejectsServerConnHandler(t *testing.T) {
	c, err := client.New("http://127.0.0.1/")
	require.NoError(t, err)
	require.Panics(t, func() { c.OnEvent("wrong-role", func(socketio.Conn) {}) })
}

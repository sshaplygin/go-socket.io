package socketio_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

type benchItem struct {
	User   string            `json:"user"`
	Text   string            `json:"text"`
	Time   int64             `json:"time"`
	Tags   []string          `json:"tags,omitempty"`
	Meta   map[string]string `json:"meta,omitempty"`
	Online bool              `json:"online"`
}
type benchServer struct {
	srv          *socketio.Server
	url          string
	connected    chan socketio.Conn
	disconnected chan struct{}
	frames       chan error
	stop         chan struct{}
	deadline     <-chan time.Time
	clients      []*websocket.Conn
	readers      sync.WaitGroup
}

func benchNewServer(b *testing.B) *benchServer {
	h := &benchServer{connected: make(chan socketio.Conn, 256), disconnected: make(chan struct{}, 256), frames: make(chan error, 256), stop: make(chan struct{})}
	timer := time.NewTimer(5 * time.Minute)
	h.deadline = timer.C
	h.srv = socketio.NewServer(socketio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	h.srv.OnConnect("/", func(c socketio.Conn) error { h.connected <- c; return nil })
	h.srv.OnDisconnect("/", func(socketio.Conn, string) {
		select {
		case h.disconnected <- struct{}{}:
		case <-h.stop:
		}
	})
	ts := httptest.NewServer(h.srv)
	h.url = "ws" + strings.TrimPrefix(ts.URL, "http") + "/?EIO=3&transport=websocket"
	served := make(chan struct{})
	go func() { defer close(served); _ = h.srv.Serve() }()
	b.Cleanup(func() {
		close(h.stop)
		for _, c := range h.clients {
			_ = c.Close()
		}
		h.readers.Wait()
		_ = h.srv.Close()
		ts.Close()
		<-served
		timer.Stop()
		polling.Default.Client.CloseIdleConnections()
	})
	return h
}
func (h *benchServer) dial(b *testing.B) (*websocket.Conn, socketio.Conn) {
	b.Helper()
	c, resp, err := websocket.DefaultDialer.Dial(h.url, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		b.Fatal(err)
	}
	if err = c.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		_ = c.Close()
		b.Fatal(err)
	}
	for _, prefix := range []string{"0", "40"} {
		_, p, err := c.ReadMessage()
		if err != nil {
			_ = c.Close()
			b.Fatal(err)
		}
		if !strings.HasPrefix(string(p), prefix) {
			_ = c.Close()
			b.Fatalf("expected %s handshake, got %q", prefix, p)
		}
	}
	if err = c.SetReadDeadline(time.Time{}); err != nil {
		_ = c.Close()
		b.Fatal(err)
	}
	select {
	case conn := <-h.connected:
		return c, conn
	case <-h.deadline:
		_ = c.Close()
		b.Fatal("connection callback timeout")
		return nil, nil
	}
}
func (h *benchServer) addClient(b *testing.B) socketio.Conn {
	c, conn := h.dial(b)
	h.clients = append(h.clients, c)
	h.readers.Add(1)
	go func() {
		defer h.readers.Done()
		for {
			_, _, err := c.ReadMessage()
			select {
			case h.frames <- err:
			case <-h.stop:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return conn
}
func (h *benchServer) waitFrames(b *testing.B, n int) {
	b.Helper()
	for i := 0; i < n; i++ {
		select {
		case err := <-h.frames:
			if err != nil {
				b.Fatal(err)
			}
		case <-h.deadline:
			b.Fatal("frame timeout")
		}
	}
}
func BenchmarkBroadcastWebsocket(b *testing.B) {
	items := make([]benchItem, 100)
	for i := range items {
		items[i] = benchItem{"alice", "hello world, this is a chat message", 1700000000, []string{"a", "b", "c"}, map[string]string{"k1": "v1", "k2": "v2"}, true}
	}
	for _, tc := range []struct {
		name    string
		payload interface{}
	}{{"string-10", "hello"}, {"structs100-10", items}} {
		b.Run(tc.name, func(b *testing.B) {
			h := benchNewServer(b)
			for i := 0; i < 10; i++ {
				h.addClient(b)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !h.srv.BroadcastToNamespace("/", "data", tc.payload) {
					b.Fatal("broadcast rejected")
				}
				h.waitFrames(b, 10)
			}
			b.StopTimer()
		})
	}
}
func BenchmarkEmitWebsocket(b *testing.B) {
	b.Run("string", func(b *testing.B) {
		h := benchNewServer(b)
		c := h.addClient(b)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			c.Emit("data", "hello")
			h.waitFrames(b, 1)
		}
		b.StopTimer()
	})
}
func BenchmarkJoinRoom(b *testing.B) {
	b.Run("sockets-200", func(b *testing.B) {
		h := benchNewServer(b)
		conns := make([]socketio.Conn, 200)
		for i := range conns {
			conns[i] = h.addClient(b)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if !h.srv.JoinRoom("/", "room", conns[i%len(conns)]) {
				b.Fatal("join rejected")
			}
		}
		b.StopTimer()
	})
}
func BenchmarkConnectWebsocket(b *testing.B) {
	h := benchNewServer(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, _ := h.dial(b)
		if err := c.Close(); err != nil {
			b.Fatal(err)
		}
		select {
		case <-h.disconnected:
		case <-h.deadline:
			b.Fatalf("disconnect timeout after iteration %d", i)
		}
	}
	b.StopTimer()
}

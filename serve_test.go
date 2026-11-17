package socketio

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/stretchr/testify/require"
)

// TestServeReturnsNilAfterClose checks that Serve treats the end of the
// accept loop caused by Close as a normal shutdown.
func TestServeReturnsNilAfterClose(t *testing.T) {
	srv := NewServer()
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve() }()

	require.NoError(t, srv.Close())
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after Close")
	}
}

func TestFailedRootRemovesEngineSession(t *testing.T) {
	s := NewServer(WithEngineOptions(shortPingOptions()...), WithWriteBufferSize(1))
	s.OnConnect("/", func(c Conn) error { c.Emit("one"); c.Emit("two"); return nil })
	go func() { _ = s.Serve() }()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	defer func() { _ = ws.Close() }()
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(time.Second)))
	for err == nil {
		_, _, err = ws.ReadMessage()
	}
	require.Eventually(t, func() bool { return s.Engine().Count() == 0 }, waitFor, 5*time.Millisecond, "closed failed-root session remains in engine registry")
}

func TestSuccessfulSessionRemovedAfterPeerClose(t *testing.T) {
	s, url := defectServer(t, func(*Server) {})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	require.Equal(t, 1, s.Engine().Count())
	require.NoError(t, c.conn.Close())
	require.Eventually(t, func() bool { return s.Engine().Count() == 0 }, time.Second, time.Millisecond)
}

// A connection whose Engine.IO session has ended is no longer counted when its
// OnDisconnect handler runs, as engine.io 3.6.2 has already dropped the client
// from clientsCount when socket.io 2.5.0 emits "disconnect" for "transport close".
func TestCountExcludesEndedSessionInOnDisconnect(t *testing.T) {
	counts := make(chan [2]int, 1)
	s, url := defectServer(t, func(s *Server) {
		s.OnDisconnect("/", func(Conn, string) { counts <- [2]int{s.Count(), s.Engine().Count()} })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	require.Equal(t, 1, s.Count())
	require.NoError(t, c.conn.Close())
	require.Equal(t, [2]int{0, 0}, recv(t, counts, "OnDisconnect"))
}

type constantID string

func (id constantID) NewID() string { return string(id) }

// pollingPeer is an Engine.IO 3 polling client with text payloads whose requests
// the test makes itself: each one has been answered when its call returns.
type pollingPeer struct {
	t       *testing.T
	client  *http.Client
	poll    string   // the URL of the session's requests
	sid     string   // the session ID from OPEN
	pending []string // packets received and not yet returned by next
}

// newPollingPeer opens a polling session at url.
func newPollingPeer(t *testing.T, url string) *pollingPeer {
	t.Helper()
	p := &pollingPeer{t: t, client: &http.Client{Timeout: waitFor}, poll: url + "/?EIO=3&transport=polling&b64=1"}
	t.Cleanup(p.client.CloseIdleConnections)
	open := p.next()
	require.True(t, strings.HasPrefix(open, "0"), open)
	params, err := eioparser.ReadConnParameters(strings.NewReader(open[1:]))
	require.NoError(t, err)
	p.sid = params.SID
	p.poll += "&sid=" + p.sid
	return p
}

// next returns the next packet from the server, polling when none is pending.
// The handshake response can carry packets after OPEN.
func (p *pollingPeer) next() string {
	p.t.Helper()
	if len(p.pending) == 0 {
		payload := p.do(http.MethodGet, "")
		for payload != "" {
			size, rest, ok := strings.Cut(payload, ":")
			require.True(p.t, ok, payload)
			n, err := strconv.Atoi(size)
			require.NoError(p.t, err)
			require.LessOrEqual(p.t, n, len(rest), payload)
			p.pending = append(p.pending, rest[:n])
			payload = rest[n:]
		}
	}
	require.NotEmpty(p.t, p.pending)
	packet := p.pending[0]
	p.pending = p.pending[1:]
	return packet
}

// do sends a request with a text payload and returns the body of its 200 response.
func (p *pollingPeer) do(method, payload string) string {
	p.t.Helper()
	req, err := http.NewRequest(method, p.poll, strings.NewReader(payload))
	require.NoError(p.t, err)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	}
	resp, err := p.client.Do(req)
	require.NoError(p.t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(p.t, err)
	require.Equal(p.t, http.StatusOK, resp.StatusCode, string(body))
	return string(body)
}

// The ID of an ended Engine.IO session can be issued again while the Socket.IO
// connection that held it still runs its disconnect handlers. The new connection
// connects once the previous one is released, and the previous connection's
// cleanup neither unregisters the new engine session nor takes its rooms.
func TestReusedIDWaitsForPreviousConnection(t *testing.T) {
	for _, block := range []string{"disconnecting", "OnDisconnect"} {
		t.Run(block, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var previousDone atomic.Bool
			connected := make(chan Conn, 2)
			connectedAfterPrevious := make(chan bool, 2)
			events := make(chan string, 1)
			// The clients close their sessions, so a heartbeat window far above the
			// test's waits keeps a loaded machine from ending the second session.
			s := NewServer(WithEngineOptions(
				engineio.WithPingInterval(time.Second), engineio.WithPingTimeout(time.Minute),
				engineio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
				engineio.WithSessionIDGenerator(constantID("dup")),
			))
			s.OnConnect("/", func(c Conn) error {
				c.Join("room")
				connectedAfterPrevious <- previousDone.Load()
				connected <- c
				return nil
			})
			var first sync.Once
			hold := func() { first.Do(func() { close(entered); <-release }) }
			if block == "disconnecting" {
				s.OnEvent("/", "disconnecting", func(Conn, string) { hold() })
			}
			s.OnDisconnect("/", func(Conn, string) {
				if block == "OnDisconnect" {
					hold()
				}
				previousDone.Store(true)
			})
			s.OnEvent("/", "hello", func(_ Conn, msg string) { events <- msg })
			go func() { _ = s.Serve() }()
			ts := httptest.NewServer(s)
			t.Cleanup(func() { _ = s.Close(); ts.Close() })

			// The first client's requests are made one at a time, and each has been
			// answered before the test goes on. Requests are routed by session ID: a
			// poll of the first client that reached the server after the second
			// handshake would be served by the second session, and its cancellation
			// would end that session.
			a := newPollingPeer(t, ts.URL)
			require.Equal(t, "dup", a.sid)
			require.Equal(t, "40", a.next())
			require.False(t, recv(t, connectedAfterPrevious, "first OnConnect"))
			recv(t, connected, "first OnConnect")
			require.Equal(t, "ok", a.do(http.MethodPost, "1:1"), "CLOSE")
			recv(t, entered, block)
			require.Zero(t, s.Engine().Count(), "the first engine session has ended")

			b := dialRaw(t, ts.URL)
			require.Equal(t, "dup", b.conn.ID())
			require.Equal(t, 1, s.Engine().Count())
			close(release)

			require.Equal(t, "0", b.read())
			require.True(t, recv(t, connectedAfterPrevious, "second OnConnect"), "connected while the previous connection's handlers ran")
			second := recv(t, connected, "second OnConnect")
			require.Equal(t, 1, s.Engine().Count())
			require.Contains(t, s.Engine().Clients(), "dup")
			require.Equal(t, 1, s.RoomLen("/", "room"))
			require.Equal(t, 1, s.RoomLen("/", "dup"))

			second.Emit("direct")
			require.Equal(t, "2[\"direct\"]\n", b.read())
			s.BroadcastToRoom("/", "room", "to-room")
			require.Equal(t, "2[\"to-room\"]\n", b.read())
			b.send(`2["hello","b"]`)
			require.Equal(t, "b", recv(t, events, "event from the second session"))
		})
	}
}

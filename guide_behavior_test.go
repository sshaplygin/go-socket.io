package socketio

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

// A broadcast of a lifecycle name (connect, disconnect, disconnecting, error)
// sends nothing and returns nil: each selected socket runs its own listeners of
// that name instead, as Conn.Emit does for one socket. socket.io 2.5.0 sends
// such a namespace or room broadcast to the clients and runs only the sender's
// listeners for a socket broadcast (docs/PROTOCOL.md).
func TestReservedBroadcastRunsRecipientListeners(t *testing.T) {
	seen := make(chan string, 16)
	results := make(chan error, 4)
	var root *ServerNamespace
	_, url := defectServer(t, func(s *Server) {
		root = s.Of("/")
		root.OnConnect(func(c Conn) error { c.Join("r"); return nil })
		root.On("error", func(c Conn, v string) { seen <- c.ID() + " " + v })
		root.OnEvent("trigger", func(c Conn, mode string) {
			switch mode {
			case "namespace":
				results <- root.Emit("error", mode)
			case "room":
				results <- root.To("r").Emit("error", mode)
			case "socket broadcast":
				results <- c.Broadcast().Emit("error", mode)
			case "socket room":
				results <- c.To("r").Emit("error", mode)
			}
		})
		root.OnEvent("barrier", func(c Conn) { c.Emit("barrier") })
	})
	a, b := dialRaw(t, url), dialRaw(t, url)
	require.Equal(t, "0", a.read())
	require.Equal(t, "0", b.read())
	ids := root.SocketIDs()
	require.Len(t, ids, 2)
	sender := a.conn.ID()
	other := ids[0]
	if other == sender {
		other = ids[1]
	}
	for _, tc := range []struct {
		mode string
		want []string
	}{
		{"namespace", []string{sender + " namespace", other + " namespace"}},
		{"room", []string{sender + " room", other + " room"}},
		{"socket broadcast", []string{other + " socket broadcast"}},
		{"socket room", []string{other + " socket room"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			a.send(`2["trigger","` + tc.mode + `"]`)
			require.NoError(t, recv(t, results, "the broadcast's result"))
			got := []string{recv(t, seen, "a listener")}
			if len(tc.want) == 2 {
				got = append(got, recv(t, seen, "a second listener"))
			}
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			require.Equal(t, want, got)
			// Packets of one connection arrive in order: the barrier's ACK path
			// would follow any event the broadcast had written.
			a.send(`2["barrier"]`)
			b.send(`2["barrier"]`)
			require.Equal(t, ev("barrier"), a.read(), "the sender received an event")
			require.Equal(t, ev("barrier"), b.read(), "the other socket received an event")
			require.Empty(t, drain(seen))
		})
	}
}

// Rooms belong to one namespace: a socket of /chat that joined "r" does not
// receive a broadcast to "r" of /, and every socket is in its own ID room.
func TestRoomsArePerNamespace(t *testing.T) {
	joined := make(chan Conn, 2)
	var root *ServerNamespace
	_, url := defectServer(t, func(s *Server) {
		root = s.Of("/")
		root.OnConnect(func(c Conn) error { c.Join("r"); joined <- c; return nil })
		s.Of("/chat").OnConnect(func(c Conn) error { c.Join("r"); joined <- c; return nil })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	rootConn := recv(t, joined, "root OnConnect")
	c.send("0/chat,")
	require.Equal(t, "0/chat,", c.read())
	chatConn := recv(t, joined, "/chat OnConnect")
	require.ElementsMatch(t, []string{rootConn.ID(), "r"}, rootConn.Rooms())
	require.ElementsMatch(t, []string{chatConn.ID(), "r"}, chatConn.Rooms())
	require.NoError(t, root.To("r").Emit("news", 1))
	require.Equal(t, ev("news", 1), c.read())
	require.NoError(t, root.To(rootConn.ID()).Emit("direct", 2))
	require.Equal(t, ev("direct", 2), c.read(), "the root broadcast reached /chat")
}

// A socket joins its ID room after OnConnect returns nil. Inside OnConnect,
// Rooms, SocketIDs and RoomLen leave it out, and a broadcast to the socket's ID,
// or to the namespace before the socket joins another room, returns nil but does
// not reach it; a room joined in OnConnect, a namespace broadcast after that
// join and a direct Emit do. socket.io 2.5.0
// delivers io.emit and io.to(socket.id).emit from its connection listener
// (docs/V1.md).
func TestIDRoomJoinedAfterOnConnect(t *testing.T) {
	type view struct {
		rooms, ids []string
		roomLen    int
		errs       []error
	}
	views := make(chan view, 1)
	var root *ServerNamespace
	s, url := defectServer(t, func(s *Server) {
		root = s.Of("/")
		root.OnConnect(func(c Conn) error {
			v := view{rooms: c.Rooms(), ids: root.SocketIDs(c.ID()), roomLen: s.RoomLen("/", c.ID())}
			v.errs = append(v.errs, root.Emit("all"), root.To(c.ID()).Emit("self"))
			c.Join("lobby")
			v.errs = append(v.errs, root.To("lobby").Emit("lobby"), root.Emit("all"), root.To(c.ID()).Emit("self"))
			c.Emit("direct")
			views <- v
			return nil
		})
		root.OnEvent("barrier", func(c Conn) { c.Emit("barrier") })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	v := recv(t, views, "OnConnect")
	require.Empty(t, v.rooms)
	require.Empty(t, v.ids)
	require.Zero(t, v.roomLen)
	require.Equal(t, []error{nil, nil, nil, nil, nil}, v.errs)
	require.Equal(t, ev("lobby"), c.read())
	require.Equal(t, ev("all"), c.read(), "the namespace broadcast after a join")
	require.Equal(t, ev("direct"), c.read())
	c.send(`2["barrier"]`)
	require.Equal(t, ev("barrier"), c.read(), "a broadcast from OnConnect reached the connecting socket")

	ids := root.SocketIDs()
	require.Len(t, ids, 1)
	require.Equal(t, 1, s.RoomLen("/", ids[0]))
	require.NoError(t, root.To(ids[0]).Emit("self"))
	require.Equal(t, ev("self"), c.read())
}

// Event middleware observes the decoded arguments; replacing an element of its
// slice does not change what the handler receives. socket.use in socket.io
// 2.5.0 can rewrite the packet.
func TestEventMiddlewareCannotReplaceArguments(t *testing.T) {
	got := make(chan string, 1)
	_, url := defectServer(t, func(s *Server) {
		s.Of("/").OnConnect(func(c Conn) error {
			c.Use(func(_ string, args []interface{}) error { args[0] = "rewritten"; return nil })
			return nil
		})
		s.OnEvent("/", "say", func(_ Conn, v string) { got <- v })
	})
	c := dialRaw(t, url)
	require.Equal(t, "0", c.read())
	c.send(`2["say","original"]`)
	require.Equal(t, "original", recv(t, got, "the handler"))
}

// To give a reason and drop a client, OnConnect accepts it, emits an
// application event and calls Close: the client receives CONNECT, the event and
// DISCONNECT in that order, then the session ends. A Close before a rejection is
// returned drops the ERROR packet (TestRejectedAdmissionDuringCloseDoesNotConnect
// and the subtest here).
func TestRootRejectionKeepsReasonWithOnConnectClose(t *testing.T) {
	t.Run("emit and close in OnConnect", func(t *testing.T) {
		reasons := make(chan string, 1)
		ts := newTestServer(t, func(s *Server) {
			s.Of("/").OnConnect(func(c Conn) error {
				c.Emit("unauthorized", "bad token")
				return c.Close()
			})
			s.OnDisconnect("/", func(_ Conn, reason string) { reasons <- reason })
		})
		c := dialRaw(t, ts.URL)
		require.Equal(t, "0", c.read())
		require.Equal(t, ev("unauthorized", "bad token"), c.read())
		require.Equal(t, "1", c.read())
		require.True(t, c.readClosed(), "the session stays open")
		require.Equal(t, "server namespace disconnect", recv(t, reasons, "OnDisconnect"))
	})
	t.Run("close then reject", func(t *testing.T) {
		ran := make(chan struct{}, 1)
		ts := newTestServer(t, func(s *Server) {
			s.Of("/").Use(func(c Conn) error {
				_ = c.Close()
				ran <- struct{}{}
				return errors.New("unauthorized")
			})
		})
		// The session ends while the handshake request is being answered, so the
		// dial fails on that request, or it ends right after OPEN.
		d := engineclient.Dialer{Transports: []transport.Transport{polling.Default}}
		conn, err := d.Dial(ts.URL, nil)
		recv(t, ran, "the middleware")
		if err != nil {
			var failed *polling.OpError
			require.ErrorAs(t, err, &failed, "the handshake request fails in the transport")
			return
		}
		t.Cleanup(func() { _ = conn.Close() })
		c := &rawClient{t: t, conn: conn}
		require.True(t, c.readClosed(), "the client received a packet")
	})
}

// The Engine.IO causes reach OnDisconnect as the values of the engineio.Reason
// constants. Server.Close reports engineio.ReasonForcedClose, not
// engineio.ReasonServerShutdown, which is the close reason of the Engine.IO
// session only.
func TestDisconnectReasonsEqualEngineConstants(t *testing.T) {
	var srv *Server
	reasons := make(chan string, 2)
	ts := newTestServer(t, func(s *Server) {
		srv = s
		s.OnConnect("/", func(Conn) error { return nil })
		s.OnDisconnect("/", func(_ Conn, reason string) { reasons <- reason })
	})
	left := dialRaw(t, ts.URL)
	require.Equal(t, "0", left.read())
	require.NoError(t, left.conn.Close())
	require.Equal(t, string(engineio.ReasonTransportClose), recv(t, reasons, "OnDisconnect after the client's CLOSE"))

	stays := dialRaw(t, ts.URL)
	require.Equal(t, "0", stays.read())
	require.NoError(t, srv.Close())
	reason := recv(t, reasons, "OnDisconnect after Server.Close")
	require.Equal(t, string(engineio.ReasonForcedClose), reason)
	require.NotEqual(t, string(engineio.ReasonServerShutdown), reason)
}

// A polling response written after the http.Server's WriteTimeout has expired
// for that request is lost when it fits net/http's write buffer: net/http
// buffers the write, fails to send it when the handler returns and closes the TCP
// connection, while the session counts the packet as delivered and stays open. A
// larger response fails the write in the handler, and the session ends with
// "ping timeout". With PingInterval or PingTimeout longer than WriteTimeout, a
// pending poll outlives the timeout.
func TestPollingResponseAfterServerWriteTimeoutIsLost(t *testing.T) {
	large := make([]byte, 30000)
	_, err := rand.Read(large)
	require.NoError(t, err)
	for _, tc := range []struct {
		name, payload string
		closed        bool
	}{
		{"fits the buffer", "x", false},
		// Random bytes keep the response larger than the buffer after gzip.
		{"exceeds the buffer", base64.StdEncoding.EncodeToString(large), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pollAfterWriteTimeout(t, tc.payload, tc.closed)
		})
	}
}

func pollAfterWriteTimeout(t *testing.T, payload string, closed bool) {
	const writeTimeout = time.Second
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(WithLogger(quiet), WithEngineOptions(engineio.WithLogger(quiet),
		engineio.WithPingInterval(time.Minute), engineio.WithPingTimeout(time.Minute)))
	conns, reasons := make(chan Conn, 1), make(chan string, 1)
	srv.OnConnect("/", func(c Conn) error { conns <- c; return nil })
	srv.OnDisconnect("/", func(_ Conn, reason string) { reasons <- reason })
	go func() { _ = srv.Serve() }()
	polls := make(chan struct{}, 4)
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Query().Get("sid") != "" {
			polls <- struct{}{} // net/http set this request's write deadline before calling the handler
		}
		srv.ServeHTTP(w, r)
	}))
	ts.Config.WriteTimeout = writeTimeout
	ts.Start()
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })
	// One TCP connection per request: http.Transport silently retries a GET that
	// fails on a reused connection before any response byte.
	hc := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	get := func(query string) (string, error) {
		resp, err := hc.Get(ts.URL + "/socket.io/?EIO=3&b64=1&transport=polling" + query)
		if err != nil {
			return "", err
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		return string(body), err
	}
	open, err := get("")
	require.NoError(t, err)
	var handshake struct{ Sid string }
	require.NoError(t, json.Unmarshal([]byte(open[strings.Index(open, "{"):strings.Index(open, "}")+1]), &handshake))
	c := recv(t, conns, "OnConnect")
	if !strings.Contains(open, "2:40") {
		body, err := get("&sid=" + handshake.Sid)
		require.NoError(t, err)
		require.Contains(t, body, "2:40", "the CONNECT packet")
	}
	// That CONNECT poll has returned; its token must not stand for the next poll.
	require.LessOrEqual(t, len(drain(polls)), 1)

	type result struct {
		body string
		err  error
	}
	poll := make(chan result, 1)
	go func() {
		body, err := get("&sid=" + handshake.Sid)
		poll <- result{body, err}
	}()
	recv(t, polls, "the pending poll")
	time.Sleep(2 * writeTimeout) // only lengthens the wait past the deadline under load
	c.Emit("push", payload)
	got := recv(t, poll, "the poll's end")
	require.Error(t, got.err, "the poll was answered: %.40q", got.body)
	require.NotContains(t, got.body, "push")
	if closed {
		require.Equal(t, "ping timeout", recv(t, reasons, "OnDisconnect"))
		return
	}
	require.True(t, c.Connected(), "the session reported the lost write")
	require.Equal(t, 1, srv.Count())
	require.Empty(t, drain(reasons))
}

// The client bundle route of socket.io 2.5.0 (serveClient) is not served: the
// request reaches the Engine.IO server without a transport and is rejected.
func TestClientBundleIsNotServed(t *testing.T) {
	ts := newTestServer(t, func(*Server) {})
	resp, err := http.Get(ts.URL + "/socket.io/socket.io.js")
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.JSONEq(t, `{"code":0,"message":"Transport unknown"}`, string(body))
}

// Engine.IO sessions live in one server instance: a sid issued by one instance
// is unknown to another one, so polling across instances needs session affinity.
func TestSessionIsUnknownToAnotherInstance(t *testing.T) {
	a := newTestServer(t, func(s *Server) { s.OnConnect("/", func(Conn) error { return nil }) })
	b := newTestServer(t, func(s *Server) { s.OnConnect("/", func(Conn) error { return nil }) })
	c := dialRaw(t, a.URL)
	require.Equal(t, "0", c.read())
	resp, err := http.Get(b.URL + "/socket.io/?EIO=3&transport=polling&sid=" + c.conn.ID())
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.JSONEq(t, `{"code":1,"message":"Session ID unknown"}`, string(body))
}

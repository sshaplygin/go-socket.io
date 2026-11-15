package socketio

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	redisadapter "github.com/sshaplygin/go-socket.io/adapter/redis"
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

// The listeners of a lifecycle-name broadcast run on the broadcasting goroutine
// before the call returns, also for a socket that is inside an event handler of
// its own. BroadcastToNamespace and BroadcastToRoom report true.
func TestReservedBroadcastRunsListenersBeforeItReturns(t *testing.T) {
	type outcome struct {
		reported bool
		ran      int32
	}
	inHandler, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	outcomes := make(chan outcome, 2)
	var ran atomic.Int32
	_, url := defectServer(t, func(s *Server) {
		root := s.Of("/")
		root.OnConnect(func(c Conn) error { c.Join("r"); return nil })
		root.OnEvent("block", func(Conn) { close(inHandler); <-release })
		root.On("error", func(Conn, string) { ran.Add(1) })
		root.OnEvent("trigger", func(Conn) {
			reported := s.BroadcastToNamespace("/", "error", "x")
			outcomes <- outcome{reported, ran.Load()}
			reported = s.BroadcastToRoom("/", "r", "error", "x")
			outcomes <- outcome{reported, ran.Load()}
		})
	})
	a, b := dialRaw(t, url), dialRaw(t, url)
	require.Equal(t, "0", a.read())
	require.Equal(t, "0", b.read())
	b.send(`2["block"]`)
	recv(t, inHandler, "b's handler")
	a.send(`2["trigger"]`)
	// b's handler is still running: release closes when the test returns.
	require.Equal(t, outcome{true, 2}, recv(t, outcomes, "BroadcastToNamespace"))
	require.Equal(t, outcome{true, 4}, recv(t, outcomes, "BroadcastToRoom"))
}

// An instance that receives a lifecycle-name broadcast through Redis runs the
// listeners on the adapter's subscriber reader, which also reads the replies to
// cluster requests: a request made from such a listener gets no reply, waits for
// WithRequestsTimeout and returns the local sockets with ErrRequestTimeout.
func TestClusterRequestInRedisDeliveredLifecycleListenerTimesOut(t *testing.T) {
	type result struct {
		ids []string
		err error
	}
	mr := miniredis.RunT(t)
	results := make(chan result, 1)
	instance := func(listen bool) (*Server, *rawClient) {
		var srv *Server
		ts := newTestServer(t, func(s *Server) {
			srv = s
			s.SetAdapter(redisadapter.New(redisadapter.WithAddr(mr.Addr()),
				redisadapter.WithRequestsTimeout(time.Second)))
			s.OnConnect("/", func(Conn) error { return nil })
			if listen {
				s.Of("/").On("error", func(Conn, string) {
					ids, err := s.broadcastOf("/").(redisadapter.Cluster).IDsContext(context.Background())
					results <- result{ids, err}
				})
			}
		})
		c := dialRaw(t, ts.URL)
		require.Equal(t, "0", c.read())
		return srv, c
	}
	sender, onSender := instance(false)
	receiver, onReceiver := instance(true)
	both := []string{onSender.conn.ID(), onReceiver.conn.ID()}
	sort.Strings(both)
	cluster := receiver.broadcastOf("/").(redisadapter.Cluster)
	// Outside the listener the same request is answered by the other instance.
	require.Eventually(t, func() bool {
		ids, err := cluster.IDsContext(context.Background())
		sort.Strings(ids)
		return err == nil && len(ids) == 2 && ids[0] == both[0] && ids[1] == both[1]
	}, 30*time.Second, 20*time.Millisecond)

	require.NoError(t, sender.Of("/").Emit("error", "x"))
	got := recv(t, results, "the listener's cluster request")
	require.ErrorIs(t, got.err, redisadapter.ErrRequestTimeout)
	require.Equal(t, []string{onReceiver.conn.ID()}, got.ids)
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

// A polling response over HTTP/1.x written after the http.Server's WriteTimeout
// has expired for that request is lost when it fits net/http's write buffer: net/http
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

// Over HTTP/2 net/http resets the stream of a poll that is still pending when
// the http.Server's WriteTimeout expires, with nothing to write, and the poll
// fails. Since Go 1.23 the reset cancels the request, and OnDisconnect receives
// "transport error" at that moment, with a heartbeat window of two minutes. The
// net/http of Go 1.22 does not cancel it: the session ends with "ping timeout"
// when its heartbeat window, four seconds here, has passed.
func TestPendingPollOverHTTP2EndsAtServerWriteTimeout(t *testing.T) {
	heartbeat, reason := time.Minute, engineio.ReasonTransportError
	if strings.HasPrefix(runtime.Version(), "go1.22") {
		heartbeat, reason = 2*time.Second, engineio.ReasonPingTimeout
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(WithLogger(quiet), WithEngineOptions(engineio.WithLogger(quiet),
		engineio.WithPingInterval(heartbeat), engineio.WithPingTimeout(heartbeat)))
	connected, reasons := make(chan struct{}, 1), make(chan string, 1)
	srv.OnConnect("/", func(Conn) error { connected <- struct{}{}; return nil })
	srv.OnDisconnect("/", func(_ Conn, reason string) { reasons <- reason })
	go func() { _ = srv.Serve() }()
	polls := make(chan string, 4)
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Query().Get("sid") != "" {
			polls <- r.Proto
		}
		srv.ServeHTTP(w, r)
	}))
	ts.EnableHTTP2 = true
	ts.Config.WriteTimeout = 2 * time.Second
	ts.StartTLS()
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })
	hc := ts.Client()
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
	recv(t, connected, "OnConnect")
	if !strings.Contains(open, "2:40") {
		body, err := get("&sid=" + handshake.Sid)
		require.NoError(t, err)
		require.Contains(t, body, "2:40", "the CONNECT packet")
	}
	// That CONNECT poll has returned; its token must not stand for the next poll.
	require.LessOrEqual(t, len(drain(polls)), 1)

	poll := make(chan error, 1)
	go func() {
		_, err := get("&sid=" + handshake.Sid)
		poll <- err
	}()
	require.Equal(t, "HTTP/2.0", recv(t, polls, "the pending poll"))
	require.ErrorContains(t, recv(t, poll, "the poll's end"), "INTERNAL_ERROR")
	require.Equal(t, string(reason), recv(t, reasons, "OnDisconnect"))
}

// A client that aborts a pending poll ends the session: the request context is
// cancelled, the polling transport fails, and OnDisconnect receives "transport
// error" (docs/PROTOCOL.md, polling payload).
func TestAbortedPollEndsSessionWithTransportError(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := NewServer(WithLogger(quiet), WithEngineOptions(engineio.WithLogger(quiet),
		engineio.WithPingInterval(time.Minute), engineio.WithPingTimeout(time.Minute)))
	connected, reasons := make(chan struct{}, 1), make(chan string, 1)
	srv.OnConnect("/", func(Conn) error { connected <- struct{}{}; return nil })
	srv.OnDisconnect("/", func(_ Conn, reason string) { reasons <- reason })
	go func() { _ = srv.Serve() }()
	polls := make(chan struct{}, 4)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Query().Get("sid") != "" {
			polls <- struct{}{}
		}
		srv.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { _ = srv.Close(); ts.Close() })
	hc := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	t.Cleanup(hc.CloseIdleConnections)
	get := func(ctx context.Context, query string) (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			ts.URL+"/socket.io/?EIO=3&b64=1&transport=polling"+query, nil)
		if err != nil {
			return "", err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return "", err
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		return string(body), err
	}
	open, err := get(context.Background(), "")
	require.NoError(t, err)
	var handshake struct{ Sid string }
	require.NoError(t, json.Unmarshal([]byte(open[strings.Index(open, "{"):strings.Index(open, "}")+1]), &handshake))
	recv(t, connected, "OnConnect")
	if !strings.Contains(open, "2:40") {
		body, err := get(context.Background(), "&sid="+handshake.Sid)
		require.NoError(t, err)
		require.Contains(t, body, "2:40", "the CONNECT packet")
	}
	// That CONNECT poll has returned; its token must not stand for the next poll.
	require.LessOrEqual(t, len(drain(polls)), 1)

	ctx, abort := context.WithCancel(context.Background())
	defer abort()
	aborted := make(chan error, 1)
	go func() {
		_, err := get(ctx, "&sid="+handshake.Sid)
		aborted <- err
	}()
	recv(t, polls, "the pending poll")
	abort()
	require.ErrorIs(t, recv(t, aborted, "the aborted poll"), context.Canceled)
	require.Equal(t, string(engineio.ReasonTransportError), recv(t, reasons, "OnDisconnect"))
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

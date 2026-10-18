package engineio

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

type hookRecords struct {
	mu     sync.Mutex
	values []string
}

func (r *hookRecords) add(value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = append(r.values, value)
}
func (r *hookRecords) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.values...)
}

func hookReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("hook operation timed out")
		var zero T
		return zero
	}
}

func TestEngineServerEvents(t *testing.T) {
	t.Run("connection", func(t *testing.T) {
		for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
			seen := make(chan Conn, 1)
			records := &hookRecords{}
			s := NewServer(WithConnInitor(func(_ *http.Request, c Conn) { c.SetContext("initialized"); records.add("init") }), WithHooks(&Hooks{Connection: func(c Conn) { records.add("connection"); seen <- c }}))
			ts := httptest.NewServer(s)
			t.Cleanup(func() { _ = s.Close(); ts.Close() })
			client, err := (&engineclient.Dialer{Transports: []transport.Transport{tr}}).Dial(ts.URL, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			conn, err := s.Accept()
			require.NoError(t, err)
			require.Same(t, conn, hookReceive(t, seen))
			require.Equal(t, StateOpen, conn.ReadyState())
			require.Equal(t, "initialized", conn.Context())
			require.Equal(t, []string{"init", "connection"}, records.all())
		}
	})
	t.Run("initial_headers", func(t *testing.T) {
		for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
			records := &hookRecords{}
			s := NewServer(WithHooks(&Hooks{
				InitialHeaders: func(_ *http.Request, h http.Header) { records.add("initial"); h.Set("X-Initial", "present") },
				Headers: func(_ *http.Request, h http.Header) {
					if h.Get("X-Initial") == "present" {
						records.add("headers")
					}
					h.Set("X-Headers", "present")
				},
			}))
			ts := httptest.NewServer(s)
			t.Cleanup(func() { _ = s.Close(); ts.Close() })
			client, err := (&engineclient.Dialer{Transports: []transport.Transport{tr}}).Dial(ts.URL, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			require.Equal(t, "present", client.RemoteHeader().Get("X-Initial"))
			require.Equal(t, "present", client.RemoteHeader().Get("X-Headers"))
			require.Equal(t, []string{"initial", "headers"}, records.all())
		}
	})
	t.Run("headers", func(t *testing.T) {
		records := &hookRecords{}
		entered, release := make(chan struct{}, 1), make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })
		defer unblock()
		f := newParityWire(t, []Option{WithHooks(&Hooks{Headers: func(r *http.Request, h http.Header) {
			records.add(r.Method + " " + h.Get("Content-Type"))
			h.Set("X-Observed", "yes")
			if r.Method == http.MethodGet && r.URL.Query().Get("sid") != "" {
				entered <- struct{}{}
				<-release
			}
		}})})
		reads := parityReader(f.conn)
		written := parityWrite(f.conn, "hello")
		responses := make(chan *http.Response, 1)
		errors := make(chan error, 1)
		go func() {
			r, err := f.client.Get(f.poll)
			if err != nil {
				errors <- err
				return
			}
			responses <- r
		}()
		hookReceive(t, entered)
		select {
		case <-responses:
			t.Fatal("headers reached client before the hook returned")
		case err := <-errors:
			t.Fatal(err)
		default:
		}
		unblock()
		resp := hookReceive(t, responses)
		require.Equal(t, "yes", resp.Header.Get("X-Observed"))
		require.Equal(t, "text/plain; charset=UTF-8", resp.Header.Get("Content-Type"))
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Contains(t, string(body), "4hello")
		require.NoError(t, hookReceive(t, written))
		code, _ := f.request(t, http.MethodPost, f.poll, "2:4x")
		require.Equal(t, http.StatusOK, code)
		parityMessage(t, reads, "x")
		require.Equal(t, []string{"GET text/plain; charset=UTF-8", "GET text/plain; charset=UTF-8", "POST "}, records.all())
	})
	t.Run("connection_error", func(t *testing.T) {
		blocked := errors.New("private rejection")
		for _, tc := range []struct {
			query, method string
			code          int
		}{
			{"transport=missing", http.MethodGet, 0},
			{"transport=polling&sid=missing", http.MethodGet, 1},
			{"transport=polling", http.MethodPost, 2},
			{"transport=websocket", http.MethodGet, 3},
			{"transport=polling&deny=1", http.MethodGet, 4},
		} {
			var requests []*http.Request
			var codes []int
			var messages []string
			var causes []error
			s := NewServer(WithRequestChecker(func(r *http.Request) (http.Header, error) {
				if r.URL.Query().Get("deny") != "" {
					return nil, blocked
				}
				return nil, nil
			}), WithHooks(&Hooks{ConnectionError: func(r *http.Request, code int, message string, err error) {
				requests = append(requests, r)
				codes = append(codes, code)
				messages = append(messages, message)
				causes = append(causes, err)
			}}))
			t.Cleanup(func() { _ = s.Close() })
			r := httptest.NewRequest(tc.method, "/?EIO=3&"+tc.query, nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			require.Equal(t, []int{tc.code}, codes)
			require.Equal(t, []string{serverErrors[tc.code]}, messages)
			require.Equal(t, []*http.Request{r}, requests)
			if tc.code == 4 {
				require.ErrorIs(t, causes[0], blocked)
			}
			require.GreaterOrEqual(t, w.Code, 400)
		}
	})
}

// Covers E19
func TestEngineSocketEvents(t *testing.T) {
	records := &hookRecords{}
	h := &Hooks{
		InitialHeaders: func(r *http.Request, _ http.Header) { records.add("initial-header:" + r.URL.Query().Get("transport")) },
		Headers:        func(r *http.Request, _ http.Header) { records.add("headers:" + r.URL.Query().Get("transport")) },
		Packet: func(_ Conn, pt eioparser.PacketType, ft eioparser.FrameType) {
			records.add(fmt.Sprintf("read:%d:%d", pt, ft))
		},
		PacketCreate: func(_ Conn, pt eioparser.PacketType, ft eioparser.FrameType) {
			records.add(fmt.Sprintf("create:%d:%d", pt, ft))
		},
		Heartbeat: func(Conn) { records.add("heartbeat") },
		Upgrading: func(c Conn, tr string) {
			records.add(fmt.Sprintf("upgrading:%s:%s:%t", tr, c.Transport(), c.Upgraded()))
		},
		Upgrade: func(c Conn, tr string) { records.add(fmt.Sprintf("upgrade:%s:%s:%t", tr, c.Transport(), c.Upgraded())) },
		Flush:   func(Conn) { records.add("flush") },
		Drain:   func(Conn) { records.add("drain") },
		Error: func(c Conn, err error) {
			if err != nil {
				records.add("error:" + string(c.ReadyState()))
			}
		},
	}
	f := newParityWire(t, []Option{WithHooks(h)})
	reads := parityReader(f.conn)
	ws := f.probe(t)
	_, body := f.request(t, http.MethodGet, f.poll, "")
	require.Contains(t, body, "6")
	require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("5")))
	require.Eventually(t, f.conn.Upgraded, time.Second, time.Millisecond)
	require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("2heartbeat")))
	_, pong, err := ws.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "3heartbeat", string(pong))
	require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("4hello")))
	parityMessage(t, reads, "hello")
	require.NoError(t, ws.Close())
	require.Error(t, hookReceive(t, reads).err)
	got := records.all()
	index := func(value string) int {
		for i, v := range got {
			if v == value {
				return i
			}
		}
		t.Fatalf("missing %q in %v", value, got)
		return -1
	}
	require.Less(t, index("create:0:0"), index("flush"))
	require.Less(t, index("flush"), index("drain"))
	require.Less(t, index("upgrading:websocket:polling:false"), index("upgrade:websocket:websocket:true"))
	require.Less(t, index("read:2:0"), index("heartbeat"))
	require.Less(t, index("heartbeat"), index("create:3:0"))
	require.Less(t, index("read:4:0"), index("error:closing"))
	for _, unique := range []string{"initial-header:polling", "headers:websocket", "heartbeat", "upgrading:websocket:polling:false", "upgrade:websocket:websocket:true", "error:closing"} {
		count := 0
		for _, event := range got {
			if event == unique {
				count++
			}
		}
		require.Equal(t, 1, count, unique)
	}
	require.Equal(t, StateClosed, f.conn.ReadyState())
}

func TestEngineClosePacketDoesNotEmitError(t *testing.T) {
	errors := make(chan error, 1)
	f := newParityWire(t, []Option{WithHooks(&Hooks{Error: func(_ Conn, err error) { errors <- err }})})
	reads := parityReader(f.conn)
	_, _ = f.request(t, http.MethodPost, f.poll, "1:1")
	require.Error(t, hookReceive(t, reads).err)
	require.Empty(t, errors)
	require.Equal(t, StateClosed, f.conn.ReadyState())
}

func TestRejectedWebsocketHasNoSessionHeaders(t *testing.T) {
	for _, kind := range []string{"malformed", "origin", "upgrade origin"} {
		t.Run(kind, func(t *testing.T) {
			records := &hookRecords{}
			h := &Hooks{
				InitialHeaders:  func(_ *http.Request, _ http.Header) { records.add("initial") },
				Headers:         func(r *http.Request, _ http.Header) { records.add("headers:" + r.URL.Query().Get("transport")) },
				ConnectionError: func(_ *http.Request, _ int, _ string, _ error) { records.add("error") },
			}
			opts := []Option{WithHooks(h), WithTransports([]transport.Transport{polling.Default, &websocket.Transport{CheckOrigin: func(*http.Request) bool { return false }}}...)}
			if kind == "malformed" {
				s := NewServer(opts...)
				t.Cleanup(func() { _ = s.Close() })
				w := httptest.NewRecorder()
				s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/?EIO=3&transport=websocket", nil))
				require.Equal(t, http.StatusBadRequest, w.Code)
				require.Equal(t, []string{"error"}, records.all())
				require.Zero(t, s.Count())
				return
			}
			var target string
			var server *Server
			if kind == "upgrade origin" {
				f := newParityWire(t, opts)
				server = f.srv
				target = "ws" + f.http.URL[4:] + "/?EIO=3&transport=websocket&sid=" + f.params.SID
			} else {
				server = NewServer(opts...)
				ts := httptest.NewServer(server)
				t.Cleanup(func() { _ = server.Close(); ts.Close() })
				target = "ws" + ts.URL[4:] + "/?EIO=3&transport=websocket"
			}
			_, response, err := gorilla.DefaultDialer.Dial(target, http.Header{"Origin": []string{"https://elsewhere.test"}})
			require.Error(t, err)
			require.Equal(t, http.StatusForbidden, response.StatusCode)
			require.NoError(t, response.Body.Close())
			require.Eventually(t, func() bool { values := records.all(); return len(values) > 0 && values[len(values)-1] == "error" }, time.Second, time.Millisecond)
			if kind == "upgrade origin" {
				require.Equal(t, []string{"initial", "headers:polling", "error"}, records.all())
				require.Equal(t, 1, server.Count())
			} else {
				require.Equal(t, []string{"error"}, records.all())
				require.Zero(t, server.Count())
			}
		})
	}
}

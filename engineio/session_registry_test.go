package engineio_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
)

// registryPeer is a raw Engine.IO 3 client of one session, on websocket or polling.
type registryPeer struct {
	base   string
	sid    string
	ws     *gorilla.Conn // nil for polling
	client *http.Client
}

func dialRegistryPeer(t *testing.T, base, transport string) *registryPeer {
	t.Helper()
	p := &registryPeer{base: base, client: &http.Client{Timeout: 10 * time.Second}}
	t.Cleanup(p.client.CloseIdleConnections)
	if transport == "polling" {
		p.sid = handshake(t, base)
		return p
	}
	ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/?EIO=3&transport=websocket", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(10*time.Second)))
	_, open, err := ws.ReadMessage()
	require.NoError(t, err)
	m := sidPattern.FindSubmatch(open)
	require.NotNil(t, m, "no sid in OPEN packet %q", open)
	p.sid, p.ws = string(m[1]), ws
	// Keep reading, as a client does, so the server's close frame is answered.
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	return p
}

// send writes one Engine.IO packet, such as "1" for CLOSE.
func (p *registryPeer) send(t *testing.T, packet string) {
	t.Helper()
	if p.ws != nil {
		require.NoError(t, p.ws.WriteMessage(gorilla.TextMessage, []byte(packet)))
		return
	}
	code, body := p.request(t, http.MethodPost, strconv.Itoa(len(packet))+":"+packet)
	require.Equal(t, http.StatusOK, code, body)
}

// poll issues a polling GET with the session's sid.
func (p *registryPeer) poll(t *testing.T) (int, string) {
	t.Helper()
	return p.request(t, http.MethodGet, "")
}

func (p *registryPeer) request(t *testing.T, method, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, p.base+"/?EIO=3&transport=polling&b64=1&sid="+p.sid, strings.NewReader(body))
	require.NoError(t, err)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "text/plain; charset=UTF-8")
	}
	resp, err := p.client.Do(req)
	require.NoError(t, err)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode, string(b)
}

// readUntilEnd reads and discards messages until NextReader fails and returns that error.
func readUntilEnd(conn engineio.Conn) <-chan error {
	done := make(chan error, 1)
	go func() {
		for {
			_, r, err := conn.NextReader()
			if err != nil {
				done <- err
				return
			}
			_, _ = io.Copy(io.Discard, r)
			_ = r.Close()
		}
	}()
	return done
}

func acceptOne(t *testing.T, s *engineio.Server) engineio.Conn {
	t.Helper()
	accepted := make(chan engineio.Conn, 1)
	go func() {
		c, err := s.Accept()
		if err != nil {
			c = nil
		}
		accepted <- c
	}()
	select {
	case c := <-accepted:
		require.NotNil(t, c, "Accept failed")
		return c
	case <-time.After(10 * time.Second):
		t.Fatal("session was not accepted")
		return nil
	}
}

// A standalone server unregisters a session once it has ended, by whichever
// path, without the application calling Remove: Count falls back to zero and the
// session's sid is unknown to later requests, as in engine.io 3.6.2. A polling
// session closed by Conn.Close stays registered until its CLOSE is delivered.
func TestEndedSessionLeavesRegistry(t *testing.T) {
	for _, tc := range []struct {
		name      string
		transport string
		heartbeat time.Duration // ping interval and timeout; 0 keeps the defaults
		reason    engineio.CloseReason
		end       func(t *testing.T, s *engineio.Server, conn engineio.Conn, p *registryPeer)
	}{
		{name: "websocket client CLOSE", transport: "websocket", reason: engineio.ReasonTransportClose,
			end: func(t *testing.T, _ *engineio.Server, _ engineio.Conn, p *registryPeer) { p.send(t, "1") }},
		{name: "websocket close frame", transport: "websocket", reason: engineio.ReasonTransportClose,
			end: func(t *testing.T, _ *engineio.Server, _ engineio.Conn, p *registryPeer) {
				require.NoError(t, p.ws.WriteControl(gorilla.CloseMessage, gorilla.FormatCloseMessage(gorilla.CloseNormalClosure, ""), time.Now().Add(5*time.Second)))
			}},
		{name: "websocket dropped", transport: "websocket", reason: engineio.ReasonTransportError,
			end: func(t *testing.T, _ *engineio.Server, _ engineio.Conn, p *registryPeer) {
				require.NoError(t, p.ws.UnderlyingConn().Close())
			}},
		{name: "websocket ping timeout", transport: "websocket", heartbeat: 50 * time.Millisecond, reason: engineio.ReasonPingTimeout,
			end: func(*testing.T, *engineio.Server, engineio.Conn, *registryPeer) {}},
		{name: "websocket Conn.Close", transport: "websocket", reason: engineio.ReasonForcedClose,
			end: func(t *testing.T, _ *engineio.Server, conn engineio.Conn, _ *registryPeer) {
				require.NoError(t, conn.Close())
			}},
		{name: "websocket Abort", transport: "websocket", reason: engineio.ReasonForcedClose,
			end: func(t *testing.T, _ *engineio.Server, conn engineio.Conn, _ *registryPeer) {
				require.NoError(t, engineio.Abort(conn))
			}},
		{name: "polling client CLOSE", transport: "polling", reason: engineio.ReasonTransportClose,
			end: func(t *testing.T, _ *engineio.Server, _ engineio.Conn, p *registryPeer) { p.send(t, "1") }},
		{name: "polling ping timeout", transport: "polling", heartbeat: 50 * time.Millisecond, reason: engineio.ReasonPingTimeout,
			end: func(*testing.T, *engineio.Server, engineio.Conn, *registryPeer) {}},
		{name: "polling Conn.Close", transport: "polling", reason: engineio.ReasonForcedClose,
			end: func(t *testing.T, s *engineio.Server, conn engineio.Conn, p *registryPeer) {
				require.NoError(t, conn.Close())
				require.Equal(t, 1, s.Count(), "a closing polling session stays registered until its CLOSE is delivered")
				code, body := p.poll(t)
				require.Equal(t, http.StatusOK, code, body)
				require.Contains(t, body, "1:1", "the CLOSE packet is delivered on the next poll")
			}},
		{name: "polling Abort", transport: "polling", reason: engineio.ReasonForcedClose,
			end: func(t *testing.T, _ *engineio.Server, conn engineio.Conn, _ *registryPeer) {
				require.NoError(t, engineio.Abort(conn))
			}},
		{name: "upgraded websocket client CLOSE", transport: "upgrade", reason: engineio.ReasonTransportClose,
			end: func(t *testing.T, _ *engineio.Server, conn engineio.Conn, p *registryPeer) {
				ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(p.base, "http")+"/?EIO=3&transport=websocket&sid="+p.sid, nil)
				require.NoError(t, err)
				t.Cleanup(func() { _ = ws.Close() })
				require.NoError(t, ws.SetReadDeadline(time.Now().Add(10*time.Second)))
				require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("2probe")))
				_, b, err := ws.ReadMessage()
				require.NoError(t, err)
				require.Equal(t, "3probe", string(b))
				require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("5")))
				require.Eventually(t, conn.Upgraded, 10*time.Second, time.Millisecond)
				require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("1")))
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opts []engineio.Option
			if tc.heartbeat > 0 {
				opts = append(opts, engineio.WithPingInterval(tc.heartbeat), engineio.WithPingTimeout(tc.heartbeat))
			}
			s := engineio.NewServer(opts...)
			ts := httptest.NewServer(s)
			t.Cleanup(func() { _ = s.Close(); ts.Close() })
			transport := tc.transport
			if transport == "upgrade" {
				transport = "polling"
			}
			p := dialRegistryPeer(t, ts.URL, transport)
			conn := acceptOne(t, s)
			require.Equal(t, p.sid, conn.ID())
			if tc.heartbeat == 0 { // a short heartbeat may already have expired
				require.Equal(t, 1, s.Count())
			}
			done := readUntilEnd(conn)

			tc.end(t, s, conn, p)

			var err error
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("the session did not end")
			}
			var closeErr *engineio.CloseError
			require.ErrorAs(t, err, &closeErr)
			require.Equal(t, tc.reason, closeErr.Reason)
			// NextReader returns once the close has finished, so no waiting is needed.
			require.Equal(t, engineio.StateClosed, conn.ReadyState())
			require.Zero(t, s.Count(), "an ended session stays registered")
			require.Empty(t, s.Clients())

			code, body := p.poll(t)
			require.Equal(t, http.StatusBadRequest, code)
			require.JSONEq(t, `{"code":1,"message":"Session ID unknown"}`, body)

			s.Remove(conn.ID()) // still accepted, and a no-op
			require.Zero(t, s.Count())
			require.NoError(t, conn.Close(), "closing an ended session again")
		})
	}
}

// Server.Remove keeps unregistering a live session without closing it, and the
// session's later end does not disturb the registry.
func TestRemoveBeforeSessionEnds(t *testing.T) {
	s := engineio.NewServer()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	p := dialRegistryPeer(t, ts.URL, "websocket")
	conn := acceptOne(t, s)
	done := readUntilEnd(conn)
	s.Remove(conn.ID())
	s.Remove(conn.ID())
	require.Zero(t, s.Count())
	require.Equal(t, engineio.StateOpen, conn.ReadyState(), "Remove does not close the session")

	other := dialRegistryPeer(t, ts.URL, "websocket")
	otherConn := acceptOne(t, s)
	otherDone := readUntilEnd(otherConn)
	require.Equal(t, 1, s.Count())

	p.send(t, "1")
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the removed session did not end")
	}
	require.Equal(t, 1, s.Count(), "the end of a removed session unregisters only itself")
	require.Contains(t, s.Clients(), otherConn.ID())

	other.send(t, "1")
	select {
	case err := <-otherDone:
		require.Error(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end")
	}
	require.Zero(t, s.Count())
}

type retentionMarker struct{ _ [64]byte }

// Ended sessions are released by the server: once the application drops its
// references, the garbage collector reclaims them without a Remove call.
func TestEndedSessionsAreNotRetained(t *testing.T) {
	const sessions = 32
	s := engineio.NewServer()
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })

	var reclaimed atomic.Int32
	for i := 0; i < sessions; i++ {
		p := dialRegistryPeer(t, ts.URL, "websocket")
		conn := acceptOne(t, s)
		marker := &retentionMarker{}
		runtime.SetFinalizer(marker, func(*retentionMarker) { reclaimed.Add(1) })
		conn.SetContext(marker)
		done := readUntilEnd(conn)
		if i%2 == 0 {
			p.send(t, "1")
		} else {
			require.NoError(t, p.ws.UnderlyingConn().Close())
		}
		select {
		case err := <-done:
			var closeErr *engineio.CloseError
			require.ErrorAs(t, err, &closeErr)
		case <-time.After(10 * time.Second):
			t.Fatal("the session did not end")
		}
	}
	require.Zero(t, s.Count())
	require.Eventually(t, func() bool {
		runtime.GC()
		return reclaimed.Load() == sessions
	}, 10*time.Second, 10*time.Millisecond, "ended sessions are still reachable")
}

// A session ID generator that repeats itself can open a new session with the ID
// of one that has ended: the ended session no longer holds the ID.
func TestEndedSessionReleasesItsID(t *testing.T) {
	s := engineio.NewServer(engineio.WithSessionIDGenerator(constantSessionID{}))
	ts := httptest.NewServer(s)
	t.Cleanup(func() { _ = s.Close(); ts.Close() })
	first := dialRegistryPeer(t, ts.URL, "websocket")
	firstConn := acceptOne(t, s)
	done := readUntilEnd(firstConn)
	first.send(t, "1")
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not end")
	}

	second := dialRegistryPeer(t, ts.URL, "websocket")
	require.Equal(t, first.sid, second.sid)
	secondConn := acceptOne(t, s)
	require.NotSame(t, firstConn, secondConn)
	require.Equal(t, 1, s.Count())
	require.Same(t, secondConn, s.Clients()[second.sid])
}

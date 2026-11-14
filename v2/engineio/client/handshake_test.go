package client

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
)

// TestDialClosesFailedAttempt: when the last transport tried fails after its connection
// was established, here because the server sends a MESSAGE instead of OPEN, Dial closes
// that connection before it returns the error.
func TestDialClosesFailedAttempt(t *testing.T) {
	closed := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_ = wsutil.WriteServerText(c, []byte("4not an open packet"))
		_, _ = io.Copy(io.Discard, c)
		close(closed)
	}))
	t.Cleanup(ts.Close)

	_, err := (&Dialer{Transports: []transport.Transport{websocket.Default}}).Dial(ts.URL, nil)
	require.ErrorContains(t, err, "invalid open")
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection of the failed attempt is still open")
	}
}

// TestDialHandshakeTimeout: Dialer.HandshakeTimeout bounds each transport tried. A server
// that accepts TCP but never answers the websocket handshake, and servers that never send
// OPEN on websocket or on polling, fail Dial at the timeout with an error that wraps
// os.ErrDeadlineExceeded.
func TestDialHandshakeTimeout(t *testing.T) {
	const timeout = 200 * time.Millisecond
	silent := func(t *testing.T) string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		var mu sync.Mutex
		var conns []net.Conn
		go func() {
			for c, err := l.Accept(); err == nil; c, err = l.Accept() {
				mu.Lock()
				conns = append(conns, c)
				mu.Unlock()
			}
		}()
		t.Cleanup(func() {
			_ = l.Close()
			mu.Lock()
			defer mu.Unlock()
			for _, c := range conns {
				_ = c.Close()
			}
		})
		return "http://" + l.Addr().String()
	}
	noOpen := func(t *testing.T) string {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("transport") == "websocket" {
				if c, _, _, err := ws.UpgradeHTTP(r, w); err == nil {
					_, _ = io.Copy(io.Discard, c)
					_ = c.Close()
				}
				return
			}
			<-r.Context().Done()
		}))
		t.Cleanup(ts.Close)
		return ts.URL
	}
	for _, tc := range []struct {
		name   string
		tr     transport.Transport
		server func(*testing.T) string
	}{
		{"websocket handshake", websocket.Default, silent},
		{"websocket open", websocket.Default, noOpen},
		{"polling open", polling.Default, noOpen},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url := tc.server(t)
			start := time.Now()
			errs := make(chan error, 1)
			go func() {
				_, err := (&Dialer{Transports: []transport.Transport{tc.tr}, HandshakeTimeout: timeout}).Dial(url, nil)
				errs <- err
			}()
			select {
			case err := <-errs:
				require.ErrorIs(t, err, os.ErrDeadlineExceeded)
				require.GreaterOrEqual(t, time.Since(start), timeout)
			case <-time.After(timeout + 2*time.Second):
				t.Fatal("Dial is not bounded by HandshakeTimeout")
			}
		})
	}
}

// TestDialHandshakeTimeoutPerTransport: each transport tried has its own HandshakeTimeout.
// The websocket attempt times out against a server that never answers its handshake, and
// Dial then succeeds over polling.
func TestDialHandshakeTimeoutPerTransport(t *testing.T) {
	srv := engineio.NewServer(&engineio.Options{Transports: []transport.Transport{polling.Default}})
	held := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("transport") != "websocket" {
			srv.ServeHTTP(w, r)
		} else if c, _, err := w.(http.Hijacker).Hijack(); err == nil {
			<-held
			_ = c.Close()
		}
	}))
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = srv.Close() })
	t.Cleanup(func() { close(held) })

	d := &Dialer{Transports: []transport.Transport{polling.Default, websocket.Default}, HandshakeTimeout: 200 * time.Millisecond}
	cl, err := d.Dial(ts.URL, nil)
	require.NoError(t, err)
	require.NoError(t, cl.Close())
}

// recordingConn is a connection that records the non-zero deadlines set through
// SetDeadline, which gobwas uses for the websocket handshake.
type recordingConn struct {
	net.Conn
	set []time.Time
}

func (c *recordingConn) SetDeadline(t time.Time) error {
	if !t.IsZero() {
		c.set = append(c.set, t)
	}
	return c.Conn.SetDeadline(t)
}

// TestDialNegativeHandshakeTimeout: a negative HandshakeTimeout means no bound; Dial waits
// for an OPEN packet that the server sends 300 ms after the upgrade, and the websocket
// handshake gets no deadline. A zero HandshakeTimeout gives the handshake one 10 s ahead.
func TestDialNegativeHandshakeTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, _, _, err := ws.UpgradeHTTP(r, w); err == nil {
			time.Sleep(300 * time.Millisecond)
			_ = wsutil.WriteServerText(c, []byte(`0{"sid":"s","upgrades":[],"pingInterval":25000,"pingTimeout":20000}`))
			_, _ = io.Copy(io.Discard, c)
			_ = c.Close()
		}
	}))
	t.Cleanup(ts.Close)
	for _, tc := range []struct{ timeout, ahead time.Duration }{{-1, 0}, {0, 10 * time.Second}} {
		rc := &recordingConn{}
		tr := &websocket.Transport{NetDial: func(network, addr string) (c net.Conn, err error) {
			rc.Conn, err = net.Dial(network, addr)
			return rc, err
		}}
		start := time.Now()
		cl, err := (&Dialer{Transports: []transport.Transport{tr}, HandshakeTimeout: tc.timeout}).Dial(ts.URL, nil)
		require.NoError(t, err)
		require.NoError(t, cl.Close())
		if tc.ahead == 0 {
			require.Empty(t, rc.set, "the handshake is bounded with HandshakeTimeout %v", tc.timeout)
			continue
		}
		require.Len(t, rc.set, 1)
		require.WithinDuration(t, start.Add(tc.ahead), rc.set[0], time.Second)
	}
}

// TestDialTransportHandshakeTimeoutShorter: a websocket.Transport whose HandshakeTimeout is
// shorter than the Dialer's fails at its own bound. The listener never accepts, so the
// connection completes in its backlog and the handshake is never answered.
func TestDialTransportHandshakeTimeoutShorter(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	tr := &websocket.Transport{HandshakeTimeout: 100 * time.Millisecond}
	start := time.Now()
	_, err = (&Dialer{Transports: []transport.Transport{tr}, HandshakeTimeout: 5 * time.Second}).Dial("http://"+l.Addr().String(), nil)
	require.Error(t, err)
	require.Less(t, time.Since(start), time.Second)
}

// TestDialHandshakeDeadline: for the wait for OPEN, a zero HandshakeTimeout arms a read
// deadline 10 s ahead and a positive one a deadline at its value; a negative one arms none.
func TestDialHandshakeDeadline(t *testing.T) {
	for _, tc := range []struct{ timeout, ahead time.Duration }{{0, 10 * time.Second}, {time.Minute, time.Minute}, {-1, 0}} {
		r := &deadlineRecorder{}
		start := time.Now()
		_, err := (&Dialer{Transports: []transport.Transport{r}, HandshakeTimeout: tc.timeout}).Dial("http://x", nil)
		require.NoError(t, err)
		if tc.ahead == 0 {
			require.Empty(t, r.set, "HandshakeTimeout %v", tc.timeout)
			continue
		}
		require.Len(t, r.set, 2, "HandshakeTimeout %v: armed, then cleared after OPEN", tc.timeout)
		require.WithinDuration(t, start.Add(tc.ahead), r.set[0], time.Second)
	}
}

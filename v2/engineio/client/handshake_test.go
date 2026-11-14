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

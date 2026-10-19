package client

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
)

type observedLimitTransport struct {
	transport.Transport
	opened chan *observedLimitConn
}

func (t observedLimitTransport) Dial(u *url.URL, h http.Header) (transport.Conn, error) {
	c, e := t.Transport.Dial(u, h)
	if e != nil {
		return nil, e
	}
	observed := &observedLimitConn{Conn: c, firstIO: make(chan int64, 1), closed: make(chan struct{}), readError: make(chan error, 1)}
	t.opened <- observed
	return observed, nil
}

type observedLimitConn struct {
	transport.Conn
	limit                atomic.Int64
	firstIO              chan int64
	closed               chan struct{}
	readError            chan error
	startOnce, closeOnce sync.Once
}

func (c *observedLimitConn) SetReadLimit(n int64) {
	c.limit.Store(n)
	c.Conn.(interface{ SetReadLimit(int64) }).SetReadLimit(n)
}
func (c *observedLimitConn) NextWriter(ft eioparser.FrameType, pt eioparser.PacketType) (io.WriteCloser, error) {
	c.startOnce.Do(func() { c.firstIO <- c.limit.Load() })
	return c.Conn.NextWriter(ft, pt)
}
func (c *observedLimitConn) NextReader() (eioparser.FrameType, eioparser.PacketType, io.ReadCloser, error) {
	ft, pt, r, e := c.Conn.NextReader()
	if e != nil {
		select {
		case c.readError <- e:
		default:
		}
	}
	return ft, pt, r, e
}
func (c *observedLimitConn) Close() error {
	e := c.Conn.Close()
	c.closeOnce.Do(func() { close(c.closed) })
	return e
}

func TestUpgradeCandidateLimitInstalledBeforeProbe(t *testing.T) {
	const limit = 512
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("transport") == "websocket" {
			ws, e := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
			if e != nil {
				return
			}
			defer func() { _ = ws.Close() }()
			_, body, e := ws.ReadMessage()
			if e != nil {
				return
			}
			if string(body) != "2probe" {
				return
			}
			_ = ws.WriteMessage(gorilla.TextMessage, []byte("3"+strings.Repeat("x", limit)))
			<-release
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		if r.URL.Query().Get("sid") == "" {
			open := `0{"sid":"probe-limit","upgrades":["websocket"],"pingInterval":3600000,"pingTimeout":20000}`
			_, _ = fmt.Fprintf(w, "%d:%s", len(open), open)
			return
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		_, _ = io.WriteString(w, "1:1")
	}))
	defer ts.Close()
	defer releaseOnce.Do(func() { close(release) })
	candidate := observedLimitTransport{Transport: websocket.Default, opened: make(chan *observedLimitConn, 1)}
	c, e := (&Dialer{MaxMessageSize: limit, Upgrade: true, UpgradeTimeout: time.Second, Transports: []transport.Transport{polling.Default, candidate}}).Dial(ts.URL, nil)
	require.NoError(t, e)
	defer func() { _ = c.Close() }()
	var probe *observedLimitConn
	select {
	case probe = <-candidate.opened:
	case <-time.After(2 * time.Second):
		t.Fatal("candidate did not dial")
	}
	select {
	case applied := <-probe.firstIO:
		require.Equal(t, int64(limit), applied, "limit must be installed before the PING writer")
	case <-time.After(time.Second):
		t.Fatal("candidate did not probe")
	}
	select {
	case <-probe.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("oversized probe candidate did not close")
	}
	select {
	case e := <-probe.readError:
		require.ErrorIs(t, e, gorilla.ErrReadLimit)
	default:
		t.Fatal("oversized candidate was not rejected at the configured read boundary")
	}
	require.Equal(t, "polling", c.Transport())
	require.False(t, c.Upgraded())
	require.Equal(t, StateOpen, c.ReadyState())
}

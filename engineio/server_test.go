package engineio_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/session"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func TestEnginePolling(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	svr := engineio.NewServer(nil)
	defer func() {
		must.NoError(svr.Close())
	}()

	httpSvr := httptest.NewServer(svr)
	defer httpSvr.Close()

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer wg.Done()

		conn, err := svr.Accept()
		must.NoError(err)
		defer func() {
			must.NoError(conn.Close())
		}()

		ft, r, err := conn.NextReader()
		must.NoError(err)
		should.Equal(session.TEXT, ft)

		b, err := io.ReadAll(r)
		must.NoError(err)
		should.Equal("hello你好", string(b))

		must.Nil(r.Close())

		w, err := conn.NextWriter(session.BINARY)
		must.NoError(err)

		_, err = w.Write([]byte{1, 2, 3, 4})
		must.NoError(err)
		must.Nil(w.Close())
	}()

	dialer := client.Dialer{
		Transports: []transport.Transport{polling.Default},
	}
	header := http.Header{}
	header.Set("X-EIO-Test", "client")

	cnt, err := dialer.Dial(httpSvr.URL, header)
	must.NoError(err)

	w, err := cnt.NextWriter(session.TEXT)
	must.NoError(err)

	_, err = w.Write([]byte("hello你好"))
	must.NoError(err)
	must.Nil(w.Close())

	ft, r, err := cnt.NextReader()
	must.NoError(err)
	should.Equal(session.BINARY, ft)

	b, err := io.ReadAll(r)
	must.NoError(err)
	should.Equal([]byte{1, 2, 3, 4}, b)

	must.Nil(r.Close())
	must.Nil(cnt.Close())

	wg.Wait()
}

func TestEngineWebsocket(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	svr := engineio.NewServer(nil)
	defer func() {
		must.NoError(svr.Close())
	}()

	httpSvr := httptest.NewServer(svr)
	defer httpSvr.Close()

	svrInfo := ""

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		conn, err := svr.Accept()
		must.NoError(err)

		defer func() {
			must.NoError(conn.Close())
		}()

		should.Equal("client", conn.RemoteHeader().Get("X-EIO-Test"))
		u := conn.URL()
		svrInfo = fmt.Sprintf("%s %s %s %s", conn.ID(), u.RawQuery, conn.RemoteAddr(), conn.LocalAddr())
		u.RawQuery = ""
		should.Equal("/", u.String())

		ft, r, err := conn.NextReader()
		must.NoError(err)

		should.Equal(session.TEXT, ft)

		b, err := io.ReadAll(r)
		must.NoError(err)

		should.Equal("hello你好", string(b))
		err = r.Close()
		must.NoError(err)

		w, err := conn.NextWriter(session.BINARY)
		must.NoError(err)

		_, err = w.Write([]byte{1, 2, 3, 4})
		must.NoError(err)
		must.Nil(w.Close())
	}()

	dialer := client.Dialer{
		Transports: []transport.Transport{websocket.Default},
	}
	header := http.Header{}
	header.Set("X-EIO-Test", "client")

	cnt, err := dialer.Dial(httpSvr.URL, header)
	must.NoError(err)

	u := strings.Replace(httpSvr.URL, "http", "ws", 1)
	ur := cnt.URL()
	cntInfo := fmt.Sprintf("%s %s %s %s", cnt.ID(), ur.RawQuery, cnt.LocalAddr(), cnt.RemoteAddr())
	ur.RawQuery = ""
	should.Equal(u, ur.String())

	w, err := cnt.NextWriter(session.TEXT)
	must.NoError(err)

	_, err = w.Write([]byte("hello你好"))
	must.NoError(err)

	err = w.Close()
	must.NoError(err)

	ft, r, err := cnt.NextReader()
	must.NoError(err)
	should.Equal(session.BINARY, ft)

	b, err := io.ReadAll(r)
	must.NoError(err)
	should.Equal([]byte{1, 2, 3, 4}, b)

	err = r.Close()
	must.NoError(err)

	must.NoError(cnt.Close())

	wg.Wait()

	should.Equal(cntInfo, svrInfo)
}

func TestEngineUpgrade(t *testing.T) {
	for _, delayed := range []bool{false, true} {
		name := "polling active"
		if delayed {
			name = "polling starts after probe"
		}
		t.Run(name, func(t *testing.T) { testEngineUpgrade(t, delayed) })
	}
}

// upgradeRoundTripper lets the upgrade test delay polling HTTP requests without
// changing the transport or server implementation.
type upgradeRoundTripper func(*http.Request) (*http.Response, error)

func (f upgradeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testEngineUpgrade(t *testing.T, delayedPolling bool) {
	should := assert.New(t)
	must := require.New(t)

	svr := engineio.NewServer(nil)
	defer func() {
		must.NoError(svr.Close())
	}()

	httpSvr := httptest.NewServer(svr)
	defer httpSvr.Close()

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		conn, err := svr.Accept()
		must.NoError(err)
		defer func() {
			must.NoError(conn.Close())
		}()

		ft, r, err := conn.NextReader()
		must.NoError(err)
		should.Equal(session.TEXT, ft)

		b, err := io.ReadAll(r)
		must.NoError(err)
		should.Equal("hello你好", string(b))

		must.NoError(r.Close())

		w, err := conn.NextWriter(session.BINARY)
		must.NoError(err)

		_, err = w.Write([]byte{1, 2, 3, 4})
		must.NoError(err)
		must.NoError(w.Close())
	}()

	u, err := url.Parse(httpSvr.URL)
	must.NoError(err)

	query := u.Query()
	query.Set("EIO", "3")
	u.RawQuery = query.Encode()

	// Open starts serveGet independently of the polling reader. Holding its
	// requests until PONG reproduces a client whose first poll is scheduled late.
	pollGate := make(chan struct{})
	var releaseOnce sync.Once
	releasePolling := func() { releaseOnce.Do(func() { close(pollGate) }) }
	defer releasePolling()
	if !delayedPolling {
		releasePolling()
	}
	tr := &polling.Transport{Client: &http.Client{
		Timeout: 5 * time.Second,
		Transport: upgradeRoundTripper(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodGet && req.URL.Query().Get("sid") != "" {
				select {
				case <-pollGate:
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
			}
			return http.DefaultTransport.RoundTrip(req)
		}),
	}}
	p, err := tr.Dial(u, nil)
	must.NoError(err)
	defer func() { _ = p.Close() }()

	params, err := p.(client.Opener).Open()
	must.NoError(err)

	upU := *u
	upU.Scheme = "ws"
	query = upU.Query()
	query.Set("sid", params.SID)
	upU.RawQuery = query.Encode()

	ws, err := websocket.Default.Dial(&upU, nil)
	must.NoError(err)
	defer func() { _ = ws.Close() }()

	w, err := ws.NextWriter(frame.String, packet.PING)
	must.NoError(err)

	_, err = w.Write([]byte("probe"))
	must.NoError(err)

	must.NoError(w.Close())

	ft, pt, r, err := ws.NextReader()
	must.NoError(err)

	should.Equal(frame.String, ft)
	should.Equal(packet.PONG, pt)

	b, err := io.ReadAll(r)
	must.NoError(err)

	should.Equal("probe", string(b))

	must.NoError(r.Close())

	// PONG only confirms the probe; it does not prove that a polling GET
	// has reached the server. Read NOOP and stop polling before UPGRADE, or
	// the server may reject that late GET and the reader may never signal done.
	releasePolling()
	pollDone := make(chan error, 1)
	go func() {
		ft, pt, r, err := p.NextReader()
		if err != nil {
			pollDone <- fmt.Errorf("read polling NOOP: %w", err)
			return
		}
		closeErr := r.Close()
		if ft != frame.String || pt != packet.NOOP {
			pollDone <- fmt.Errorf("expected polling NOOP (%v, %v), got (%v, %v)", frame.String, packet.NOOP, ft, pt)
			return
		}
		pollDone <- closeErr
	}()
	select {
	case err := <-pollDone:
		must.NoError(err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for polling NOOP before UPGRADE")
	}
	must.NoError(p.Close())

	w, err = ws.NextWriter(frame.String, packet.UPGRADE)
	must.NoError(err)

	must.NoError(w.Close())

	w, err = ws.NextWriter(frame.String, packet.MESSAGE)
	must.NoError(err)

	_, err = w.Write([]byte("hello你好"))
	must.NoError(err)

	must.Nil(w.Close())

	ft, pt, r, err = ws.NextReader()
	must.NoError(err)

	should.Equal(frame.Binary, ft)
	should.Equal(packet.MESSAGE, pt)

	b, err = io.ReadAll(r)
	must.NoError(err)

	must.NoError(r.Close())
	should.Equal([]byte{1, 2, 3, 4}, b)

	wg.Wait()

	must.NoError(ws.Close())
}

// TestEngineRejectsTransportDowngrade checks that once a session has been
// upgraded to websocket, a polling request carrying the same sid is refused
// with 400 instead of being treated as an "upgrade" back to polling, which
// left the request blocked until pingTimeout.
func TestEngineRejectsTransportDowngrade(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	svr := engineio.NewServer(nil)
	defer func() {
		must.NoError(svr.Close())
	}()

	httpSvr := httptest.NewServer(svr)
	defer httpSvr.Close()

	u, err := url.Parse(httpSvr.URL)
	must.NoError(err)

	query := u.Query()
	query.Set("EIO", "3")
	u.RawQuery = query.Encode()

	p, err := polling.Default.Dial(u, nil)
	must.NoError(err)
	defer func() { _ = p.Close() }()

	params, err := p.(client.Opener).Open()
	must.NoError(err)

	// Take the session out of the accept queue as the other tests do: it
	// orders Server.Close after newSession has handed the session over, and
	// it cannot block here because the handshake above has already created it.
	conn, err := svr.Accept()
	must.NoError(err)
	defer func() { _ = conn.Close() }()

	// Drain the NOOP the server sends to the polling connection while pausing it.
	go func() {
		_, _, r, err := p.NextReader()
		if err == nil {
			_ = r.Close()
		}
	}()

	upU := *u
	upU.Scheme = "ws"
	query = upU.Query()
	query.Set("sid", params.SID)
	upU.RawQuery = query.Encode()

	ws, err := websocket.Default.Dial(&upU, nil)
	must.NoError(err)
	defer func() { _ = ws.Close() }()

	w, err := ws.NextWriter(frame.String, packet.PING)
	must.NoError(err)
	_, err = w.Write([]byte("probe"))
	must.NoError(err)
	must.NoError(w.Close())

	_, pt, r, err := ws.NextReader()
	must.NoError(err)
	should.Equal(packet.PONG, pt)
	must.NoError(r.Close())

	w, err = ws.NextWriter(frame.String, packet.UPGRADE)
	must.NoError(err)
	must.NoError(w.Close())

	// The session now runs on websocket. A polling request with its sid must
	// be rejected promptly rather than held open.
	pollURL := *u
	query = pollURL.Query()
	query.Set("sid", params.SID)
	query.Set("transport", "polling")
	pollURL.RawQuery = query.Encode()

	hc := &http.Client{Timeout: 2 * time.Second}
	var status int
	rejected := assert.Eventually(t, func() bool {
		resp, err := hc.Get(pollURL.String())
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		status = resp.StatusCode
		return status == http.StatusBadRequest
	}, 5*time.Second, 50*time.Millisecond)
	if !rejected {
		t.Fatalf("polling request after upgrade was not rejected; last status %d", status)
	}
}

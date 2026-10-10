package websocket

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// wsURL returns the ws:// form of an httptest server URL.
func wsURL(t *testing.T, s *httptest.Server) *url.URL {
	t.Helper()
	u, err := url.Parse(s.URL)
	require.NoError(t, err)
	u.Scheme = "ws"
	return u
}

// connPair dials a server running tr and returns the client and server ends.
func connPair(t *testing.T, tr *Transport) (client, server transport.Conn) {
	t.Helper()
	accepted := make(chan transport.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := tr.Accept(w, r)
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		accepted <- c
	}))
	t.Cleanup(s.Close)
	client, err := tr.Dial(wsURL(t, s), nil)
	require.NoError(t, err)
	server = <-accepted
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return client, server
}

func sendPacket(t *testing.T, c transport.Conn, ft frame.Type, pt packet.Type, data []byte) {
	t.Helper()
	w, err := c.NextWriter(ft, pt)
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

func readPacket(t *testing.T, c transport.Conn) (frame.Type, packet.Type, []byte) {
	t.Helper()
	ft, pt, r, err := c.NextReader()
	require.NoError(t, err)
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return ft, pt, b
}

// TestWireFormat checks the bytes of Engine.IO v4 messages as a raw peer sees them:
// text packets carry a type byte, binary packets are raw, and a client masks.
func TestWireFormat(t *testing.T) {
	accepted := make(chan transport.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&Transport{}).Accept(w, r)
		if err == nil {
			accepted <- c
		}
	}))
	defer s.Close()

	raw, br, _, err := ws.Dial(context.Background(), wsURL(t, s).String())
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()
	sc := <-accepted
	defer func() { _ = sc.Close() }()
	var src io.Reader = raw
	if br != nil {
		src = br
	}

	sendPacket(t, sc, frame.String, packet.MESSAGE, []byte("hello"))
	sendPacket(t, sc, frame.Binary, packet.MESSAGE, []byte{0, 1, 2})
	sendPacket(t, sc, frame.String, packet.PING, nil)
	for _, want := range []struct {
		op   ws.OpCode
		body string
	}{{ws.OpText, "4hello"}, {ws.OpBinary, "\x00\x01\x02"}, {ws.OpText, "2"}} {
		f, err := ws.ReadFrame(src)
		require.NoError(t, err)
		assert.Equal(t, want.op, f.Header.OpCode)
		assert.True(t, f.Header.Fin)
		assert.False(t, f.Header.Masked, "server frames are not masked")
		assert.Equal(t, want.body, string(f.Payload))
	}

	// A client sends one masked message per packet; "b" + base64 is a binary packet.
	require.NoError(t, wsutil.WriteClientText(raw, []byte("bAAEC")))
	ft, pt, data := readPacket(t, sc)
	assert.Equal(t, frame.Binary, ft)
	assert.Equal(t, packet.MESSAGE, pt)
	assert.Equal(t, []byte{0, 1, 2}, data)
}

func TestInvalidPacketClosesWith1002(t *testing.T) {
	for name, body := range map[string][]byte{"empty text": {}, "bad type": []byte("x1"), "bad base64": []byte("b%%")} {
		t.Run(name, func(t *testing.T) {
			accepted := make(chan transport.Conn, 1)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c, err := (&Transport{}).Accept(w, r); err == nil {
					accepted <- c
				}
			}))
			defer s.Close()
			raw, _, _, err := ws.Dial(context.Background(), wsURL(t, s).String())
			require.NoError(t, err)
			defer func() { _ = raw.Close() }()
			sc := <-accepted

			require.NoError(t, wsutil.WriteClientText(raw, body))
			_, _, _, err = sc.NextReader()
			require.ErrorIs(t, err, ErrInvalidPacket)
			require.NoError(t, raw.SetReadDeadline(time.Now().Add(5*time.Second)))
			f, err := ws.ReadFrame(raw)
			require.NoError(t, err)
			code, _ := ws.ParseCloseFrameData(f.Payload)
			assert.Equal(t, ws.OpClose, f.Header.OpCode)
			assert.Equal(t, ws.StatusProtocolError, code)
			_, err = ws.ReadFrame(raw)
			require.Error(t, err, "the close frame is followed by the end of the stream")
		})
	}
}

func TestMaxPayload(t *testing.T) {
	tr := &Transport{MaxPayload: 16}
	cc, sc := connPair(t, tr)

	// A write over the limit is a local error: nothing is sent, the connection lives.
	w, err := cc.NextWriter(frame.String, packet.MESSAGE)
	require.NoError(t, err)
	_, err = w.Write(bytes.Repeat([]byte("x"), 16)) // plus the type byte
	require.NoError(t, err)
	require.ErrorIs(t, w.Close(), ErrTooLarge)
	w, err = cc.NextWriter(frame.String, packet.MESSAGE)
	require.NoError(t, err)
	_, err = w.Write(bytes.Repeat([]byte("x"), 17))
	require.ErrorIs(t, err, ErrTooLarge)
	require.ErrorIs(t, w.Close(), ErrTooLarge)

	sendPacket(t, cc, frame.String, packet.MESSAGE, bytes.Repeat([]byte("y"), 15))
	_, _, data := readPacket(t, sc)
	assert.Len(t, data, 15)

	// A peer that sends more is cut off with 1009.
	accepted := make(chan transport.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := tr.Accept(w, r); err == nil {
			accepted <- c
		}
	}))
	defer s.Close()
	raw, _, _, err := ws.Dial(context.Background(), wsURL(t, s).String())
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()
	srv := <-accepted
	require.NoError(t, wsutil.WriteClientBinary(raw, bytes.Repeat([]byte{1}, 17)))
	_, _, _, err = srv.NextReader()
	require.ErrorIs(t, err, ErrTooLarge)
	require.NoError(t, raw.SetReadDeadline(time.Now().Add(5*time.Second)))
	f, err := ws.ReadFrame(raw)
	require.NoError(t, err)
	code, _ := ws.ParseCloseFrameData(f.Payload)
	assert.Equal(t, ws.StatusMessageTooBig, code)
}

// A peer that is still sending when it is cut off must receive the close frame:
// the server half-closes and drains instead of resetting the connection with
// unread data in its receive buffer, also when the consumer calls Close as soon as
// the read error is returned.
func TestCloseFrameSurvivesUnreadData(t *testing.T) {
	accepted := make(chan transport.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := (&Transport{MaxPayload: 16}).Accept(w, r); err == nil {
			accepted <- c
		}
	}))
	defer s.Close()
	raw, _, _, err := ws.Dial(context.Background(), wsURL(t, s).String())
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()
	srv := <-accepted
	defer func() { _ = srv.Close() }()

	go func() { _ = wsutil.WriteClientBinary(raw, bytes.Repeat([]byte{1}, 256<<10)) }()
	_, _, _, err = srv.NextReader()
	require.ErrorIs(t, err, ErrTooLarge)
	// The consumer closes on the read error, as engineio.Session does; the close
	// frame must still reach the peer.
	require.NoError(t, srv.Close())
	require.NoError(t, raw.SetReadDeadline(time.Now().Add(5*time.Second)))
	f, err := ws.ReadFrame(raw)
	require.NoError(t, err)
	code, _ := ws.ParseCloseFrameData(f.Payload)
	assert.Equal(t, ws.StatusMessageTooBig, code)
	_, err = ws.ReadFrame(raw)
	assert.ErrorIs(t, err, io.EOF)

	// The reads and writes of the failed connection stop at once.
	_, _, _, err = srv.NextReader()
	require.ErrorIs(t, err, net.ErrClosed)
	w, err := srv.NextWriter(frame.String, packet.MESSAGE)
	require.NoError(t, err)
	require.ErrorIs(t, w.Close(), net.ErrClosed)
}

// TestBufferSizes sends messages larger than WriteBufferSize, which wsutil splits
// into fragments, to a reader with a small ReadBufferSize.
func TestBufferSizes(t *testing.T) {
	cc, sc := connPair(t, &Transport{ReadBufferSize: 64, WriteBufferSize: 100})
	for _, size := range []int{0, 1, 99, 100, 101, 5000, 100000} {
		data := bytes.Repeat([]byte("é"), size/2)
		sendPacket(t, cc, frame.String, packet.MESSAGE, data)
		ft, pt, got := readPacket(t, sc)
		assert.Equal(t, frame.String, ft)
		assert.Equal(t, packet.MESSAGE, pt)
		assert.Equal(t, data, got, "size %d", size)
		sendPacket(t, sc, frame.Binary, packet.MESSAGE, data)
		ft, _, got = readPacket(t, cc)
		assert.Equal(t, frame.Binary, ft)
		assert.Equal(t, data, got, "size %d", size)
	}
}

func TestConcurrentWritersKeepPacketsWhole(t *testing.T) {
	cc, sc := connPair(t, &Transport{})
	const n = 64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sendPacket(t, cc, frame.String, packet.MESSAGE, bytes.Repeat([]byte("a"), 3000))
		}()
	}
	for i := 0; i < n; i++ {
		_, _, data := readPacket(t, sc)
		require.Equal(t, bytes.Repeat([]byte("a"), 3000), data)
	}
	wg.Wait()
}

func TestWriterErrors(t *testing.T) {
	cc, _ := connPair(t, &Transport{})
	_, err := cc.NextWriter(frame.Type(9), packet.MESSAGE)
	require.ErrorIs(t, err, transport.ErrInvalidFrame)

	// The v4 binary form cannot carry a type: the connection stays open.
	w, err := cc.NextWriter(frame.Binary, packet.PING)
	require.NoError(t, err)
	require.ErrorIs(t, w.Close(), ErrInvalidPacket)
	require.NoError(t, w.Close(), "second Close")
	_, err = w.Write([]byte("x"))
	require.Error(t, err)

	require.NoError(t, cc.Close())
	w, err = cc.NextWriter(frame.String, packet.MESSAGE)
	require.NoError(t, err)
	require.ErrorIs(t, w.Close(), net.ErrClosed)
}

// A close frame from the peer is answered with its status and surfaces as a
// wsutil.ClosedError, which the client's ping loop logs at DEBUG.
func TestPeerCloseFrame(t *testing.T) {
	accepted := make(chan transport.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := (&Transport{}).Accept(w, r); err == nil {
			accepted <- c
		}
	}))
	defer s.Close()
	raw, _, _, err := ws.Dial(context.Background(), wsURL(t, s).String())
	require.NoError(t, err)
	defer func() { _ = raw.Close() }()
	sc := <-accepted

	body := ws.NewCloseFrameBody(ws.StatusGoingAway, "")
	require.NoError(t, ws.WriteFrame(raw, ws.MaskFrameInPlace(ws.NewCloseFrame(body))))
	_, _, _, err = sc.NextReader()
	var closed wsutil.ClosedError
	require.ErrorAs(t, err, &closed)
	assert.Equal(t, ws.StatusGoingAway, closed.Code)
	require.NoError(t, raw.SetReadDeadline(time.Now().Add(5*time.Second)))
	f, err := ws.ReadFrame(raw)
	require.NoError(t, err)
	code, _ := ws.ParseCloseFrameData(f.Payload)
	assert.Equal(t, ws.OpClose, f.Header.OpCode)
	assert.Equal(t, ws.StatusGoingAway, code)
	_, _, _, err = sc.NextReader()
	require.Error(t, err, "the connection is closed after the reply")
}

func TestOrigin(t *testing.T) {
	for name, tc := range map[string]struct {
		tr     *Transport
		origin string
		ok     bool
	}{
		"no origin":        {&Transport{}, "", true},
		"same origin":      {&Transport{}, "http://%s", true},
		"other origin":     {&Transport{}, "http://example.org", false},
		"malformed origin": {&Transport{}, "://", false},
		"allowed by hook":  {&Transport{CheckOrigin: func(*http.Request) bool { return true }}, "http://example.org", true},
		"denied by hook":   {&Transport{CheckOrigin: func(*http.Request) bool { return false }}, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			var acceptErr error
			var wg sync.WaitGroup
			wg.Add(1)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer wg.Done()
				c, err := tc.tr.Accept(w, r)
				acceptErr = err
				if err == nil {
					_ = c.Close()
				}
			}))
			defer s.Close()
			origin := strings.ReplaceAll(tc.origin, "%s", strings.TrimPrefix(s.URL, "http://"))
			header := make(http.Header)
			if origin != "" {
				header.Set("Origin", origin)
			}
			c, err := tc.tr.Dial(wsURL(t, s), header)
			wg.Wait()
			if tc.ok {
				require.NoError(t, err)
				require.NoError(t, acceptErr)
				_ = c.Close()
				return
			}
			var hs HandshakeError
			require.ErrorAs(t, acceptErr, &hs)
			var de DialError
			require.ErrorAs(t, err, &de)
			require.NotNil(t, de.Response)
			assert.Equal(t, http.StatusForbidden, de.Response.StatusCode)
		})
	}
}

// plainWriter hides http.Hijacker, as a wrapping ResponseWriter or HTTP/2 does.
type plainWriter struct{ http.ResponseWriter }

func TestAcceptWithoutHijacker(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/engine.io/?transport=websocket", nil)
	c, err := (&Transport{}).Accept(plainWriter{rec}, r)
	require.ErrorIs(t, err, ErrNotHijacker)
	assert.Nil(t, c)
	assert.Empty(t, rec.Body.String(), "Accept must leave the answer to the caller")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBadHandshakeIsAnswered(t *testing.T) {
	var gotErr error
	done := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		_, gotErr = (&Transport{}).Accept(w, r)
	}))
	defer s.Close()
	resp, err := http.Get(s.URL) // a plain GET, not an upgrade
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	<-done
	var hs HandshakeError
	require.ErrorAs(t, gotErr, &hs)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestDialRejectedKeepsResponse(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Reason", "nope")
		http.Error(w, "go away", http.StatusTeapot)
	}))
	defer s.Close()
	_, err := (&Transport{}).Dial(wsURL(t, s), nil)
	var de DialError
	require.ErrorAs(t, err, &de)
	require.NotNil(t, de.Response)
	assert.Equal(t, http.StatusTeapot, de.Response.StatusCode)
	assert.Equal(t, "nope", de.Response.Header.Get("X-Reason"))
	body, _ := io.ReadAll(de.Response.Body)
	assert.Equal(t, "go away\n", string(body))
}

func TestDialHeadersAndHost(t *testing.T) {
	got := make(chan *http.Request, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r
		w.Header().Set("Set-Cookie", "a=b")
		c, err := (&Transport{}).Accept(w, r)
		if err == nil {
			_ = c.Close()
		}
	}))
	defer s.Close()
	header := http.Header{"Host": {"virtual.example"}, "X-Custom": {"1"}}
	c, err := (&Transport{}).Dial(wsURL(t, s), header)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	r := <-got
	assert.Equal(t, "virtual.example", r.Host)
	assert.Equal(t, "1", r.Header.Get("X-Custom"))
	assert.Equal(t, "a=b", c.RemoteHeader().Get("Set-Cookie"))
	assert.Equal(t, "websocket", r.URL.Query().Get("transport"))
	assert.Equal(t, http.Header{"Host": {"virtual.example"}, "X-Custom": {"1"}}, header, "the caller's header is not modified")
}

func TestNetDial(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := (&Transport{}).Accept(w, r); err == nil {
			_ = c.Close()
		}
	}))
	defer s.Close()
	var dialed string
	tr := &Transport{NetDial: func(network, addr string) (net.Conn, error) {
		dialed = addr
		return net.Dial(network, addr)
	}}
	c, err := tr.Dial(wsURL(t, s), nil)
	require.NoError(t, err)
	_ = c.Close()
	assert.Equal(t, strings.TrimPrefix(s.URL, "http://"), dialed)
}

// connectProxy is a minimal CONNECT proxy; it records the requests it served.
func connectProxy(t *testing.T, status int) (addr string, requests <-chan *http.Request) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	reqs := make(chan *http.Request, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				reqs <- req
				if status != http.StatusOK {
					_, _ = io.WriteString(c, fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", status, http.StatusText(status)))
					return
				}
				up, err := net.Dial("tcp", req.Host)
				if err != nil {
					return
				}
				defer func() { _ = up.Close() }()
				_, _ = io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
				go func() { _, _ = io.Copy(up, br) }()
				_, _ = io.Copy(c, up)
			}()
		}
	}()
	return l.Addr().String(), reqs
}

func TestProxy(t *testing.T) {
	accepted := make(chan transport.Conn, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := (&Transport{}).Accept(w, r); err == nil {
			accepted <- c
		}
	}))
	defer s.Close()
	target := strings.TrimPrefix(s.URL, "http://")

	t.Run("tunnel", func(t *testing.T) {
		proxyAddr, reqs := connectProxy(t, http.StatusOK)
		var seen *http.Request
		tr := &Transport{Proxy: func(r *http.Request) (*url.URL, error) {
			seen = r
			return &url.URL{Scheme: "http", Host: proxyAddr, User: url.UserPassword("u", "p")}, nil
		}}
		cc, err := tr.Dial(wsURL(t, s), nil)
		require.NoError(t, err)
		defer func() { _ = cc.Close() }()
		sc := <-accepted
		defer func() { _ = sc.Close() }()
		assert.Equal(t, "http", seen.URL.Scheme)
		req := <-reqs
		assert.Equal(t, http.MethodConnect, req.Method)
		assert.Equal(t, target, req.Host)
		assert.Equal(t, "Basic dTpw", req.Header.Get("Proxy-Authorization"))
		sendPacket(t, cc, frame.String, packet.MESSAGE, []byte("through"))
		_, _, data := readPacket(t, sc)
		assert.Equal(t, "through", string(data))
	})
	t.Run("no proxy for this request", func(t *testing.T) {
		tr := &Transport{Proxy: func(*http.Request) (*url.URL, error) { return nil, nil }}
		cc, err := tr.Dial(wsURL(t, s), nil)
		require.NoError(t, err)
		_ = cc.Close()
		_ = (<-accepted).Close()
	})
	t.Run("refused", func(t *testing.T) {
		proxyAddr, _ := connectProxy(t, http.StatusForbidden)
		tr := &Transport{Proxy: func(*http.Request) (*url.URL, error) { return &url.URL{Scheme: "http", Host: proxyAddr}, nil }}
		_, err := tr.Dial(wsURL(t, s), nil)
		require.ErrorContains(t, err, "proxy refused CONNECT")
	})
	t.Run("unsupported scheme", func(t *testing.T) {
		tr := &Transport{Proxy: func(*http.Request) (*url.URL, error) { return &url.URL{Scheme: "socks5", Host: "127.0.0.1:1"}, nil }}
		_, err := tr.Dial(wsURL(t, s), nil)
		require.ErrorContains(t, err, `unsupported proxy scheme "socks5"`)
	})
	t.Run("chooser error", func(t *testing.T) {
		boom := errors.New("boom")
		tr := &Transport{Proxy: func(*http.Request) (*url.URL, error) { return nil, boom }}
		_, err := tr.Dial(wsURL(t, s), nil)
		require.ErrorIs(t, err, boom)
	})
}

func TestHandshakeTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	go func() { // accepts and never answers
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			defer func() { _ = c.Close() }()
		}
	}()
	start := time.Now()
	_, err = (&Transport{HandshakeTimeout: 100 * time.Millisecond}).Dial(&url.URL{Scheme: "ws", Host: l.Addr().String()}, nil)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second)
}

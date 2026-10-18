package socketio

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

func handshakeRaw(t *testing.T, target string, headers http.Header, tr transport.Transport) *rawClient {
	t.Helper()
	d := engineclient.Dialer{Transports: []transport.Transport{tr}}
	c, err := d.Dial(target, headers)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return &rawClient{t: t, conn: c}
}

// Covers K4
func TestConnEngineConn(t *testing.T) {
	p := start(t, 'S', hooks{})
	require.Same(t, p.fc, p.nc.EngineConn())
	require.Equal(t, engineio.StateOpen, p.nc.EngineConn().ReadyState())
	require.Equal(t, "fake", p.nc.EngineConn().Transport())
	require.False(t, p.nc.EngineConn().Upgraded())
}

// Covers K5
func TestConnRequest(t *testing.T) {
	seen := make(chan Conn, 40)
	check := func(c Conn) error {
		r := c.Request()
		if r == nil {
			return fmt.Errorf("nil request in %s", c.Namespace())
		}
		if r.Method != http.MethodGet || r.URL.Path != "/handshake" || r.URL.Query().Get("marker") != "kept" || r.Header.Get("X-Handshake") != "original" {
			return fmt.Errorf("wrong request in %s: %s %s %v", c.Namespace(), r.Method, r.URL, r.Header)
		}
		seen <- c
		return nil
	}
	var srv *Server
	ts := newTestServer(t, func(s *Server) {
		srv = s
		s.OnConnect("/", check)
		s.Of("/chat").Use(check)
	})
	var retained []Conn
	for i := 0; i < 20; i++ {
		c := handshakeRaw(t, ts.URL+"/handshake?marker=kept", http.Header{"X-Handshake": []string{"original"}}, polling.Default)
		require.Equal(t, "0", c.read())
		root := recv(t, seen, "root request")
		c.send("0/chat,")
		require.Equal(t, "0/chat,", c.read())
		chat := recv(t, seen, "middleware request")
		require.Same(t, root.Request(), chat.Request())
		retained = append(retained, root, chat)
	}
	require.NoError(t, srv.Close())
	for _, c := range retained {
		require.NotNil(t, c.Request())
		require.Equal(t, "original", c.Request().Header.Get("X-Handshake"))
	}
	fake := start(t, 'S', hooks{})
	require.Nil(t, fake.nc.Request())
	require.Equal(t, Handshake{}, fake.nc.Handshake())
}

// Covers N8
func TestNamespaceConnectQuery(t *testing.T) {
	query := make(chan string, 1)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(Conn) error { return nil })
		s.Of("/chat").Use(func(c Conn) error { query <- c.Handshake().Query.Get("x"); return nil })
	})
	c := dialRaw(t, ts.URL)
	require.Equal(t, "0", c.read())
	c.send("0/chat?x=1,")
	require.Equal(t, "0/chat,", c.read())
	require.Equal(t, "1", recv(t, query, "namespace query"))
}

// Covers K7
func TestHandshakeQuery(t *testing.T) {
	seen := make(chan Conn, 2)
	ts := newTestServer(t, func(s *Server) {
		s.OnConnect("/", func(c Conn) error { seen <- c; return nil })
		s.OnConnect("/chat", func(c Conn) error { seen <- c; return nil })
	})
	c := handshakeRaw(t, ts.URL+"/?engine=kept&shared=engine", nil, polling.Default)
	require.Equal(t, "0", c.read())
	root := recv(t, seen, "root handshake")
	c.send("0/chat?shared=namespace&tag=one&tag=two,")
	require.Equal(t, "0/chat,", c.read())
	chat := recv(t, seen, "chat handshake")
	query := chat.Handshake().Query
	require.Equal(t, "kept", query.Get("engine"))
	require.Equal(t, "namespace", query.Get("shared"))
	require.Equal(t, []string{"one", "two"}, query["tag"])
	require.Equal(t, "engine", root.Handshake().Query.Get("shared"))
	query.Set("shared", "changed")
	require.Equal(t, "namespace", chat.Handshake().Query.Get("shared"), "returned metadata does not mutate the stored handshake")
}

// Covers K10
func TestHandshakeData(t *testing.T) {
	for _, secure := range []bool{false, true} {
		for _, cross := range []bool{false, true} {
			name := fmt.Sprintf("tls=%t/origin=%t", secure, cross)
			t.Run(name, func(t *testing.T) {
				seen := make(chan Conn, 2)
				before := time.Now()
				s := NewServer(WithEngineOptions(shortPingOptions()...))
				check := func(c Conn) error {
					h, r := c.Handshake(), c.Request()
					if r == nil || h.Secure != secure || h.XDomain != cross || h.Address != r.RemoteAddr || h.Headers.Get("X-Handshake") != "original" || h.URL.Path != "/metadata" || h.Time.Before(before) || h.Time.After(time.Now()) {
						return fmt.Errorf("invalid handshake: %+v", h)
					}
					seen <- c
					return nil
				}
				s.OnConnect("/", check)
				s.Of("/chat").Use(check)
				go func() { _ = s.Serve() }()
				var ts *httptest.Server
				if secure {
					ts = httptest.NewTLSServer(s)
				} else {
					ts = httptest.NewServer(s)
				}
				t.Cleanup(func() { _ = s.Close(); ts.Close() })
				headers := http.Header{"X-Handshake": []string{"original"}}
				if cross {
					headers.Set("Origin", "https://elsewhere.test")
				}
				c := handshakeRaw(t, ts.URL+"/metadata", headers, &polling.Transport{Client: ts.Client()})
				require.Equal(t, "0", c.read())
				root := recv(t, seen, "root metadata")
				c.send("0/chat,")
				require.Equal(t, "0/chat,", c.read())
				chat := recv(t, seen, "middleware metadata")
				require.Same(t, root.Request(), chat.Request())
			})
		}
	}
	t.Run("namespace after upgrade keeps polling request", testHandshakeAfterUpgrade)
}

func testHandshakeAfterUpgrade(t *testing.T) {
	seen := make(chan Conn, 2)
	var srv *Server
	ts := newTestServer(t, func(s *Server) {
		srv = s
		s.OnConnect("/", func(c Conn) error { seen <- c; return nil })
		s.Of("/chat").Use(func(c Conn) error { seen <- c; return nil })
	})
	client := &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	get := func(target string) string {
		t.Helper()
		r, err := http.NewRequest(http.MethodGet, target, nil)
		require.NoError(t, err)
		r.Header.Set("X-Handshake", "polling")
		r.Header.Set("Origin", "https://original.test")
		response, err := client.Do(r)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		require.Equal(t, 200, response.StatusCode)
		data, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		return string(data)
	}
	body := get(ts.URL + "/?EIO=3&transport=polling&b64=1&shared=polling&initial=yes")
	_, open, ok := strings.Cut(body, ":")
	require.True(t, ok)
	params, err := eioparser.ReadConnParameters(strings.NewReader(open[1:]))
	require.NoError(t, err)
	poll := ts.URL + "/?EIO=3&transport=polling&b64=1&sid=" + params.SID
	require.Equal(t, "2:40", get(poll))
	root := recv(t, seen, "polling root")
	ws, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket&sid="+params.SID+"&upgrade=only", http.Header{"X-Upgrade": []string{"websocket"}, "Origin": []string{ts.URL}})
	require.NoError(t, err)
	defer func() { _ = ws.Close() }()
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(waitFor)))
	require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("2probe")))
	_, packet, err := ws.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "3probe", string(packet))
	require.Equal(t, "1:6", get(poll))
	require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("5")))
	require.Eventually(t, func() bool { return root.EngineConn().Upgraded() }, time.Second, time.Millisecond)
	require.NoError(t, ws.WriteMessage(gorilla.TextMessage, []byte("40/chat?shared=namespace,")))
	_, packet, err = ws.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "40/chat,", string(packet))
	chat := recv(t, seen, "chat after upgrade")
	h := chat.Handshake()
	require.Same(t, root.Request(), chat.Request())
	require.Equal(t, "polling", h.Headers.Get("X-Handshake"))
	require.Empty(t, h.Headers.Get("X-Upgrade"))
	require.Equal(t, root.Request().RemoteAddr, h.Address)
	require.Equal(t, root.Request().URL.RawQuery, h.URL.RawQuery)
	require.Empty(t, h.Query.Get("upgrade"))
	require.Equal(t, "namespace", h.Query.Get("shared"))
	require.Equal(t, "yes", h.Query.Get("initial"))
	require.Equal(t, "websocket", chat.EngineConn().Transport())
	require.True(t, chat.EngineConn().Upgraded())
	require.Equal(t, 1, srv.Count())
}

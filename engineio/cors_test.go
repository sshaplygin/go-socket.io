package engineio

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

const foreignOrigin = "https://elsewhere.test"

func corsRequest(t *testing.T, client *http.Client, method, target, origin, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	require.NoError(t, err)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	}
	response, err := client.Do(req)
	require.NoError(t, err)
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	return response, string(data)
}

// corsSession exercises every successful polling response method and both
// Origin header forms. It returns a live polling session for upgrade checks.
func corsSession(t *testing.T, opts []Option, origin string, allowed bool) *parityWire {
	t.Helper()
	f := &parityWire{srv: NewServer(opts...), client: &http.Client{Timeout: 2 * time.Second}}
	f.http = httptest.NewServer(f.srv)
	t.Cleanup(func() { _ = f.srv.Close(); f.http.Close(); f.client.CloseIdleConnections() })
	check := func(response *http.Response) {
		t.Helper()
		require.Equal(t, http.StatusOK, response.StatusCode)
		wantOrigin, credentials := "", ""
		if allowed {
			wantOrigin = origin
			if origin == "" {
				wantOrigin = "*"
			} else {
				credentials = "true"
			}
		}
		require.Equal(t, wantOrigin, response.Header.Get("Access-Control-Allow-Origin"))
		require.Equal(t, credentials, response.Header.Get("Access-Control-Allow-Credentials"))
	}
	response, body := corsRequest(t, f.client, http.MethodGet, f.http.URL+"/?EIO=3&transport=polling&b64=1", origin, "")
	check(response)
	_, open, ok := strings.Cut(body, ":")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(open, "0"), open)
	var err error
	f.params, err = eioparser.ReadConnParameters(strings.NewReader(open[1:]))
	require.NoError(t, err)
	f.poll = f.http.URL + "/?EIO=3&transport=polling&b64=1&sid=" + f.params.SID
	accepted := make(chan Conn, 1)
	go func() { c, _ := f.srv.Accept(); accepted <- c }()
	f.conn = hookReceive(t, accepted)
	require.NotNil(t, f.conn)
	reads := parityReader(f.conn)
	written := parityWrite(f.conn, "out")
	response, body = corsRequest(t, f.client, http.MethodGet, f.poll, origin, "")
	check(response)
	require.Equal(t, "4:4out", body)
	require.NoError(t, hookReceive(t, written))
	response, body = corsRequest(t, f.client, http.MethodPost, f.poll, origin, "3:4in")
	check(response)
	require.Equal(t, "ok", body)
	parityMessage(t, reads, "in")
	response, body = corsRequest(t, f.client, http.MethodOptions, f.poll, origin, "")
	check(response)
	require.Empty(t, body)
	require.Equal(t, "Content-Type", response.Header.Get("Access-Control-Allow-Headers"))
	return f
}

func corsWebsocket(t *testing.T, f *parityWire, origin string, upgrade, accepted bool) {
	t.Helper()
	target := "ws" + strings.TrimPrefix(f.http.URL, "http") + "/?EIO=3&transport=websocket"
	if upgrade {
		target += "&sid=" + f.params.SID
	}
	headers := http.Header{}
	if origin != "" {
		headers.Set("Origin", origin)
	}
	c, response, err := gorilla.DefaultDialer.Dial(target, headers)
	if !accepted {
		require.Error(t, err)
		require.NotNil(t, response)
		require.Equal(t, http.StatusForbidden, response.StatusCode)
		require.NoError(t, response.Body.Close())
		return
	}
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)
	require.NoError(t, c.SetReadDeadline(time.Now().Add(2*time.Second)))
	if upgrade {
		require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("2probe")))
	}
	_, data, err := c.ReadMessage()
	require.NoError(t, err)
	if upgrade {
		require.Equal(t, "3probe", string(data))
	} else {
		require.True(t, strings.HasPrefix(string(data), "0{"), string(data))
	}
}

// Covers P8
func TestCORSDefault(t *testing.T) {
	for _, kind := range []string{"nil options", "zero options"} {
		for _, origin := range []string{"", foreignOrigin} {
			t.Run(kind+"/"+origin, func(t *testing.T) {
				var opts []Option
				if kind == "zero options" {
					opts = []Option{}
				}
				f := corsSession(t, opts, origin, true)
				corsWebsocket(t, f, origin, true, true)
				corsWebsocket(t, f, origin, false, true)
			})
		}
	}
}

// Covers S4
func TestCORSDisabled(t *testing.T) {
	for _, origin := range []string{"", foreignOrigin} {
		t.Run("legacy/"+origin, func(t *testing.T) {
			f := corsSession(t, []Option{WithCORS(!(true))}, origin, false)
			corsWebsocket(t, f, origin, true, origin == "")
			corsWebsocket(t, f, origin, false, origin == "")
		})
	}
	for _, disabled := range []bool{false, true} {
		for _, allowed := range []bool{false, true} {
			name := "enabled default"
			if disabled {
				name = "disabled default"
			}
			if allowed {
				name += "/explicit allow"
			} else {
				name += "/explicit deny"
			}
			t.Run(name, func(t *testing.T) {
				check := func(r *http.Request) bool { return allowed && r.Header.Get("Origin") == foreignOrigin }
				opts := []Option{WithCORS(!(disabled)), WithTransports([]transport.Transport{&polling.Transport{CheckOrigin: check}, &websocket.Transport{CheckOrigin: check}}...)}
				f := corsSession(t, opts, foreignOrigin, allowed)
				corsWebsocket(t, f, foreignOrigin, true, allowed)
				corsWebsocket(t, f, foreignOrigin, false, allowed)
			})
		}
	}
}

type otherCORSTransport struct{ transport.Transport }

func (*otherCORSTransport) Name() string { return "other" }

func TestCORSTransportCopies(t *testing.T) {
	client := &http.Client{Timeout: time.Second}
	p := &polling.Transport{Client: client}
	w := &websocket.Transport{ReadBufferSize: 123, WriteBufferSize: 456}
	other := &otherCORSTransport{}
	configured := []transport.Transport{p, w, other}
	s := NewServer(WithTransports(configured...))
	t.Cleanup(func() { _ = s.Close() })
	pt, ok := s.transports.Get("polling")
	require.True(t, ok)
	wt, ok := s.transports.Get("websocket")
	require.True(t, ok)
	ot, ok := s.transports.Get("other")
	require.True(t, ok)
	require.NotSame(t, p, pt)
	require.NotSame(t, w, wt)
	require.Same(t, other, ot)
	require.Same(t, client, pt.(*polling.Transport).Client)
	require.Equal(t, 123, wt.(*websocket.Transport).ReadBufferSize)
	require.Equal(t, 456, wt.(*websocket.Transport).WriteBufferSize)
	require.Nil(t, p.CheckOrigin)
	require.Nil(t, w.CheckOrigin)
	require.Same(t, p, configured[0])
	require.Same(t, w, configured[1])
	// Resolving one enabled server must not enable another disabled server or
	// mutate either package's global default transport.
	beforePolling, beforeWebsocket := polling.Default.CheckOrigin, websocket.Default.CheckOrigin
	defaultServer := NewServer()
	defer func() { _ = defaultServer.Close() }()
	require.Equal(t, beforePolling == nil, polling.Default.CheckOrigin == nil)
	require.Equal(t, beforeWebsocket == nil, websocket.Default.CheckOrigin == nil)
	disabled := NewServer(WithCORS(!(true)), WithTransports(configured...))
	defer func() { _ = disabled.Close() }()
	dp, _ := disabled.transports.Get("polling")
	dw, _ := disabled.transports.Get("websocket")
	require.NotSame(t, p, dp)
	require.Nil(t, dp.(*polling.Transport).CheckOrigin)
	require.NotSame(t, w, dw)
	require.Nil(t, dw.(*websocket.Transport).CheckOrigin)
}

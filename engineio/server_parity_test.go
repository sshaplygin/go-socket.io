package engineio

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// parityOptions retains named wire-scenario settings independently of public API spelling.
func parityOptions(values map[string]any) []Option {
	var opts []Option
	for name, value := range values {
		switch name {
		case "UpgradeTimeout":
			opts = append(opts, WithUpgradeTimeout(value.(time.Duration)))
		case "MaxHTTPBufferSize":
			opts = append(opts, WithMaxHTTPBufferSize(value.(int64)))
		case "DisableUpgrades":
			opts = append(opts, WithUpgrades(!value.(bool)))
		default:
			panic("unsupported wire-scenario option: " + name)
		}
	}
	return opts
}

type parityWire struct {
	srv    *Server
	http   *httptest.Server
	client *http.Client
	poll   string
	conn   Conn
	params eioparser.ConnParameters
}

func newParityWire(t *testing.T, opts []Option) *parityWire {
	t.Helper()
	f := &parityWire{srv: NewServer(opts...), client: &http.Client{Timeout: 2 * time.Second}}
	f.http = httptest.NewServer(f.srv)
	t.Cleanup(func() { _ = f.srv.Close(); f.http.Close(); f.client.CloseIdleConnections() })
	code, b := f.request(t, http.MethodGet, f.http.URL+"/?EIO=3&transport=polling&b64=1", "")
	require.Equal(t, http.StatusOK, code, b)
	_, packet, ok := strings.Cut(b, ":")
	require.True(t, ok)
	require.True(t, strings.HasPrefix(packet, "0"), packet)
	var err error
	f.params, err = eioparser.ReadConnParameters(strings.NewReader(packet[1:]))
	require.NoError(t, err)
	f.poll = f.http.URL + "/?EIO=3&transport=polling&b64=1&sid=" + f.params.SID
	accepted := make(chan Conn, 1)
	go func() { c, _ := f.srv.Accept(); accepted <- c }()
	select {
	case f.conn = <-accepted:
		require.NotNil(t, f.conn)
	case <-time.After(time.Second):
		t.Fatal("OPEN was not accepted")
	}
	return f
}

func (f *parityWire) request(t *testing.T, method, target, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	require.NoError(t, err)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	}
	resp, err := f.client.Do(req)
	require.NoError(t, err)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode, string(b)
}

func (f *parityWire) probe(t *testing.T) *gorilla.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(f.http.URL, "http") + "/?EIO=3&transport=websocket&sid=" + f.params.SID
	c, _, err := gorilla.DefaultDialer.Dial(u, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("2probe")))
	_, b, err := c.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "3probe", string(b))
	return c
}

type parityRead struct {
	message string
	err     error
}

func parityReader(c Conn) <-chan parityRead {
	out := make(chan parityRead, 100)
	go func() {
		for {
			_, r, err := c.NextReader()
			if err != nil {
				out <- parityRead{err: err}
				return
			}
			b, err := io.ReadAll(r)
			closeErr := r.Close()
			if err == nil {
				err = closeErr
			}
			out <- parityRead{string(b), err}
			if err != nil {
				_ = c.Close()
				return
			}
		}
	}()
	return out
}
func parityMessage(t *testing.T, reads <-chan parityRead, want string) {
	t.Helper()
	select {
	case got := <-reads:
		require.NoError(t, got.err)
		require.Equal(t, want, got.message)
	case <-time.After(time.Second):
		t.Fatal("session did not deliver " + want)
	}
}
func parityWrite(c Conn, message string) <-chan error {
	done := make(chan error, 1)
	go func() {
		w, err := c.NextWriter(eioparser.String)
		if err == nil {
			_, err = io.WriteString(w, message)
			closeErr := w.Close()
			if err == nil {
				err = closeErr
			}
		}
		done <- err
	}()
	return done
}

// Covers P4
func TestUpgradeProbeWindow(t *testing.T) {
	for _, name := range []string{"post", "cadence", "failed", "timeout"} {
		t.Run(name, func(t *testing.T) {
			values := map[string]any{}
			if name == "timeout" {
				values["UpgradeTimeout"] = 100 * time.Millisecond
			}
			f := newParityWire(t, parityOptions(values))
			reads := parityReader(f.conn)
			c := f.probe(t)
			if name == "cadence" {
				start, n := time.Now(), 0
				for time.Since(start) < time.Second {
					code, b := f.request(t, http.MethodGet, f.poll, "")
					require.Equal(t, 200, code)
					require.Equal(t, "1:6", b)
					n++
				}
				require.GreaterOrEqual(t, n, 1)
				require.LessOrEqual(t, n, 11)
				return
			}
			switch name {
			case "failed":
				require.NoError(t, c.Close())
			case "timeout":
				_, _, err := c.ReadMessage()
				require.Error(t, err)
			}
			code, b := f.request(t, http.MethodPost, f.poll, "2:4a")
			require.Equal(t, 200, code, b)
			require.Equal(t, "ok", b)
			if name == "post" {
				started := time.Now()
				code, b = f.request(t, http.MethodGet, f.poll, "")
				require.Less(t, time.Since(started), time.Second, "probe-window GET must finish within one second")
				require.Equal(t, 200, code)
				require.Equal(t, "1:6", b)
				require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("5")))
				write := parityWrite(f.conn, "server")
				_, b, err := c.ReadMessage()
				require.NoError(t, err)
				require.Equal(t, "4server", string(b))
				require.NoError(t, <-write)
				require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("4b")))
			} else {
				code, b = f.request(t, http.MethodPost, f.poll, "2:4b")
				require.Equal(t, 200, code, b)
				write := parityWrite(f.conn, "server")
				code, b = f.request(t, http.MethodGet, f.poll, "")
				require.Equal(t, 200, code)
				require.Equal(t, "7:4server", b)
				require.NoError(t, <-write)
			}
			parityMessage(t, reads, "a")
			parityMessage(t, reads, "b")
		})
	}
}

// Covers E3
func TestUpgradeTimeout(t *testing.T) {
	f := newParityWire(t, parityOptions(map[string]any{"UpgradeTimeout": 100 * time.Millisecond}))
	_ = parityReader(f.conn)
	c := f.probe(t)
	require.NoError(t, c.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err := c.ReadMessage()
	require.Error(t, err)
	if timeout, ok := err.(interface{ Timeout() bool }); ok {
		require.False(t, timeout.Timeout(), "server must close the probe before the client's deadline")
	}
	write := parityWrite(f.conn, "still polling")
	code, b := f.request(t, http.MethodGet, f.poll, "")
	require.Equal(t, 200, code)
	require.Equal(t, "14:4still polling", b)
	require.NoError(t, <-write)
	for _, opts := range [][]Option{nil, {}} {
		s := NewServer(opts...)
		v := reflect.ValueOf(s).Elem().FieldByName("upgradeTimeout")
		require.True(t, v.IsValid())
		require.Equal(t, int64(10*time.Second), v.Int())
		_ = s.Close()
	}
}

// Covers E4
func TestMaxHTTPBufferSize(t *testing.T) {
	t.Run("polling", func(t *testing.T) {
		rec := newRecorder()
		opts := parityOptions(map[string]any{"MaxHTTPBufferSize": int64(8)})
		opts = append(opts, WithLogger(slog.New(rec)))
		f := newParityWire(t, opts)
		reads := parityReader(f.conn)
		code, b := f.request(t, http.MethodPost, f.poll, "6:4hello")
		require.Equal(t, 200, code, b)
		parityMessage(t, reads, "hello")
		code, b = f.request(t, http.MethodPost, f.poll, "7:4hello!")
		require.Equal(t, http.StatusRequestEntityTooLarge, code, b)
		select {
		case got := <-reads:
			require.Error(t, got.err)
		case <-time.After(time.Second):
			t.Fatal("oversize POST did not fail the session")
		}
		require.Eventually(t, func() bool { return len(rec.find("engineio: session close")) == 1 }, time.Second, time.Millisecond)
		require.Equal(t, "transport error", rec.find("engineio: session close")[0]["reason"])
	})
	t.Run("websocket", func(t *testing.T) {
		f := newParityWire(t, parityOptions(map[string]any{"MaxHTTPBufferSize": int64(8)}))
		reads := parityReader(f.conn)
		c := f.probe(t)
		require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("5")))
		require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("4hello!!")))
		parityMessage(t, reads, "hello!!")
		require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("4hello!!!")))
		_, _, err := c.ReadMessage()
		require.True(t, gorilla.IsCloseError(err, gorilla.CloseMessageTooBig), "%v", err)
	})
	t.Run("default", func(t *testing.T) {
		for _, opts := range [][]Option{nil, {}, parityOptions(map[string]any{"MaxHTTPBufferSize": int64(-1)})} {
			s := NewServer(opts...)
			v := reflect.ValueOf(s).Elem().FieldByName("maxHTTPBufferSize")
			require.True(t, v.IsValid())
			require.Equal(t, int64(1_000_000), v.Int())
			_ = s.Close()
		}
	})
}

// Covers E7
func TestDisableUpgrades(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint(disabled), func(t *testing.T) {
			f := newParityWire(t, parityOptions(map[string]any{"DisableUpgrades": disabled}))
			if !disabled {
				require.Contains(t, f.params.Upgrades, "websocket")
				c := f.probe(t)
				reads := parityReader(f.conn)
				require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("5")))
				require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("4upgraded")))
				parityMessage(t, reads, "upgraded")
			} else {
				require.NotNil(t, f.params.Upgrades)
				require.Empty(t, f.params.Upgrades)
				code, b := f.request(t, http.MethodGet, f.http.URL+"/?transport=websocket&sid="+f.params.SID, "")
				require.Equal(t, 400, code)
				require.JSONEq(t, `{"code":3,"message":"Bad request"}`, b)
			}
		})
	}
}

// Covers E2
func TestHeartbeatDefaults(t *testing.T) {
	f := newParityWire(t, nil)
	require.Equal(t, 25*time.Second, f.params.PingInterval)
	require.Equal(t, 20*time.Second, f.params.PingTimeout)
}

// Covers E5
// Covers E14
// Covers P11
// Covers P12
func TestEngineRequestValidation(t *testing.T) {
	for _, tc := range []struct {
		name, method, query string
		code, status        int
		reject              bool
	}{
		{"transport", "GET", "transport=missing", 0, 400, false},
		{"sid", "GET", "transport=polling&sid=missing", 1, 400, false},
		{"post", "POST", "transport=polling", 2, 400, false},
		{"preflight", "OPTIONS", "transport=polling", 2, 400, false},
		{"checker", "GET", "transport=polling", 4, 403, true},
		{"websocket checker", "GET", "transport=websocket", 4, 403, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := []Option{}
			if tc.reject {
				opts = append(opts, WithRequestChecker(func(*http.Request) (http.Header, error) { return nil, fmt.Errorf("private reason") }))
			}
			s := NewServer(opts...)
			defer func() { _ = s.Close() }()
			r := httptest.NewRequest(tc.method, "/?EIO=3&"+tc.query, nil)
			if strings.Contains(tc.name, "websocket") {
				r.Header.Set("Connection", "Upgrade")
				r.Header.Set("Upgrade", "websocket")
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code)
			var got struct {
				Code    int
				Message string
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			require.Equal(t, tc.code, got.Code)
			require.Equal(t, [...]string{"Transport unknown", "Session ID unknown", "Bad handshake method", "Bad request", "Forbidden"}[tc.code], got.Message)
			require.NotContains(t, got.Message, "private reason")
			require.Zero(t, s.Count())
		})
	}
}

func TestRequestCheckerOnlyChecksHandshake(t *testing.T) {
	var calls atomic.Int32
	opts := []Option{WithRequestChecker(func(*http.Request) (http.Header, error) {
		calls.Add(1)
		return http.Header{"X-Checked": []string{"yes"}}, nil
	})}
	f := newParityWire(t, opts)
	reads := parityReader(f.conn)
	code, b := f.request(t, http.MethodPost, f.poll, "2:4a")
	require.Equal(t, 200, code, b)
	parityMessage(t, reads, "a")
	write := parityWrite(f.conn, "reply")
	code, b = f.request(t, http.MethodGet, f.poll, "")
	require.Equal(t, 200, code)
	require.Equal(t, "6:4reply", b)
	require.NoError(t, <-write)
	c := f.probe(t)
	require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("5")))
	require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("4b")))
	parityMessage(t, reads, "b")
	require.Equal(t, int32(1), calls.Load(), "polls, POSTs and upgrade must not rerun the handshake checker")
}

func TestPollingChunkedMessageLimit(t *testing.T) {
	f := newParityWire(t, parityOptions(map[string]any{"MaxHTTPBufferSize": int64(8)}))
	reads := parityReader(f.conn)
	req, err := http.NewRequest(http.MethodPost, f.poll, io.NopCloser(strings.NewReader("7:4hello!")))
	require.NoError(t, err)
	req.ContentLength = -1
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp, err := f.client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
	select {
	case got := <-reads:
		require.Error(t, got.err)
	case <-time.After(time.Second):
		t.Fatal("oversize chunked POST did not close the transport")
	}
}

func TestWebsocketHandshakeMessageLimit(t *testing.T) {
	s := NewServer(parityOptions(map[string]any{"MaxHTTPBufferSize": int64(8)})...)
	srv := httptest.NewServer(s)
	defer srv.Close()
	defer func() { _ = s.Close() }()
	c, _, err := gorilla.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?transport=websocket&EIO=3", nil)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.NoError(t, c.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err = c.ReadMessage()
	require.NoError(t, err)
	accepted, err := s.Accept()
	require.NoError(t, err)
	reads := parityReader(accepted)
	require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("4hello!!")))
	parityMessage(t, reads, "hello!!")
	require.NoError(t, c.WriteMessage(gorilla.TextMessage, []byte("4hello!!!")))
	_, _, err = c.ReadMessage()
	require.True(t, gorilla.IsCloseError(err, gorilla.CloseMessageTooBig), "%v", err)
}

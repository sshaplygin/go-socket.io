package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/sshaplygin/go-socket.io/client"
	wstransport "github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

const (
	allowedOrigin = "http://localhost:3000"
	foreignOrigin = "http://evil.example"
)

var sidPattern = regexp.MustCompile(`"sid":"([^"]+)"`)

func init() { gin.SetMode(gin.TestMode) }

// startExample serves the example's router the way run does.
func startExample(t *testing.T) *httptest.Server {
	t.Helper()
	server, handler := newHandler(allowedOrigin)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve()
	}()
	ts := httptest.NewServer(handler)
	t.Cleanup(func() {
		_ = server.Close()
		<-done
		ts.CloseClientConnections() // long polls of open sessions
		ts.Close()
	})
	return ts
}

// request sends a polling request with an optional Origin and returns the response
// with its body read.
func request(t *testing.T, method, url, origin, body string, header http.Header) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(data)
}

// handshake opens a polling session with the given Origin and returns the response
// and the session ID.
func handshake(t *testing.T, ts *httptest.Server, origin string) (*http.Response, string) {
	t.Helper()
	resp, body := request(t, http.MethodGet, ts.URL+"/socket.io/?EIO=3&transport=polling", origin, "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("handshake with Origin %q: HTTP %d %q", origin, resp.StatusCode, body)
	}
	m := sidPattern.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("handshake with Origin %q: no sid in %q", origin, body)
	}
	return resp, m[1]
}

// wantCORS checks the CORS origin headers a browser reads.
func wantCORS(t *testing.T, what string, resp *http.Response, origin, credentials string) {
	t.Helper()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("%s: Access-Control-Allow-Origin %q, want %q", what, got, origin)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != credentials {
		t.Errorf("%s: Access-Control-Allow-Credentials %q, want %q", what, got, credentials)
	}
}

// TestPollingCORS pins what a credentialed browser XHR (the default of socket.io-client
// 1.x and 2.x) needs: the configured origin and the server's own origin are reflected
// with credentials, a foreign origin gets no CORS headers, and a request without an
// Origin, as non-browser clients send it, is still served.
func TestPollingCORS(t *testing.T) {
	ts := startExample(t)
	for _, tc := range []struct {
		name, origin, wantOrigin, wantCredentials string
	}{
		{"configured origin", allowedOrigin, allowedOrigin, "true"},
		{"own origin", ts.URL, ts.URL, "true"},
		{"foreign origin", foreignOrigin, "", ""},
		{"no origin", "", "*", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, sid := handshake(t, ts, tc.origin)
			wantCORS(t, "handshake", resp, tc.wantOrigin, tc.wantCredentials)

			session := ts.URL + "/socket.io/?EIO=3&transport=polling&sid=" + sid
			// The preflight of a binary POST (Content-Type application/octet-stream).
			resp, body := request(t, http.MethodOptions, session, tc.origin, "", http.Header{
				"Access-Control-Request-Method":  {"POST"},
				"Access-Control-Request-Headers": {"content-type"},
			})
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("preflight: HTTP %d %q", resp.StatusCode, body)
			}
			if got := resp.Header.Get("Access-Control-Allow-Headers"); got != "Content-Type" {
				t.Errorf("preflight: Access-Control-Allow-Headers %q, want Content-Type", got)
			}
			wantCORS(t, "preflight", resp, tc.wantOrigin, tc.wantCredentials)

			resp, body = request(t, http.MethodPost, session, tc.origin, "1:2", http.Header{
				"Content-Type": {"text/plain;charset=UTF-8"},
			})
			if resp.StatusCode != http.StatusOK || body != "ok" {
				t.Fatalf("POST: HTTP %d %q", resp.StatusCode, body)
			}
			wantCORS(t, "POST", resp, tc.wantOrigin, tc.wantCredentials)
		})
	}
}

// TestWebsocketOrigin pins the websocket origin check for direct connections and for
// upgrades of a polling session: browsers always send Origin there, so a foreign page
// is refused while the configured and the server's own origins connect.
func TestWebsocketOrigin(t *testing.T) {
	ts := startExample(t)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/socket.io/?EIO=3&transport=websocket"
	for _, tc := range []struct {
		name, origin string
		want         int
	}{
		{"configured origin", allowedOrigin, http.StatusSwitchingProtocols},
		{"own origin", ts.URL, http.StatusSwitchingProtocols},
		{"foreign origin", foreignOrigin, http.StatusForbidden},
		{"no origin", "", http.StatusSwitchingProtocols},
	} {
		for _, upgrade := range []bool{false, true} {
			name := tc.name + "/direct"
			if upgrade {
				name = tc.name + "/upgrade"
			}
			t.Run(name, func(t *testing.T) {
				target := wsURL
				if upgrade {
					// The polling handshake comes from the page that later upgrades.
					_, sid := handshake(t, ts, tc.origin)
					target += "&sid=" + sid
				}
				header := http.Header{}
				if tc.origin != "" {
					header.Set("Origin", tc.origin)
				}
				conn, resp, err := websocket.DefaultDialer.Dial(target, header)
				if conn != nil {
					_ = conn.Close()
				}
				if resp == nil {
					t.Fatalf("dial: %v", err)
				}
				if resp.StatusCode != tc.want {
					t.Fatalf("websocket with Origin %q: HTTP %d (%v), want %d", tc.origin, resp.StatusCode, err, tc.want)
				}
				if tc.want != http.StatusSwitchingProtocols && !errors.Is(err, websocket.ErrBadHandshake) {
					t.Fatalf("refused websocket: error %v, want %v", err, websocket.ErrBadHandshake)
				}
			})
		}
	}
}

// TestChatWithoutOrigin runs the chat through the same router with the Go client, which
// sends no Origin: over polling with its websocket upgrade, and over websocket alone.
func TestChatWithoutOrigin(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []client.Option
	}{
		{"polling and upgrade", nil},
		{"websocket", []client.Option{client.WithTransports(&wstransport.Transport{})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := startExample(t)
			cl, err := client.New(ts.URL, tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			login := make(chan map[string]interface{}, 1)
			cl.OnEvent("login", func(_ client.Conn, data map[string]interface{}) {
				select {
				case login <- data:
				default:
				}
			})
			if err := cl.Connect(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cl.Close() })
			cl.Emit("add user", "gopher")
			select {
			case data := <-login:
				if data["numUsers"] != 1.0 {
					t.Fatalf("login payload %v, want numUsers 1", data)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("no login event within 10s")
			}
		})
	}
}

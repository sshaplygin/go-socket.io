package engineio_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/engineio"
)

// benchWaitTimeout bounds each echo round trip, so a lost frame fails the
// iteration instead of stalling the run.
const benchWaitTimeout = 30 * time.Second

func benchEchoServer(b *testing.B) *httptest.Server {
	srv := engineio.NewServer(engineio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	ts := httptest.NewServer(srv)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := srv.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		for {
			typ, r, err := c.NextReader()
			if err != nil {
				return
			}
			data, readErr := io.ReadAll(r)
			closeErr := r.Close()
			if readErr != nil || closeErr != nil {
				return
			}
			w, err := c.NextWriter(typ)
			if err != nil {
				return
			}
			_, writeErr := w.Write(data)
			closeErr = w.Close()
			if writeErr != nil || closeErr != nil {
				return
			}
		}
	}()
	b.Cleanup(func() { _ = srv.Close(); ts.Close(); <-done })
	return ts
}

func BenchmarkWebsocketEcho(b *testing.B) {
	for _, tc := range []struct{ name, payload string }{{"small", "4hello"}, {"4KiB", "4" + strings.Repeat("x", 4096)}} {
		b.Run(tc.name, func(b *testing.B) {
			ts := benchEchoServer(b)
			c, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/?EIO=3&transport=websocket", nil)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = c.Close() })
			if err = c.SetReadDeadline(time.Now().Add(benchWaitTimeout)); err != nil {
				b.Fatal(err)
			}
			if _, _, err = c.ReadMessage(); err != nil {
				b.Fatal(err)
			}
			payload := []byte(tc.payload)
			b.ReportAllocs()
			b.SetBytes(int64(len(payload) - 1))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				deadline := time.Now().Add(benchWaitTimeout)
				if err := c.SetWriteDeadline(deadline); err != nil {
					b.Fatal(err)
				}
				if err := c.SetReadDeadline(deadline); err != nil {
					b.Fatal(err)
				}
				if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
					b.Fatal(err)
				}
				_, got, err := c.ReadMessage()
				if err != nil {
					b.Fatal(err)
				}
				if !bytes.Equal(got, payload) {
					b.Fatalf("unexpected echo %q", got)
				}
			}
		})
	}
}

func benchHTTP(b *testing.B, c *http.Client, method, url, body string) []byte {
	b.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		b.Fatal(err)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "text/plain; charset=UTF-8")
	}
	resp, err := c.Do(req)
	if err != nil {
		b.Fatal(err)
	}
	data, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		b.Fatal(readErr)
	}
	if closeErr != nil {
		b.Fatal(closeErr)
	}
	if resp.StatusCode != http.StatusOK {
		b.Fatalf("HTTP %d: %s", resp.StatusCode, data)
	}
	return data
}

func BenchmarkPollingEcho(b *testing.B) {
	ts := benchEchoServer(b)
	c := &http.Client{Transport: &http.Transport{}, Timeout: 10 * time.Second}
	b.Cleanup(c.CloseIdleConnections)
	url := ts.URL + "/?EIO=3&transport=polling&b64=1"
	open := benchHTTP(b, c, http.MethodGet, url, "")
	_, packet, ok := strings.Cut(string(open), ":")
	if !ok || !strings.HasPrefix(packet, "0") {
		b.Fatalf("invalid OPEN: %q", open)
	}
	var handshake struct {
		SID string `json:"sid"`
	}
	if err := json.Unmarshal([]byte(packet[1:]), &handshake); err != nil {
		b.Fatal(err)
	}
	url += "&sid=" + handshake.SID
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchHTTP(b, c, http.MethodPost, url, "6:4hello")
		got := benchHTTP(b, c, http.MethodGet, url, "")
		if string(got) != "6:4hello" {
			b.Fatalf("unexpected echo %q", got)
		}
	}
}

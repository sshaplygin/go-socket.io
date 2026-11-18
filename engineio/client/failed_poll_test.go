package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

// TestFailedPollEndsSession checks that a poll response that cannot be decoded
// ends a polling session for its writers too: they return an error instead of
// blocking with no poller and no write deadline (#287).
func TestFailedPollEndsSession(t *testing.T) {
	cp := eioparser.ConnParameters{PingInterval: 25 * time.Second, PingTimeout: 20 * time.Second, SID: "sid1", Upgrades: []string{}}
	var open bytes.Buffer
	_, err := cp.WriteTo(&open)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("sid") == "":
			_, _ = fmt.Fprintf(w, "%d:0%s", open.Len()+1, open.String())
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte("not a payload"))
		default:
			_, _ = io.Copy(io.Discard, r.Body)
		}
	}))
	t.Cleanup(srv.Close)
	httpClient := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(httpClient.CloseIdleConnections)

	d := Dialer{Transports: []transport.Transport{&polling.Transport{Client: httpClient}}}
	conn, err := d.Dial(srv.URL+"/?b64=1", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	_, _, err = conn.NextReader()
	require.Error(t, err)
	for i := 0; i < 2; i++ {
		done := make(chan struct{})
		go func() {
			defer close(done)
			w, err := conn.NextWriter(eioparser.String)
			if err == nil {
				_, _ = w.Write([]byte("hello"))
				_ = w.Close()
			}
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("writer %d still blocked 2s after the failed poll", i+1)
		}
	}
}

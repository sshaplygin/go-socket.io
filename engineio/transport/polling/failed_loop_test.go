package polling

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// failingPollServer answers the handshake with OPEN, every later poll with
// poll, and accepts POSTs.
func failingPollServer(t *testing.T, poll func(w http.ResponseWriter, r *http.Request, done <-chan struct{})) *url.URL {
	t.Helper()
	cp := eioparser.ConnParameters{PingInterval: 25 * time.Second, PingTimeout: 20 * time.Second, SID: "sid1", Upgrades: []string{}}
	var open bytes.Buffer
	_, err := cp.WriteTo(&open)
	require.NoError(t, err)
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("sid") == "":
			_, _ = fmt.Fprintf(w, "%d:0%s", open.Len()+1, open.String())
		case r.Method == http.MethodGet:
			poll(w, r, done)
		default:
			_, _ = io.Copy(io.Discard, r.Body)
		}
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	u, err := url.Parse(srv.URL + "/?b64=1")
	require.NoError(t, err)
	return u
}

func holdPoll(_ http.ResponseWriter, r *http.Request, done <-chan struct{}) {
	select {
	case <-r.Context().Done():
	case <-done:
	}
}

// TestFailedPollClosesClientConn checks that a poll that fails closes the
// client connection by itself, with no writer involved: a closed connection
// refuses PauseContext with io.ErrClosedPipe (#287). net/http reports a body
// read past http.Client.Timeout as a Temporary error, which must close it too.
func TestFailedPollClosesClientConn(t *testing.T) {
	for _, tc := range []struct {
		name string
		poll func(w http.ResponseWriter, r *http.Request, done <-chan struct{})
	}{
		{"undecodable body", func(w http.ResponseWriter, _ *http.Request, _ <-chan struct{}) {
			_, _ = w.Write([]byte("not a payload"))
		}},
		{"body stalled past the HTTP client timeout", func(w http.ResponseWriter, r *http.Request, done <-chan struct{}) {
			_, _ = w.Write([]byte("20:4hel"))
			w.(http.Flusher).Flush()
			holdPoll(w, r, done)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := failingPollServer(t, tc.poll)
			httpClient := &http.Client{Timeout: 300 * time.Millisecond}
			t.Cleanup(httpClient.CloseIdleConnections)
			conn, err := (&Transport{Client: httpClient}).Dial(u, nil)
			require.NoError(t, err)
			cc := conn.(*clientConn)
			t.Cleanup(func() { _ = cc.Close() })
			_, err = cc.Open()
			require.NoError(t, err)
			// The body is decoded only for a reader; read until the poll fails.
			for {
				_, _, r, err := cc.NextReader()
				if err != nil {
					break
				}
				_ = r.Close()
			}

			require.Eventually(t, func() bool {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				defer cancel()
				err := cc.PauseContext(ctx)
				if err == nil {
					cc.Resume()
				}
				return err == io.ErrClosedPipe
			}, 2*time.Second, 20*time.Millisecond, "connection still open 2s after the failed poll")
		})
	}
}

// TestFailedSendEndsClientConn checks that a send loop that ends on a FlushOut
// error, here an expired write deadline, ends the client connection, so that
// its reader returns instead of waiting on a poll that no longer matters (#287).
func TestFailedSendEndsClientConn(t *testing.T) {
	u := failingPollServer(t, holdPoll)
	httpClient := &http.Client{}
	t.Cleanup(httpClient.CloseIdleConnections)
	conn, err := (&Transport{Client: httpClient}).Dial(u, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.SetWriteDeadline(time.Now().Add(100*time.Millisecond)))
	_, err = conn.(*clientConn).Open()
	require.NoError(t, err)

	read := make(chan error, 1)
	go func() {
		_, _, r, err := conn.NextReader()
		if err == nil {
			_ = r.Close()
		}
		read <- err
	}()
	select {
	case err := <-read:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("reader still blocked 2s after the send loop failed")
	}
}

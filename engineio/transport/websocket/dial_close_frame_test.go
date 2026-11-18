package websocket

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

// newDialCloseFixture holds a Dial connection in its server field and a raw server peer.
func newDialCloseFixture(t *testing.T) *closeFixture {
	t.Helper()
	peers := make(chan *gorilla.Conn, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if peer, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil); err == nil {
			peers <- peer
		}
	}))
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	require.NoError(t, err)
	c, err := (&Transport{}).Dial(u, nil)
	require.NoError(t, err)
	f := &closeFixture{server: c, peer: <-peers}
	t.Cleanup(func() { _ = f.peer.Close(); _ = c.(interface{ Abort() error }).Abort() })
	return f
}

// The client side closes as the server side does: Close sends a normal closure frame
// and waits for the reply at most closeReplyTimeout. A server that stopped reading
// holds Close at most until the frame's write deadline; Close releases a blocked writer
// and does not take the write lock a failed one holds (TestServerCloseFromFailedWriter...).
func TestDialClose(t *testing.T) {
	for _, peer := range []string{"replies", "does not reply", "stalled, writer blocked", "stalled, writer failed"} {
		t.Run(peer, func(t *testing.T) {
			f := newDialCloseFixture(t)
			stalled, writeDone := strings.HasPrefix(peer, "stalled"), make(chan error, 1)
			switch peer {
			case "does not reply":
				f.peer.SetCloseHandler(func(int, string) error { return nil })
			case "stalled, writer failed":
				require.NoError(t, f.server.SetWriteDeadline(time.Now().Add(300*time.Millisecond)))
			}
			var written atomic.Int64
			if stalled {
				w, err := f.server.NextWriter(eioparser.String, eioparser.MESSAGE)
				require.NoError(t, err)
				go func() {
					for n, err := 0, error(nil); ; written.Add(int64(n)) {
						if n, err = w.Write([]byte(strings.Repeat("x", 32<<10))); err != nil {
							writeDone <- err // w is not closed: the write lock stays held
							return
						}
					}
				}()
			}
			for last, stable := int64(-1), 0; peer == "stalled, writer blocked" && stable < 5; {
				time.Sleep(20 * time.Millisecond) // until the writer makes no progress
				if now := written.Load(); now == last && now > 0 {
					stable++
				} else {
					stable, last = 0, now
				}
			}
			if peer == "stalled, writer failed" {
				require.Error(t, <-writeDone)
			}
			start, done := time.Now(), make(chan error, 1)
			go func() { done <- f.server.Close() }()
			if !stalled {
				requireCloseCode(t, readClose(t, f.peer), gorilla.CloseNormalClosure)
			}
			select {
			case err := <-done:
				require.True(t, stalled || err == nil, "%v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("Close is not bounded")
			}
			require.Less(t, time.Since(start), 5*time.Second)
			if peer == "stalled, writer blocked" {
				require.Error(t, <-writeDone, "Close releases the blocked writer")
			}
		})
	}
}

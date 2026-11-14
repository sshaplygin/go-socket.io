package client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
)

// TestDialClosesFailedAttempt: when the last transport tried fails after its connection
// was established, here because the server sends a MESSAGE instead of OPEN, Dial closes
// that connection before it returns the error.
func TestDialClosesFailedAttempt(t *testing.T) {
	closed := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_ = wsutil.WriteServerText(c, []byte("4not an open packet"))
		_, _ = io.Copy(io.Discard, c)
		close(closed)
	}))
	t.Cleanup(ts.Close)

	_, err := (&Dialer{Transports: []transport.Transport{websocket.Default}}).Dial(ts.URL, nil)
	require.ErrorContains(t, err, "invalid open")
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection of the failed attempt is still open")
	}
}

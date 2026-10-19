package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gorilla "github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
)

func TestClientRejectsOversizedOpen(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			packet := `0{"sid":"` + strings.Repeat("x", 2<<20) + `","upgrades":[],"pingInterval":25000,"pingTimeout":20000}`
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tr.Name() == "websocket" {
					ws, err := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
					if err != nil {
						return
					}
					defer func() { _ = ws.Close() }()
					_ = ws.WriteMessage(gorilla.TextMessage, []byte(packet))
					return
				}
				w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
				_, _ = fmt.Fprintf(w, "%d:%s", len(packet), packet)
			}))
			defer ts.Close()
			c, err := (&Dialer{Transports: []transport.Transport{tr}}).Dial(ts.URL, nil)
			if c != nil {
				_ = c.Close()
			}
			require.Error(t, err, "OPEN must be bounded before JSON allocates a peer-controlled SID")
		})
	}
}

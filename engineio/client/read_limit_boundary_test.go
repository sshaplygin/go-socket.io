package client

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	gorilla "github.com/gorilla/websocket"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/stretchr/testify/require"
)

const boundaryOpen = `0{"sid":"boundary","upgrades":[],"pingInterval":3600000,"pingTimeout":20000}`

// sizedLimitPacket measures the actual transport boundary: websocket message
// bytes include the packet type; polling body bytes also include payload framing.
func sizedLimitPacket(t *testing.T, name, prefix, padding string, size int) (packet, wire string) {
	t.Helper()
	for n := 0; n <= size; n++ {
		packet = prefix + strings.Repeat(padding, n)
		wire = packet
		if name == "polling" {
			wire = fmt.Sprintf("%d:%s", len(packet), packet)
		}
		if len(wire) == size {
			return packet, wire
		}
	}
	t.Fatalf("cannot construct %d-byte %s packet", size, name)
	return "", ""
}

func TestClientConfiguredOpenLimitBoundary(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		for _, size := range []int{512, 513} {
			t.Run(fmt.Sprintf("%s/%d", tr.Name(), size), func(t *testing.T) {
				packet, wire := sizedLimitPacket(t, tr.Name(), boundaryOpen, " ", size)
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tr.Name() == "websocket" {
						ws, e := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
						if e != nil {
							return
						}
						defer func() { _ = ws.Close() }()
						_ = ws.WriteMessage(gorilla.TextMessage, []byte(packet))
						return
					}
					w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
					if r.URL.Query().Get("sid") != "" {
						_, _ = io.WriteString(w, "1:1")
						return
					}
					_, _ = io.WriteString(w, wire)
				}))
				defer ts.Close()
				c, e := (&Dialer{MaxMessageSize: 512, Transports: []transport.Transport{tr}}).Dial(ts.URL, nil)
				if c != nil {
					defer func() { _ = c.Close() }()
				}
				if size == 512 {
					require.NoError(t, e)
					require.Equal(t, "boundary", c.ID())
				} else {
					require.Error(t, e)
					require.Nil(t, c)
				}
			})
		}
	}
}

func TestClientConfiguredLimitAppliesToLaterMessages(t *testing.T) {
	for _, tr := range []transport.Transport{polling.Default, websocket.Default} {
		t.Run(tr.Name(), func(t *testing.T) {
			exactPacket, exactWire := sizedLimitPacket(t, tr.Name(), "4", "x", 512)
			largePacket, largeWire := sizedLimitPacket(t, tr.Name(), "4", "y", 513)
			var polls atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tr.Name() == "websocket" {
					ws, e := (&gorilla.Upgrader{}).Upgrade(w, r, nil)
					if e != nil {
						return
					}
					defer func() { _ = ws.Close() }()
					for _, p := range []string{boundaryOpen, exactPacket, largePacket} {
						if ws.WriteMessage(gorilla.TextMessage, []byte(p)) != nil {
							return
						}
					}
					return
				}
				w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
				if r.URL.Query().Get("sid") == "" {
					_, _ = fmt.Fprintf(w, "%d:%s", len(boundaryOpen), boundaryOpen)
					return
				}
				switch polls.Add(1) {
				case 1:
					_, _ = io.WriteString(w, exactWire)
				case 2:
					_, _ = io.WriteString(w, largeWire)
				default:
					_, _ = io.WriteString(w, "1:1")
				}
			}))
			defer ts.Close()
			c, e := (&Dialer{MaxMessageSize: 512, Transports: []transport.Transport{tr}}).Dial(ts.URL, nil)
			require.NoError(t, e)
			defer func() { _ = c.Close() }()
			_, r, e := c.NextReader()
			require.NoError(t, e)
			body, e := io.ReadAll(r)
			require.NoError(t, e)
			require.NoError(t, r.Close())
			require.Equal(t, exactPacket[1:], string(body))
			_, r, e = c.NextReader()
			if e == nil {
				_, e = io.ReadAll(r)
				closeErr := r.Close()
				if e == nil {
					e = closeErr
				}
			}
			require.Error(t, e, "a later oversized message must retain the configured boundary")
		})
	}
}

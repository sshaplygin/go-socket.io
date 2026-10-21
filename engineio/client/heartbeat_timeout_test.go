package client_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	ec "github.com/sshaplygin/go-socket.io/engineio/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	ws "github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func TestMissingPong(t *testing.T) {
	for _, firstPong := range []bool{false, true} {
		t.Run(map[bool]string{false: "never_pong", true: "one_pong"}[firstPong], func(t *testing.T) {
			pings := make(chan struct{}, 100)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = c.Close() }()
				if err = c.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"test","upgrades":[],"pingInterval":20,"pingTimeout":40}`)); err != nil {
					return
				}
				responded := false
				for {
					_, b, err := c.ReadMessage()
					if err != nil {
						return
					}
					if string(b) == "2" {
						select {
						case pings <- struct{}{}:
						default:
						}
						if firstPong && !responded {
							responded = true
							_ = c.WriteMessage(websocket.TextMessage, []byte("3"))
						}
					}
				}
			}))
			defer srv.Close()
			c, err := (&ec.Dialer{Transports: []transport.Transport{ws.Default}}).Dial(srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close() }()
			ended := make(chan error, 1)
			go func() {
				_, r, err := c.NextReader()
				if r != nil {
					_ = r.Close()
				}
				ended <- err
			}()
			select {
			case <-pings:
			case <-time.After(time.Second):
				t.Fatal("no PING")
			}
			select {
			case err := <-ended:
				var cause *ec.CloseError
				if !errors.As(err, &cause) || cause.Reason != ec.ReasonPingTimeout {
					t.Fatalf("expected ping timeout, got %v", err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatalf("no heartbeat timeout after 500ms; window=60ms, firstPong=%v, state=%v, additional PINGs=%d", firstPong, c.ReadyState(), len(pings))
			}
		})
	}
}

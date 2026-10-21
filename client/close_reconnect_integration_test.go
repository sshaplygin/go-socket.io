package client_test

import (
	"net/http/httptest"
	"testing"
	"time"

	sio "github.com/sshaplygin/go-socket.io"
	client "github.com/sshaplygin/go-socket.io/client"
	ws "github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

func TestReconnectFromDisconnect(t *testing.T) {
	for _, mode := range []string{"socket_close", "client_close"} {
		t.Run(mode, func(t *testing.T) {
			s := sio.NewServer()
			defer func() { _ = s.Close() }()
			h := httptest.NewServer(s)
			defer h.Close()
			go func() { _ = s.Serve() }()
			c, err := client.New(h.URL, client.WithReconnection(false), client.WithTransports(ws.Default))
			if err != nil {
				t.Fatal(err)
			}
			connected := make(chan string, 4)
			result := make(chan error, 4)
			c.OnConnect(func(n client.Conn) error { connected <- n.ID(); return nil })
			c.OnDisconnect(func(client.Conn, string) { result <- c.Connect() })
			defer func() { c.OnDisconnect(nil); _ = c.Close() }()
			if err = c.Connect(); err != nil {
				t.Fatal(err)
			}
			var initial string
			select {
			case initial = <-connected:
			case <-time.After(time.Second):
				t.Fatal("initial connect timeout")
			}
			if mode == "socket_close" {
				err = c.Socket("/").Close()
			} else {
				err = c.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-result:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("callback did not return")
			}
			select {
			case replacement := <-connected:
				if replacement == "" || replacement == initial {
					t.Fatalf("expected a fresh connection ID, got %q after %q", replacement, initial)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("Connect returned nil in OnDisconnect, but replacement connection never arrived")
			}
		})
	}
}

// Command echo runs a loopback-only framing oracle for the optional Node tests.
package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gobwas/ws"
	framing "github.com/sshaplygin/go-socket.io/experiments/eio4-websocket"
)

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Hijacker); !ok {
			http.Error(w, "HTTP/1.1 upgrade required", http.StatusNotImplemented)
			return
		}
		raw, buffered, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			return
		}
		defer func() { _ = raw.Close() }()
		if err := raw.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return
		}
		conn, err := framing.New(raw, buffered.Reader, false, 64)
		if err != nil {
			return
		}
		for {
			op, body, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(op, body); err != nil {
				return
			}
		}
	})}
	fmt.Println(listener.Addr().String())
	if err := server.Serve(listener); err != nil {
		log.Fatal(err)
	}
}

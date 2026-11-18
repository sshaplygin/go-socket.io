package main

import (
	"time"

	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/browser"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
)

var _ transport.Transport = (*browser.Transport)(nil)

func main() {
	ws := &browser.Transport{Subprotocols: []string{"chat"}, HandshakeTimeout: time.Second}
	c, err := client.New("http://127.0.0.1", client.WithTransports(polling.Default, ws))
	if err != nil {
		return
	}
	defer c.Close()
}

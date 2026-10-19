package main

import (
	"github.com/sshaplygin/go-socket.io/client"
	"time"
)

var (
	_ func(*client.Client, func(client.Conn) error)    = (*client.Client).OnConnect
	_ func(*client.Socket, string, interface{}) func() = (*client.Socket).On
	_ client.AckFunc                                   = func(...interface{}) {}
)

func main() {
	c, err := client.New("http://127.0.0.1", client.WithTimeout(time.Second))
	if err != nil {
		return
	}
	defer c.Close()
	c.Socket("/chat").On("event", func(c client.Conn, n int, ack client.AckFunc) { ack(n) })
}

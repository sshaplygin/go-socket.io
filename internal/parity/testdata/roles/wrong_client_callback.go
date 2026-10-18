package main

import (
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
)

func main() {
	c, _ := client.New("http://127.0.0.1", nil)
	c.OnConnect(func(socketio.Conn) error { return nil })
}

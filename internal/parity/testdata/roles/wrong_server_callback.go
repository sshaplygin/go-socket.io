package main

import (
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
)

func main() { socketio.NewServer(nil).OnConnect("/", func(client.Conn) error { return nil }) }

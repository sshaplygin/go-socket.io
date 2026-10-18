package main

import (
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
)

func main() { socketio.NewServer().OnConnect("/", func(client.Conn) error { return nil }) }

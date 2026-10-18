package main

import (
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
)

func main() { _ = socketio.NewServer(client.WithTimeout(time.Second)) }

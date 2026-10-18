package main

import (
	"github.com/sshaplygin/go-socket.io/client"
)

func main() { _, _ = client.New("http://127.0.0.1:8000", &client.Options{}) }

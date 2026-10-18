package main

import (
	"github.com/sshaplygin/go-socket.io/engineio"
)

func main() { _ = engineio.NewServer(&engineio.Options{}) }

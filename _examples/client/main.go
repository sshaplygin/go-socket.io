// Command client joins the chat served by any of the other examples with the Go
// client: it adds a user, sends one message, prints every chat event it receives for a second
// and closes.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/sshaplygin/go-socket.io/client"
)

func main() {
	addr := flag.String("addr", "http://127.0.0.1:8000", "address of the server")
	name := flag.String("name", "go-client", "user name")
	message := flag.String("message", "hello from the Go client", "message to send")
	flag.Parse()
	if err := run(*addr, *name, *message); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(addr, name, message string) (err error) {
	cl, err := client.New(addr)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, cl.Close()) }()

	loggedIn := make(chan struct{})
	var loginOnce sync.Once
	cl.OnEvent("login", func(_ client.Conn, data map[string]interface{}) {
		log.Printf("login: %v", data)
		loginOnce.Do(func() { close(loggedIn) })
	})
	for _, event := range []string{"user joined", "user left", "new message", "typing", "stop typing"} {
		cl.OnEvent(event, func(_ client.Conn, data map[string]interface{}) {
			log.Printf("%s: %v", event, data)
		})
	}

	if err := cl.Connect(); err != nil {
		return err
	}

	cl.Emit("add user", name)
	select {
	case <-loggedIn:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("no login event within 5s")
	}

	cl.Emit("new message", message)
	time.Sleep(time.Second) // listen for other users

	return nil
}

// Command client joins the chat served by any of the other examples with the experimental Go
// client: it adds a user, sends one message, prints every chat event it receives for a second
// and closes.
package main

import (
	"flag"
	"log"
	"time"

	"github.com/sshaplygin/go-socket.io/client"
)

func main() {
	addr := flag.String("addr", "http://127.0.0.1:8000", "address of the server")
	name := flag.String("name", "go-client", "user name")
	message := flag.String("message", "hello from the Go client", "message to send")
	flag.Parse()

	cl, err := client.New(*addr, nil)
	if err != nil {
		log.Fatal(err)
	}

	loggedIn := make(chan struct{})
	cl.OnEvent("login", func(_ client.Conn, data map[string]interface{}) {
		log.Printf("login: %v", data)
		close(loggedIn)
	})
	for _, event := range []string{"user joined", "user left", "new message", "typing", "stop typing"} {
		cl.OnEvent(event, func(_ client.Conn, data map[string]interface{}) {
			log.Printf("%s: %v", event, data)
		})
	}

	if err := cl.Connect(); err != nil {
		log.Fatal(err)
	}

	cl.Emit("add user", *name)
	select {
	case <-loggedIn:
	case <-time.After(5 * time.Second):
		log.Fatal("no login event within 5s")
	}

	cl.Emit("new message", *message)
	time.Sleep(time.Second) // listen for other users

	if err := cl.Close(); err != nil {
		log.Fatal(err)
	}
}

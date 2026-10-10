// Command client joins the chat served by any of the other examples with the experimental Go
// client: it adds a user, sends one message, prints every chat event it receives for a second
// and closes.
package main

import (
	"flag"
	"log"
	"time"

	socketio "github.com/sshaplygin/go-socket.io/v2"
)

func main() {
	addr := flag.String("addr", "http://127.0.0.1:8000", "address of the server")
	name := flag.String("name", "go-client", "user name")
	message := flag.String("message", "hello from the Go client", "message to send")
	flag.Parse()

	client, err := socketio.NewClient(*addr, nil)
	if err != nil {
		log.Fatal(err)
	}

	loggedIn := make(chan struct{})
	client.OnEvent("login", func(_ socketio.Conn, data map[string]interface{}) {
		log.Printf("login: %v", data)
		close(loggedIn)
	})
	for _, event := range []string{"user joined", "user left", "new message", "typing", "stop typing"} {
		client.OnEvent(event, func(_ socketio.Conn, data map[string]interface{}) {
			log.Printf("%s: %v", event, data)
		})
	}

	if err := client.Connect(); err != nil {
		log.Fatal(err)
	}

	client.Emit("add user", *name)
	select {
	case <-loggedIn:
	case <-time.After(5 * time.Second):
		log.Fatal("no login event within 5s")
	}

	client.Emit("new message", *message)
	time.Sleep(time.Second) // listen for other users

	if err := client.Close(); err != nil {
		log.Fatal(err)
	}
}

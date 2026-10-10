package main

import (
	"log"
	"net/http"

	socketio "github.com/sshaplygin/go-socket.io/v2"
)

func main() {
	server := socketio.NewServer(nil)

	registerChat(server)

	go func() {
		if err := server.Serve(); err != nil {
			log.Fatalf("socketio listen error: %s\n", err)
		}
	}()
	defer server.Close()

	http.Handle("/socket.io/", server)
	http.Handle("/", http.FileServer(http.Dir("../asset")))

	log.Println("Serving at localhost:8000...")
	log.Fatal(http.ListenAndServe(":8000", nil))
}

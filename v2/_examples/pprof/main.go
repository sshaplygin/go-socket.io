package main

import (
	"log"
	"net/http"
	"net/http/pprof"
	_ "net/http/pprof"

	socketio "github.com/sshaplygin/go-socket.io/v2"
	"github.com/sshaplygin/go-socket.io/v2/engineio"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/v2/engineio/transport/websocket"
)

var allowOriginFunc = func(r *http.Request) bool {
	return true
}

func main() {
	opts := &engineio.Options{
		Transports: []transport.Transport{
			&polling.Transport{
				CheckOrigin: allowOriginFunc,
			},
			&websocket.Transport{
				CheckOrigin: allowOriginFunc,
			},
		},
	}

	server := socketio.NewServer(opts)

	registerChat(server)

	debugMux := http.NewServeMux()

	debugMux.HandleFunc("/pprof/*", pprof.Index)
	debugMux.HandleFunc("/pprof/cmdline", pprof.Cmdline)
	debugMux.HandleFunc("/pprof/profile", pprof.Profile)
	debugMux.HandleFunc("/pprof/symbol", pprof.Symbol)
	debugMux.HandleFunc("/pprof/trace", pprof.Trace)

	go func() {
		log.Println("Serving debug at :8001...")

		if err := http.ListenAndServe(":8001", debugMux); err != nil {
			log.Fatalf("debug serve error: %s\n", err)
		}
	}()

	go func() {
		log.Println("Serving socketio...")

		if err := server.Serve(); err != nil {
			log.Fatalf("socketio serve error: %s\n", err)
		}
	}()
	defer server.Close()

	http.Handle("/socket.io/", server)
	http.Handle("/", http.FileServer(http.Dir("../asset")))

	log.Println("Serving at :8000...")
	log.Fatal(http.ListenAndServe(":8000", nil))
}

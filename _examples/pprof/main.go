package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"net/http/pprof"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

var allowOriginFunc = func(r *http.Request) bool {
	return true
}

func main() {
	addr := flag.String("addr", ":8000", "HTTP listen address")
	debugAddr := flag.String("debug-addr", ":8001", "pprof listen address")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *addr, *debugAddr); err != nil {
		log.Print(err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context, addr, debugAddr string) error {
	opts := []engineio.Option{engineio.WithTransports([]transport.Transport{
		&polling.Transport{
			CheckOrigin: allowOriginFunc,
		},
		&websocket.Transport{
			CheckOrigin: allowOriginFunc,
		},
	}...)}

	server := socketio.NewServer(socketio.WithEngineOptions(opts...))

	registerChat(server)

	debugMux := http.NewServeMux()

	debugMux.HandleFunc("/pprof/*", pprof.Index)
	debugMux.HandleFunc("/pprof/cmdline", pprof.Cmdline)
	debugMux.HandleFunc("/pprof/profile", pprof.Profile)
	debugMux.HandleFunc("/pprof/symbol", pprof.Symbol)
	debugMux.HandleFunc("/pprof/trace", pprof.Trace)

	mux := http.NewServeMux()
	mux.Handle("/socket.io/", server)
	mux.Handle("/", http.FileServer(http.Dir("../asset")))
	return serve(ctx, server,
		&http.Server{Addr: addr, Handler: mux},
		&http.Server{Addr: debugAddr, Handler: debugMux},
	)
}

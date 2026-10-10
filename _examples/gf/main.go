package main

import (
	"log"

	"github.com/gogf/gf/frame/g"
	"github.com/gogf/gf/net/ghttp"

	socketio "github.com/sshaplygin/go-socket.io"
)

func cors(r *ghttp.Request) {
	r.Response.CORSDefault()
	r.Middleware.Next()
}

func main() {
	s := g.Server()

	server := socketio.NewServer(nil)

	s.BindMiddlewareDefault(cors)
	s.BindHandler("/socket.io/", func(r *ghttp.Request) {
		server.ServeHTTP(r.Response.Writer, r.Request)
	})

	registerChat(server)

	go func() {
		if err := server.Serve(); err != nil {
			log.Fatalf("socketio listen error: %s\n", err)
		}
	}()
	defer server.Close()

	s.SetPort(8000)
	s.Run()
}

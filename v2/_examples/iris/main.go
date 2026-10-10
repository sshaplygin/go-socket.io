// Package main runs a go-socket.io based websocket server with Iris web server.
package main

import (
	"log"

	"github.com/kataras/iris/v12"

	socketio "github.com/sshaplygin/go-socket.io"
)

func main() {
	app := iris.New()

	server := socketio.NewServer(nil)

	registerChat(server)

	go func() {
		if err := server.Serve(); err != nil {
			log.Fatalf("socketio listen error: %s\n", err)
		}
	}()
	defer server.Close()

	app.HandleMany("GET POST", "/socket.io/{any:path}", iris.FromStd(server))
	app.HandleDir("/", "../asset")

	if err := app.Run(
		iris.Addr(":8000"),
		iris.WithoutPathCorrection,
		iris.WithoutServerError(iris.ErrServerClosed),
	); err != nil {
		log.Fatal("failed run app: ", err)
	}
}

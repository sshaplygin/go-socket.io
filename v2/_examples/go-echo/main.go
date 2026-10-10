package main

import (
	"github.com/labstack/echo"

	socketio "github.com/sshaplygin/go-socket.io/v2"
)

func main() {
	server := socketio.NewServer(nil)

	registerChat(server)

	go server.Serve()
	defer server.Close()

	e := echo.New()
	e.HideBanner = true

	e.Static("/", "../asset")
	e.Any("/socket.io/", func(context echo.Context) error {
		server.ServeHTTP(context.Response(), context.Request())
		return nil
	})
	e.Logger.Fatal(e.Start(":8000"))
}

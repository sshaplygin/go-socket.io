package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	socketio "github.com/sshaplygin/go-socket.io"
)

func main() {
	router := gin.New()

	server := socketio.NewServer(nil)

	registerChat(server)

	go func() {
		if err := server.Serve(); err != nil {
			log.Fatalf("socketio listen error: %s\n", err)
		}
	}()
	defer server.Close()

	router.GET("/socket.io/*any", gin.WrapH(server))
	router.POST("/socket.io/*any", gin.WrapH(server))
	router.StaticFS("/public", http.Dir("../asset"))

	if err := router.Run(":8000"); err != nil {
		log.Fatal("failed run app: ", err)
	}
}

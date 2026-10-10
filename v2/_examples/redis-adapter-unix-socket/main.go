package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	socketio "github.com/sshaplygin/go-socket.io/v2"
)

func main() {
	router := gin.New()

	server := socketio.NewServer(nil)

	_, err := server.Adapter(&socketio.RedisAdapterOptions{
		Addr:    "/tmp/docker/redis.sock",
		Network: "unix",
	})
	if err != nil {
		log.Println("error:", err)
		return
	}

	registerChat(server)

	go server.Serve()
	defer server.Close()

	router.GET("/socket.io/*any", gin.WrapH(server))
	router.POST("/socket.io/*any", gin.WrapH(server))
	router.StaticFS("/public", http.Dir("../asset"))

	if err := router.Run(); err != nil {
		log.Fatal("failed run app: ", err)
	}
}

module github.com/sshaplygin/go-socket.io/_examples/go-echo

go 1.26.0

require (
	github.com/labstack/echo/v5 v5.4.4
	github.com/sshaplygin/go-socket.io v0.0.0-00010101000000-000000000000
)

require (
	github.com/gorilla/websocket v1.5.3 // indirect
	golang.org/x/net v0.61.0 // indirect
)

replace github.com/sshaplygin/go-socket.io => ../../

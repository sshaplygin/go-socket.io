module github.com/sshaplygin/go-socket.io/experiments/ws-bench

go 1.22

require (
	github.com/gobwas/ws v1.4.0
	github.com/gorilla/websocket v1.5.3
	github.com/sshaplygin/go-socket.io/experiments/eio4-websocket v0.0.0
)

require (
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	golang.org/x/sys v0.6.0 // indirect
)

replace github.com/sshaplygin/go-socket.io/experiments/eio4-websocket => ../eio4-websocket

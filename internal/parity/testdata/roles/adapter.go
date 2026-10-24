package main

import (
	"time"

	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/adapter"
	redisadapter "github.com/sshaplygin/go-socket.io/adapter/redis"
)

type externalAdapter struct{ *adapter.Memory }

func (*externalAdapter) BindSocketLookup(func(string) adapter.Socket) {}

var _ adapter.Adapter = (*externalAdapter)(nil)
var _ adapter.SocketLookupBinder = (*externalAdapter)(nil)
var _ adapter.Socket = socketio.Conn(nil)
var _ func(...redisadapter.Option) adapter.Factory = redisadapter.New
var _ func(time.Duration) redisadapter.Option = redisadapter.WithDialTimeout

func main() {
	server := socketio.NewServer(socketio.WithAdapter(func(string) (adapter.Adapter, error) { return &externalAdapter{adapter.NewMemory()}, nil }))
	defer server.Close()
	server.ForEach("/", "room", func(socket adapter.Socket) {
		socket.EmitWithOptions(adapter.Options{Volatile: true, NoCompress: true}, "news")
	})
	server.SetAdapter(redisadapter.New(redisadapter.WithAddr("127.0.0.1:6379"), redisadapter.WithDialTimeout(time.Second)))
}

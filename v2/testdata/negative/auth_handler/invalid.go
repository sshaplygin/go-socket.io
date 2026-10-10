package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/v2"
)

var _ = sio.Auth[string](func(context.Context, *sio.Socket, int) error { return nil })

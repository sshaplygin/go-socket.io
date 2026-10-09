package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io"
)

var _ = sio.Hooks{EventStart: func(context.Context, sio.EventInfo) {}}

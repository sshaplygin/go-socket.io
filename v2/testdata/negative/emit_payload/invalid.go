package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io"
)

var _ = sio.NewEvent[string]("e").Emit(context.Background(), nil, 42)

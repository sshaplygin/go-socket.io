package negative

import (
	"context"

	sio "github.com/sshaplygin/go-socket.io"
)

var _ *sio.Namespace = (&sio.Server{}).Namespace(context.Background(), "/")

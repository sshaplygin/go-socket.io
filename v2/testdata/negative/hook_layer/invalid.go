package negative

import (
	sio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
)

var _ = sio.Options{Hooks: &engineio.Hooks{}}

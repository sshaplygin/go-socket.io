package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io"
)

func invalid() {
	result, _ := sio.NewAckEvent[string, int]("e").EmitWithAck(context.Background(), nil, "")
	var _ string = result
}

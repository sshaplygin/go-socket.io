package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/experiments/v2-api"
)

func invalid() {
	result, _ := sio.NewAckEvent[string, int]("e").EmitWithAck(context.Background(), nil, "")
	var _ string = result
}

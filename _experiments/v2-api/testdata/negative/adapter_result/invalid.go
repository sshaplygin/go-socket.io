package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/experiments/v2-api"
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/fixtures/externaladapter"
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/parser"
)

type Invalid struct{ externaladapter.Adapter }

func (*Invalid) Broadcast(context.Context, parser.Packet, sio.BroadcastOptions) error { return nil }

var _ sio.Adapter = (*Invalid)(nil)

package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io/v2"
	"github.com/sshaplygin/go-socket.io/v2/internal/fixtures/externaladapter"
	"github.com/sshaplygin/go-socket.io/v2/parser"
)

type Invalid struct{ externaladapter.Adapter }

func (*Invalid) Broadcast(context.Context, parser.Packet, sio.BroadcastOptions) error { return nil }

var _ sio.Adapter = (*Invalid)(nil)

package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/internal/fixtures/externaladapter"
	"github.com/sshaplygin/go-socket.io/parser"
)

type Invalid struct{ externaladapter.Adapter }

func (*Invalid) Broadcast(context.Context, parser.Packet, sio.BroadcastOptions) error { return nil }

var _ sio.Adapter = (*Invalid)(nil)

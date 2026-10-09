package negative

import (
	"context"
	sio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/parser"
)

type Invalid struct{}

func (*Invalid) Deliver(context.Context, sio.SocketID, parser.Packet) {}
func (*Invalid) Snapshot(sio.SocketID) (sio.RemoteSocket, bool)       { return sio.RemoteSocket{}, false }

var _ sio.LocalSockets = (*Invalid)(nil)

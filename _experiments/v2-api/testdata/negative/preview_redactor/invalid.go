package negative

import (
	"context"
	"github.com/sshaplygin/go-socket.io/experiments/v2-api/engineio"
)

type Invalid struct{}

func (Invalid) Enabled(context.Context) bool { return true }
func (Invalid) Redact(context.Context, engineio.SessionInfo, engineio.PacketInfo, []byte, int) string {
	return ""
}

var _ engineio.PayloadRedactor = Invalid{}

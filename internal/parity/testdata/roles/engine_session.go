package main

import (
	"errors"
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"io"
)

var _ func(engineio.Conn) error = engineio.Abort
var _ error = (*engineio.CloseError)(nil)
var _ engineio.IDGenerator = (*engineio.DefaultIDGenerator)(nil)
var _ interface {
	NextWriterCompress(eioparser.FrameType, bool) (io.WriteCloser, error)
} = engineio.CompressWriter(nil)

func main() {
	generator := &engineio.DefaultIDGenerator{}
	server := socketio.NewServer(socketio.WithEngineOptions(engineio.WithSessionIDGenerator(generator), engineio.WithHooks(&engineio.Hooks{Connection: func(c engineio.Conn) { var state engineio.ReadyState = c.ReadyState(); _ = state == engineio.StateOpen }})))
	defer server.Close()
	var closed *engineio.CloseError
	if errors.As(errors.New("transport"), &closed) {
		var reason engineio.CloseReason = closed.Reason
		_ = reason == engineio.ReasonTransportError
	}
}

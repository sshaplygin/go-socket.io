package socketio

import (
	"errors"
	"fmt"
)

// connect errors.
var (
	errUnavailableRootHandler = errors.New("root ('/') doesn't have a namespace handler")

	errFailedConnectNamespace = errors.New("failed connect to namespace without handler")
)

// common connection dispatch errors.
var (
	errHandleDispatch = errors.New("handler dispatch error")

	errDecodeArgs = errors.New("decode args error")
)

// errWriteBufferFull is reported to OnError when a connection's outbound
// queue is full; the connection is then closed. If the queue overflows while
// Close runs OnDisconnect, the report may or may not reach OnError and the
// engine.io connection may be closed twice. Once Close has run OnDisconnect,
// Emit drops packets without a report.
var errWriteBufferFull = errors.New("write buffer full")

type errorMessage struct {
	namespace string

	err error
}

func (e errorMessage) Error() string {
	return fmt.Sprintf("error in namespace: (%s) with error: (%s)", e.namespace, e.err.Error())
}

func newErrorMessage(namespace string, err error) *errorMessage {
	return &errorMessage{
		namespace: namespace,
		err:       err,
	}
}

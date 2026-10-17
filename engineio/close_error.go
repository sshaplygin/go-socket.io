package engineio

import "github.com/sshaplygin/go-socket.io/engineio/session"

// CloseError and CloseReason describe the first cause of a session's close.
type (
	CloseError  = session.CloseError
	CloseReason = session.CloseReason
)

const (
	ReasonTransportClose = session.ReasonTransportClose
	ReasonPingTimeout    = session.ReasonPingTimeout
	ReasonTransportError = session.ReasonTransportError
	ReasonForcedClose    = session.ReasonForcedClose
	ReasonServerShutdown = session.ReasonServerShutdown
)

package engineio

import "github.com/sshaplygin/go-socket.io/engineio/session"

// ReadyState is a connection's session state.
type ReadyState = session.ReadyState

const (
	StateOpening = session.StateOpening
	StateOpen    = session.StateOpen
	StateClosing = session.StateClosing
	StateClosed  = session.StateClosed
)

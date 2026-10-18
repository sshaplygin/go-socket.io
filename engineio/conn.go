package engineio

import (
	sessionImpl "github.com/sshaplygin/go-socket.io/engineio/internal/session"
)

// Conn is an accepted server-side Engine.IO connection.
type Conn = sessionImpl.Conn

// CompressWriter optionally writes packets with a per-packet compression flag.
// NextWriter on these connections is NextWriterCompress with compress true.
type CompressWriter = sessionImpl.CompressWriter

// ReadyState is the state of a session.
type ReadyState = sessionImpl.ReadyState

const (
	StateOpening = sessionImpl.StateOpening
	StateOpen    = sessionImpl.StateOpen
	StateClosing = sessionImpl.StateClosing
	StateClosed  = sessionImpl.StateClosed
)

// CloseReason is why a session closed: the first cause the session observed.
type CloseReason = sessionImpl.CloseReason

const (
	ReasonTransportClose = sessionImpl.ReasonTransportClose
	ReasonPingTimeout    = sessionImpl.ReasonPingTimeout
	ReasonTransportError = sessionImpl.ReasonTransportError
	ReasonForcedClose    = sessionImpl.ReasonForcedClose
	ReasonServerShutdown = sessionImpl.ReasonServerShutdown
)

// CloseError is returned, wrapped, by NextReader and NextWriter once a session
// closes, including the first call that observes the close. Previously obtained
// readers and writers keep returning their own errors.
type CloseError = sessionImpl.CloseError

// Abort immediately closes a server session; custom engines receive ordinary Close.
func Abort(c Conn) error {
	if s, ok := c.(*sessionImpl.Session); ok {
		return s.Abort()
	}
	return c.Close()
}

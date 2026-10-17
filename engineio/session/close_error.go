package session

// CloseReason is why a session closed: the first cause the session observed.
type CloseReason string

const (
	// ReasonTransportClose means the client sent a CLOSE packet.
	ReasonTransportClose CloseReason = "transport close"
	// ReasonPingTimeout means no packet arrived within PingInterval + PingTimeout.
	ReasonPingTimeout CloseReason = "ping timeout"
	// ReasonTransportError means a transport read or write failed before the deadline.
	ReasonTransportError CloseReason = "transport error"
	// ReasonForcedClose means Session.Close was called.
	ReasonForcedClose CloseReason = "forced close"
	// ReasonServerShutdown means engineio.Server.Close closed the session.
	ReasonServerShutdown CloseReason = "server shutting down"
)

// CloseError is returned, wrapped, by NextReader and NextWriter once a session
// closes, including the first call that observes the close. Previously obtained
// readers and writers keep returning their own errors.
type CloseError struct {
	Reason CloseReason
	// Err is the transport error for ReasonTransportError, io.EOF for
	// ReasonTransportClose, and nil for the other reasons.
	Err error
}

// Error describes the close reason and, when present, its underlying error.
func (e *CloseError) Error() string {
	message := "engineio: session closed: " + string(e.Reason)
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

// Unwrap returns the underlying transport error.
func (e *CloseError) Unwrap() error { return e.Err }

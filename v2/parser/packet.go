package parser

import "strconv"

// Type is the base type of a Packet. A binary event or binary acknowledgement is an
// Event or Ack that carries attachments; the wire digits 5 and 6 never appear as a
// Type.
type Type byte

const (
	// Connect is a namespace CONNECT, carrying an optional JSON object.
	Connect Type = iota
	// Disconnect is a namespace DISCONNECT, carrying no data.
	Disconnect
	// Event is an EVENT, or a BINARY_EVENT when it has attachments.
	Event
	// Ack is an ACK, or a BINARY_ACK when it has attachments.
	Ack
	// ConnectError is a CONNECT_ERROR, carrying a JSON object or string.
	ConnectError
)

// MaxID is the largest acknowledgement ID, the largest integer a JavaScript number
// holds exactly (2^53-1).
const MaxID uint64 = 1<<53 - 1

// String returns the protocol name of t.
func (t Type) String() string {
	switch t {
	case Connect:
		return "CONNECT"
	case Disconnect:
		return "DISCONNECT"
	case Event:
		return "EVENT"
	case Ack:
		return "ACK"
	case ConnectError:
		return "CONNECT_ERROR"
	}
	return "Type(" + strconv.Itoa(int(t)) + ")"
}

package parser

// Type is the Socket.IO packet type, written as one digit.
type Type byte

const (
	// Connect asks to join a namespace or confirms the join.
	Connect Type = iota
	// Disconnect leaves a namespace.
	Disconnect
	// Event carries an event name and its arguments.
	Event
	// Ack carries the reply arguments of an acknowledged event.
	Ack
	// Error carries an error message for a namespace, such as the reason a
	// namespace connection was refused.
	Error

	// BinaryEvent type
	binaryEvent
	// BinaryAck type
	binaryAck
)

// Header is the part of a packet that precedes its JSON payload.
type Header struct {
	// Type is the packet type. Binary event and ACK packets are represented as
	// Event and Ack: the Encoder selects the binary variant when the payload has
	// attachments, and the Decoder reports it as the plain type.
	Type Type
	// ID is the acknowledgement ID: the ID an Event asks to be acknowledged
	// with, or the ID an Ack answers. It is valid only when NeedAck is set.
	ID uint64
	// NeedAck reports whether the packet carries ID. The Encoder writes ID only
	// when NeedAck is set; the Decoder sets it when the packet has an ID.
	NeedAck bool
	// Namespace is the namespace of the packet, such as "/chat"; empty means
	// the main namespace "/".
	Namespace string
	// Query is the query string that follows "?" in the namespace of a
	// received packet, without the "?": "x=1" for the CONNECT packet
	// 0/nsp?x=1. The Encoder does not write it.
	Query string
}

// Payload pairs a packet header with its arguments. The Encoder and the
// Decoder of this package do not use it.
type Payload struct {
	// Header is the packet header.
	Header Header

	// Data holds the packet arguments.
	Data []interface{}
}

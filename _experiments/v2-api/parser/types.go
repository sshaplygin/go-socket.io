// Package parser describes the proposed packet boundary. It implements no codec.
package parser

import "encoding/json"

// Type is a Socket.IO protocol v5 packet type.
type Type uint8

const (
	Connect Type = iota
	Disconnect
	Event
	Ack
	ConnectError
	BinaryEvent
	BinaryAck
)

// Arguments preserves positional arguments and their separate binary attachments.
// Ownership, placeholder validation and typed conversion require runtime codecs.
type Arguments struct {
	Values      []json.RawMessage
	Attachments [][]byte
}

// Packet preserves an optional ack ID, including the valid ID zero.
// Data is the lazily decoded JSON payload; attachments travel with it as one group.
type Packet struct {
	Type        Type
	Namespace   string
	ID          *uint64
	Data        json.RawMessage
	Attachments [][]byte
}

// BinaryValue identifies bytes that an argument codec must encode as attachments.
type BinaryValue interface{ SocketIOBinary() []byte }

// ArgumentCodec is a proposed typed conversion boundary, not an implementation.
type ArgumentCodec[T any] struct {
	Encode func(T) (Arguments, error)
	Decode func(Arguments) (T, error)
}

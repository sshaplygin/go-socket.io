package parser

import "encoding/json"

// Arguments carries the positional arguments of an event or acknowledgement and
// the binary attachments that belong to them. Ownership of the slices,
// placeholder validation and typed conversion belong to the codec (roadmap 2.3P);
// this declaration copies and validates nothing.
type Arguments struct {
	Values      []json.RawMessage
	Attachments [][]byte
}

// Packet is the value form of a Socket.IO packet used by the v2 root API and by
// adapters. ID is nil when the packet requests no acknowledgement; zero is a valid
// ID. Data is the lazily decoded JSON payload and Attachments travel with it as one
// message group. A packet with attachments is a binary event or binary ack on the
// wire; Type holds the base type. The legacy Header and Payload types remain for
// the current codec until 2.3P replaces it.
type Packet struct {
	Type        Type
	Namespace   string
	ID          *uint64
	Data        json.RawMessage
	Attachments [][]byte
}

// BinaryValue marks bytes that an argument codec must send as an attachment,
// including a value nested in a struct, slice or map.
type BinaryValue interface{ SocketIOBinary() []byte }

// ArgumentCodec converts one Go value to and from positional arguments. A typed
// event descriptor binds one when the runtime lands (roadmap 2.3); dispatch never
// reflects over handlers.
type ArgumentCodec[T any] struct {
	Encode func(T) (Arguments, error)
	Decode func(Arguments) (T, error)
}

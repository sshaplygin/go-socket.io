package socketio

import (
	"context"

	"github.com/sshaplygin/go-socket.io/parser"
)

// Endpoint is the packet send and acknowledgement surface shared by server sockets
// and the Go client. It lets event descriptors work on both sides without the root
// package importing the client package. A runtime implementation must copy encoded
// data before a send returns and queue binary headers and attachments as one
// indivisible message group.
type Endpoint interface {
	SendPacket(context.Context, parser.Packet) error
	RequestAck(context.Context, parser.Packet) (parser.Arguments, error)
}

// ClientRegistration is what a client implements so that descriptors can register
// handlers on it. RegisterEvent is the raw escape hatch behind HandleClient.
type ClientRegistration interface {
	Endpoint
	RegisterEvent(string, ClientRawHandler) error
}

// RawHandler handles any event of a namespace on the server side without a typed
// schema. It is the explicit raw escape hatch and is not selected by event name.
type RawHandler func(context.Context, *Socket, RawEvent) error

// ClientRawHandler is the client-side counterpart of RawHandler.
type ClientRawHandler func(context.Context, Endpoint, RawEvent) error

// RawAck responds to a raw event using the application's positional acknowledgement
// convention.
type RawAck interface {
	Respond(context.Context, parser.Arguments) error
}

// RawEvent exposes the event name, the positional JSON arguments with their
// attachments and an optional responder. Ack is nil when the peer did not request an
// acknowledgement.
type RawEvent struct {
	Name string
	Args parser.Arguments
	Ack  RawAck
}

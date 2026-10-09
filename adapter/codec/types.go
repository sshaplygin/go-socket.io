package codec

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Errors are matched with errors.Is. The returned error carries the detail.
var (
	// ErrMalformed marks input that is not a well-formed message of the format:
	// truncated or invalid MessagePack or JSON, a missing required field, a field of the
	// wrong type, an extension type.
	ErrMalformed = errors.New("adapter codec: malformed message")
	// ErrUnsupported marks a well-formed message of a kind this package does not
	// implement: a request or response type outside the supported set (see the package
	// documentation) or a MessagePack request or response.
	ErrUnsupported = errors.New("adapter codec: unsupported message")
	// ErrLimit marks input that exceeds a Limits bound.
	ErrLimit = errors.New("adapter codec: limit exceeded")
	// ErrInvalid marks a value the encoder refuses to put on the wire.
	ErrInvalid = errors.New("adapter codec: invalid value")
)

// Default values of the Limits fields.
const (
	DefaultMaxMessageBytes = 8 << 20
	DefaultMaxDepth        = 32
	DefaultMaxAttachments  = 64
)

// Limits bound what a decoder accepts from a peer. A zero field selects its default,
// a negative one is rejected with ErrInvalid. The bounds apply to the decoded
// message: the decoder reads at most MaxMessageBytes, allocates no more than the
// input length demands, and never recurses deeper than MaxDepth.
type Limits struct {
	// MaxMessageBytes is the largest accepted message, in bytes.
	MaxMessageBytes int
	// MaxDepth is the deepest accepted nesting of arrays and maps in a MessagePack
	// message. The message itself is depth 0.
	MaxDepth int
	// MaxAttachments is the largest number of binary values in one packet.
	MaxAttachments int
}

func (l Limits) resolve() (Limits, error) {
	if l.MaxMessageBytes < 0 || l.MaxDepth < 0 || l.MaxAttachments < 0 {
		return l, fmt.Errorf("%w: negative limit %+v", ErrInvalid, l)
	}
	if l.MaxMessageBytes == 0 {
		l.MaxMessageBytes = DefaultMaxMessageBytes
	}
	if l.MaxDepth == 0 {
		l.MaxDepth = DefaultMaxDepth
	}
	if l.MaxAttachments == 0 {
		l.MaxAttachments = DefaultMaxAttachments
	}
	return l, nil
}

func (l Limits) checkSize(msg []byte) error {
	if len(msg) > l.MaxMessageBytes {
		return fmt.Errorf("%w: message of %d bytes, limit %d", ErrLimit, len(msg), l.MaxMessageBytes)
	}
	return nil
}

// Flags are the flags of a broadcast that a peer sends in the "flags" object. The
// Node adapter never publishes the local flag. A nil field is absent from the wire.
// Nothing in the Go Adapter contract produces these yet (BroadcastFlags carries only
// Local); they exist so that a Node packet decodes without loss.
type Flags struct {
	Volatile *bool  `json:"volatile,omitempty"`
	Compress *bool  `json:"compress,omitempty"`
	Timeout  *int64 `json:"timeout,omitempty"`
}

// Options are the broadcast options of the wire: the "opts" object of a broadcast
// and of the requests that select sockets. Rooms and Except are room names; an
// empty Rooms selects every socket. Flags is nil when the peer sent none; a
// broadcast is always written with a flags object, a request never is.
type Options struct {
	Rooms  []string
	Except []string
	Flags  *Flags
}

// RemoteSocket is the wire form of a socket snapshot, an entry of the "sockets"
// array of a fetchSockets response. It is a local type because this package never
// imports the root package; the adapter converts to and from socketio.RemoteSocket.
// Handshake is a JSON object, Data is JSON, and both are nil when absent.
//
// The decoder passes Handshake through unchanged, including the auth key and the
// authorization, cookie and proxy-authorization headers that a Node peer sends. The
// caller is responsible for socketio.RedactHandshake.
type RemoteSocket struct {
	ID        string
	Rooms     []string
	Handshake json.RawMessage
	Data      json.RawMessage
}

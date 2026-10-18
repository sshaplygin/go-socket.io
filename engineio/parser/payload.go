package parser

import "github.com/sshaplygin/go-socket.io/engineio/parser/internal/payload"

// PayloadWriter lends a destination for one encoded packet. ReleasePayloadWriter
// receives the encoding/write error, or nil, after that packet; its error takes
// precedence. AcquirePayloadWriter errors are returned without a release.
type PayloadWriter = payload.Writer

// PayloadReader lends an entire Engine.IO payload and reports whether it uses
// binary payload framing. ReleasePayloadReader runs after the final packet is
// closed or decoding fails; its error takes precedence. Acquisition errors are
// returned without a release.
type PayloadReader = payload.Reader

// PayloadEncoder writes Engine.IO v3 polling payload packets. Use one writer at a
// time and close each packet writer before acquiring the next.
type PayloadEncoder = payload.Encoder

// PayloadDecoder reads Engine.IO v3 polling payload packets. Close each packet
// reader before acquiring the next; Close drains unread packet bytes. A decode
// or release failure is terminal: later reader, read and close calls return the
// saved error without reusing or releasing the source again. Non-EOF body read
// failures release the source immediately, including reads returning data and an
// error together. EOF ends the current packet. Acquisition failures
// may be retried because no payload was acquired.
type PayloadDecoder = payload.Decoder

// PayloadEncoderOption configures a payload encoder.
type PayloadEncoderOption = payload.EncoderOption

// WithBinarySupport selects binary payload framing. The default is true;
// transports pass the support negotiated with their peer.
func WithBinarySupport(enabled bool) PayloadEncoderOption { return payload.WithBinarySupport(enabled) }

// NewPayloadEncoder binds a writer with binary payload support enabled.
func NewPayloadEncoder(w PayloadWriter, opts ...PayloadEncoderOption) *PayloadEncoder {
	return payload.NewEncoder(w, opts...)
}

// NewPayloadDecoder binds a payload source. Each acquired reader is released after its last packet or a decoding failure.
func NewPayloadDecoder(r PayloadReader) *PayloadDecoder { return payload.NewDecoder(r) }

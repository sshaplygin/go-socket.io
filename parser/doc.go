// Package parser implements the Socket.IO protocol 5 wire codec and the value forms
// the v2 API passes between the runtime, adapters and application codecs.
//
// A message is one text frame, the envelope, followed by as many binary frames as the
// envelope announces. Encode and Decode convert a Packet to and from a complete
// message; Assembler accepts the frames of a connection one at a time. Both are
// bounded by Limits, validate namespaces, acknowledgement IDs, JSON payload shapes
// and binary placeholders, and never share memory with their callers: a value passed
// to a function is borrowed for the call, a value it returns is owned by the caller.
// The functions and codecs of this package keep no mutable state, so a Packet or an
// Arguments value that several connections share may be encoded concurrently as long
// as no goroutine writes to it. An Assembler is the exception: one per connection,
// not safe for concurrent use.
//
// # Packets and arguments
//
// Packet.Data is the payload exactly as sent, an array for EVENT and ACK. EventPacket
// and AckPacket build a Packet from Arguments, EventArguments and AckArguments split
// one, and Concat and Arguments.Slice join and divide the arguments of a codec that
// expands to several positional arguments. JSON is an ArgumentCodec for one argument
// that moves every BinaryValue of a Go value into an attachment. The package makes no
// choice about which codec belongs to which event; that is the root package's.
//
// # Differences from the Node.js parser
//
// The wire format is that of socket.io-parser 4.2.7 (protocol 5), checked against it
// by testdata/oracle. Deliberate differences:
//
//   - Missing EVENT, ACK or CONNECT_ERROR data is rejected, although Node's decoder
//     accepts an absent payload. CONNECT may omit data; DISCONNECT must omit it.
//   - A namespace in a header must start with "/" and end its header with a comma, and
//     contains no comma, NUL, CR or LF. The default namespace is returned as "/".
//   - Attachment counts are unsigned decimal digits and positive; "1.0" and "1e0" are
//     rejected, and so is a count of zero, as by 4.2.7.
//   - An acknowledgement ID above 2^53-1 is rejected. Leading zeros are accepted and
//     written canonically.
//   - A placeholder index must be an integer name of an attachment; a fractional index
//     is rejected where Node yields an undefined attachment value.
//   - Envelopes must be valid UTF-8 and valid JSON of bounded depth. The attachment
//     count, byte and depth limits are checked here; Node's decoder waits for missing
//     attachments instead of failing, so the Assembler adds a deadline.
//   - Unreferenced attachments and repeated placeholder indices are accepted, as by
//     Node; each decoded reference owns its bytes where Node shares one buffer.
//   - JSON decoding replaces an unpaired UTF-16 surrogate escape in a string by U+FFFD
//     when a value is decoded into a Go string; the raw Packet.Data keeps the escape.
package parser

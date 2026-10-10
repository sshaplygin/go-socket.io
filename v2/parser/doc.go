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
// # Wire format
//
// The wire format is that of socket.io-parser 4.2.7 (protocol 5). The deliberate
// differences from the Node.js parser are listed in docs/PROTOCOL.md of the
// repository, which owns them; testdata/oracle checks the codec against Node.
package parser

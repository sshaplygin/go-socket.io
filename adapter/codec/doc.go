// Package codec encodes and decodes the messages that Socket.IO servers exchange
// through a message broker, in the format of the non-sharded Node Redis adapter
// (@socket.io/redis-adapter 8.3.0). It is the shared message format of the broker
// adapters (roadmap 2.2 and 4b): an adapter module chooses channels or subjects and
// does the transport, this package defines the bytes.
//
// It depends on package parser for the packet value form and never on the root
// socketio package, so it declares its own wire types (Options, Flags, RemoteSocket,
// Request, Response). It does no I/O, keeps no state and is safe for concurrent use.
//
// # Messages
//
// A Broadcast is a MessagePack array [uid, packet, opts], published to the broadcast
// channel of a namespace or room (EncodeBroadcast, DecodeBroadcast). Binary values are
// MessagePack bin values inside the packet's data. In a parser.Packet they appear as
// {"_placeholder":true,"num":N} objects with the bytes in Attachments[N], numbered in
// order of appearance; the encoder accepts any numbering that uses each attachment
// once. A map that already has the placeholder shape is not a representable event
// argument: the decoder refuses it and the encoder reads it as a placeholder.
//
// A Request is a JSON object on the request channel (EncodeRequest, DecodeRequest)
// and a Response a JSON object on a response channel (EncodeResponse,
// DecodeResponse). Supported are the request types ALL_ROOMS, REMOTE_JOIN,
// REMOTE_LEAVE, REMOTE_DISCONNECT, REMOTE_FETCH and SERVER_SIDE_EMIT, and the
// responses that list rooms, socket ids or socket snapshots. Everything else the
// pinned adapter sends decodes to ErrUnsupported: the legacy SOCKETS request (only its
// response, a list of socket ids, is read), the cluster broadcast with acknowledgements (BROADCAST and its
// MessagePack request, BROADCAST_CLIENT_COUNT and BROADCAST_ACK responses) and the
// server-side emit acknowledgement response. Channel and subject names are not part of
// this package.
//
// # Untrusted input
//
// Decoders take bytes from a peer. They accept at most Limits.MaxMessageBytes, check
// every declared string, binary, array and map length against the bytes that are
// left before allocating, refuse nesting deeper than Limits.MaxDepth and more than
// Limits.MaxAttachments binary values, refuse MessagePack extension types, non-finite
// numbers, invalid UTF-8 and trailing bytes, and return owned values that alias
// nothing of the input. Errors match ErrMalformed, ErrUnsupported, ErrLimit or
// ErrInvalid with errors.Is. The decoders check the structure of a message and not its
// meaning: room names, the namespace, event names and the snapshots' contents are for
// the adapter to validate.
//
// A decoded RemoteSocket.Handshake is exactly what the peer sent. The guarantee of
// socketio.RemoteSocket (no auth key, no authorization, cookie or proxy-authorization
// header) is established by the adapter, which calls socketio.RedactHandshake on every
// snapshot it decodes; this package does not import the root and cannot.
//
// # Fixtures
//
// testdata/publications.json holds the 22 scenarios captured from the pinned Node
// adapter, and testdata/reference the script that reproduces them and checks the
// encoder's output with Node's decoders; testdata/README.md describes both.
package codec

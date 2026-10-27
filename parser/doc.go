// Package parser encodes and decodes Socket.IO protocol v4 packets.
// Header describes a packet and Buffer marks binary attachments within event
// arguments. Prepare encodes reusable packets; Encoder and Decoder exchange
// text headers and binary frames through transport-independent interfaces.
package parser

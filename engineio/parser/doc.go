// Package parser provides Engine.IO protocol v3 packet and polling-payload codecs.
// FrameReader and FrameWriter separate packet encoding from transport framing.
// Packet codecs handle individual frames; payload codecs handle the binary or
// base64 text framing used by HTTP polling.
package parser

package parser

import (
	"io"

	"github.com/sshaplygin/go-socket.io/engineio/parser/internal/packet"
)

// FrameType is the byte frame type (String or Binary). Byte returns its wire value.
type FrameType = packet.FrameType

const (
	// String identifies a string frame.
	String = packet.String
	// Binary identifies a binary frame.
	Binary = packet.Binary
)

// ByteToFrameType converts a byte to FrameType.
func ByteToFrameType(b byte) FrameType { return packet.ByteToFrameType(b) }

// PacketType identifies OPEN through NOOP. String returns its name; StringByte
// and BinaryByte return its header byte in string and binary frames.
type PacketType = packet.PacketType

const (
	// OPEN is sent from the server when a new transport is opened.
	OPEN = packet.OPEN
	// CLOSE requests transport closure without shutting down the connection itself.
	CLOSE = packet.CLOSE
	// PING is sent by the client; the server answers with PONG and the same data.
	PING = packet.PING
	// PONG responds to a ping packet.
	PONG = packet.PONG
	// MESSAGE carries application data.
	MESSAGE = packet.MESSAGE
	// UPGRADE requests a switch to the probed transport.
	UPGRADE = packet.UPGRADE
	// NOOP forces a poll cycle when a websocket connection arrives.
	NOOP = packet.NOOP
)

// ByteToPacketType converts a byte to PacketType.
func ByteToPacketType(b byte, typ FrameType) PacketType { return packet.ByteToPacketType(b, typ) }

// Frame holds a frame type in its field FType and the frame body in Data.
type Frame = packet.Frame

// Packet holds a frame type in its field FType, a packet type in PType and the
// packet body in Data.
type Packet = packet.Packet

// FrameWriter is the writer which supports framing.
type FrameWriter interface {
	// NextWriter returns a writer for one frame of the given type; the frame
	// is complete when the writer is closed.
	NextWriter(FrameType) (io.WriteCloser, error)
}

// FrameReader is the reader which supports framing.
type FrameReader interface {
	// NextReader returns the type and the contents of the next frame; the
	// reader is closed before the next frame is requested.
	NextReader() (FrameType, io.ReadCloser, error)
}

// The public frame interfaces keep the method sets of the codec contracts.
var (
	_ FrameWriter        = packet.FrameWriter(nil)
	_ packet.FrameWriter = FrameWriter(nil)
	_ FrameReader        = packet.FrameReader(nil)
	_ packet.FrameReader = FrameReader(nil)
)

// PacketEncoder writes Engine.IO packet headers to framed destinations.
// (*PacketEncoder).NextWriter opens a writer for one packet of the given frame
// and packet type.
type PacketEncoder = packet.Encoder

// PacketDecoder reads Engine.IO packet headers from framed sources.
// (*PacketDecoder).NextReader returns the frame type, packet type and a reader
// for the next packet.
type PacketDecoder = packet.Decoder

// NewPacketEncoder returns a PacketEncoder that writes each packet as one frame
// of w: the packet type, as a digit in a String frame or as a byte in a Binary
// frame, followed by the body the caller writes. A PacketEncoder is not safe for
// concurrent use.
func NewPacketEncoder(w FrameWriter) *PacketEncoder { return packet.NewEncoder(w) }

// NewPacketDecoder returns a PacketDecoder that reads each frame of r as one
// packet: the first byte gives the packet type, and the returned reader yields
// the rest of the frame. The caller must close that reader before the next
// call. A PacketDecoder is not safe for concurrent use.
func NewPacketDecoder(r FrameReader) *PacketDecoder { return packet.NewDecoder(r) }

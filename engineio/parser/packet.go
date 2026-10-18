package parser

import "github.com/sshaplygin/go-socket.io/engineio/parser/internal/packet"

// FrameType is the type of frames.
type FrameType = packet.FrameType

const (
	// String identifies a string frame.
	String = packet.String
	// Binary identifies a binary frame.
	Binary = packet.Binary
)

// ByteToFrameType converts a byte to FrameType.
func ByteToFrameType(b byte) FrameType { return packet.ByteToFrameType(b) }

// PacketType is the type of packet.
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

// Frame contains a frame type and its data.
type Frame = packet.Frame

// Packet contains a frame type, packet type and data.
type Packet = packet.Packet

// FrameWriter is the writer which supports framing.
type FrameWriter = packet.FrameWriter

// FrameReader is the reader which supports framing.
type FrameReader = packet.FrameReader

// PacketEncoder writes Engine.IO packet headers to framed destinations.
type PacketEncoder = packet.Encoder

// PacketDecoder reads Engine.IO packet headers from framed sources.
type PacketDecoder = packet.Decoder

func NewPacketEncoder(w FrameWriter) *PacketEncoder { return packet.NewEncoder(w) }

func NewPacketDecoder(r FrameReader) *PacketDecoder { return packet.NewDecoder(r) }

package parser

import (
	"io"

	"github.com/sshaplygin/go-socket.io/engineio/parser/internal/packet"
)

// FrameType is the byte frame type (String or Binary). Byte() byte returns its wire value.
type FrameType = packet.FrameType

const (
	// String identifies a string frame.
	String = packet.String
	// Binary identifies a binary frame.
	Binary = packet.Binary
)

// ByteToFrameType converts a byte to FrameType.
func ByteToFrameType(b byte) FrameType { return packet.ByteToFrameType(b) }

// PacketType identifies OPEN through NOOP. String() string returns its name;
// StringByte() byte and BinaryByte() byte return the respective wire headers.
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

// Frame contains fields FType FrameType and Data []byte.
type Frame = packet.Frame

// Packet contains fields FType FrameType, PType PacketType and Data []byte.
type Packet = packet.Packet

// FrameWriter is the writer which supports framing.
type FrameWriter interface {
	NextWriter(FrameType) (io.WriteCloser, error)
}

// FrameReader is the reader which supports framing.
type FrameReader interface {
	NextReader() (FrameType, io.ReadCloser, error)
}

// The public frame interfaces keep the method sets of the codec contracts.
var (
	_ FrameWriter        = packet.FrameWriter(nil)
	_ packet.FrameWriter = FrameWriter(nil)
	_ FrameReader        = packet.FrameReader(nil)
	_ packet.FrameReader = FrameReader(nil)
	_ FrameWriter        = (*FakeDiscardWriter)(nil)
)

// PacketEncoder writes Engine.IO packet headers to framed destinations.
// NextWriter(FrameType, PacketType) (io.WriteCloser, error) opens a packet writer.
type PacketEncoder = packet.Encoder

// PacketDecoder reads Engine.IO packet headers from framed sources.
// NextReader() (FrameType, PacketType, io.ReadCloser, error) opens a packet reader.
type PacketDecoder = packet.Decoder

func NewPacketEncoder(w FrameWriter) *PacketEncoder { return packet.NewEncoder(w) }

func NewPacketDecoder(r FrameReader) *PacketDecoder { return packet.NewDecoder(r) }

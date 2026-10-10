// Package websocket is the Engine.IO WebSocket transport, built on gobwas/ws.
//
// The codec (Encode, Decode) handles the contents of one WebSocket data message
// as an Engine.IO v4 packet; message.go handles RFC 6455 headers, masking,
// fragmentation, control frames and bounded reads. Engine.IO PING/PONG packets
// are distinct from WebSocket ping/pong control frames.
//
// permessage-deflate is not negotiated: the handshake never offers or accepts
// an extension.
package websocket

import (
	"bytes"
	"encoding/base64"
	"errors"
	"unicode/utf8"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

var (
	// ErrInvalidPacket indicates malformed data or an unrepresentable packet.
	ErrInvalidPacket = errors.New("websocket: invalid packet")
	// ErrTooLarge indicates message contents exceed the caller's byte limit.
	ErrTooLarge = errors.New("websocket: packet too large")
	// ErrInvalidLimit indicates a nonpositive message size limit.
	ErrInvalidLimit = errors.New("websocket: message size limit must be positive")
)

// Packet contains data without an Engine.IO type prefix. Binary packets must
// have type MESSAGE; the v4 binary encoding cannot represent any other type.
type Packet struct {
	Frame frame.Type
	Type  packet.Type
	Data  []byte
}

// Encode returns the WebSocket data message type and contents for p. When
// supportsBinary is false, binary data uses a text message with a 'b' prefix and
// canonical padded base64, as in the JS parser. Otherwise binary data is sent
// unchanged, including empty data. Text data gets a type byte and may contain
// the polling record separator: WebSocket messages already provide boundaries.
// maxBytes must be positive and limits encoded contents, excluding WebSocket
// headers. The result owns its bytes; errors never return partial contents.
func Encode(p Packet, supportsBinary bool, maxBytes int) (frame.Type, []byte, error) {
	if maxBytes <= 0 {
		return 0, nil, ErrInvalidLimit
	}
	if len(p.Data) > maxBytes {
		return 0, nil, ErrTooLarge
	}
	if p.Type < packet.OPEN || p.Type > packet.NOOP {
		return 0, nil, ErrInvalidPacket
	}
	switch p.Frame {
	case frame.Binary:
		if p.Type != packet.MESSAGE {
			return 0, nil, ErrInvalidPacket
		}
		if supportsBinary {
			return frame.Binary, bytes.Clone(p.Data), nil
		}
		remaining := maxBytes - 1 // Binary prefix.
		if len(p.Data) > (remaining/4)*3 {
			return 0, nil, ErrTooLarge
		}
		out := make([]byte, 1, 1+base64.StdEncoding.EncodedLen(len(p.Data)))
		out[0] = 'b'
		return frame.String, base64.StdEncoding.AppendEncode(out, p.Data), nil
	case frame.String:
		if len(p.Data) == maxBytes {
			return 0, nil, ErrTooLarge
		}
		if !utf8.Valid(p.Data) {
			return 0, nil, ErrInvalidPacket
		}
		out := make([]byte, 1, 1+len(p.Data))
		out[0] = p.Type.StringByte()
		return frame.String, append(out, p.Data...), nil
	default:
		return 0, nil, ErrInvalidPacket
	}
}

// Decode decodes one complete WebSocket data message. Raw binary messages are
// MESSAGE packets with no Engine.IO type byte. Text messages beginning with 'b'
// decode to binary MESSAGE packets, matching the JS parser's base64 fallback.
// Text otherwise requires a type byte in '0'..'6' and valid UTF-8 data.
// maxBytes must be positive and counts encoded contents, not WebSocket headers.
// Callers must also limit reads before buffering; this function cannot bound
// their allocations. Results own their data; errors return the zero Packet.
// As in the staged polling codec, base64 must be canonical and contain no whitespace.
func Decode(messageType frame.Type, body []byte, maxBytes int) (Packet, error) {
	if maxBytes <= 0 {
		return Packet{}, ErrInvalidLimit
	}
	if len(body) > maxBytes {
		return Packet{}, ErrTooLarge
	}
	switch messageType {
	case frame.Binary:
		return Packet{Frame: frame.Binary, Type: packet.MESSAGE, Data: bytes.Clone(body)}, nil
	case frame.String:
		if len(body) == 0 {
			return Packet{}, ErrInvalidPacket
		}
		if body[0] == 'b' {
			data := body[1:]
			if bytes.ContainsAny(data, "\r\n") {
				return Packet{}, ErrInvalidPacket
			}
			out := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
			n, err := base64.StdEncoding.Strict().Decode(out, data)
			if err != nil {
				return Packet{}, ErrInvalidPacket
			}
			return Packet{Frame: frame.Binary, Type: packet.MESSAGE, Data: out[:n]}, nil
		}
		if body[0] < '0' || body[0] > '6' || !utf8.Valid(body[1:]) {
			return Packet{}, ErrInvalidPacket
		}
		return Packet{Frame: frame.String, Type: packet.Type(body[0] - '0'), Data: bytes.Clone(body[1:])}, nil
	default:
		return Packet{}, ErrInvalidPacket
	}
}

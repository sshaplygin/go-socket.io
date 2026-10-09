// Package eio4 encodes complete Engine.IO v4 HTTP polling payloads.
// It is staged for the v2 transport rewrite; the v1 transport does not use it.
//
// Callers choose HTTP body limits and own queues, deadlines, pause and resume.
// This codec has no session state or goroutines. It does not encode WebSocket frames.
// The polling rewrite should use it through package payload, as Go's internal
// visibility rules prevent direct use by the sibling transport/polling package.
package eio4

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

const separator = byte(0x1e)

var (
	// ErrInvalidPayload indicates malformed wire data or an unencodable packet.
	ErrInvalidPayload = errors.New("eio4: invalid polling payload")
	// ErrTooLarge indicates that the encoded body exceeds the caller's limit.
	ErrTooLarge = errors.New("eio4: polling payload too large")
	// ErrInvalidLimit indicates a nonpositive wire size limit.
	ErrInvalidLimit = errors.New("eio4: wire size limit must be positive")
)

// Packet contains one Engine.IO packet, with no type byte in Data.
// Binary frames are always MESSAGE packets. Text data must be valid UTF-8 and
// cannot contain the polling record separator (which has no escape sequence).
type Packet struct {
	Frame frame.Type
	Type  packet.Type
	Data  []byte
}

// Encode returns a complete polling body, including separators and base64
// expansion, limited to maxBytes wire bytes. maxBytes must be positive.
// An empty batch is invalid: the transport should wait or explicitly send NOOP.
// The result owns its bytes. On any error, no partial body is returned.
func Encode(packets []Packet, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, ErrInvalidLimit
	}
	if len(packets) == 0 {
		return nil, ErrInvalidPayload
	}
	_, size, err := measure(packets, maxBytes)
	if err != nil {
		return nil, err
	}
	return encodePackets(packets, size), nil
}

// measure returns the number and wire size of complete packets before the first
// error. size never includes bytes from a packet that did not fit.
func measure(packets []Packet, maxBytes int) (count, size int, err error) {
	for i, p := range packets {
		// Raw data is a lower bound on wire size for either frame type. Reject
		// oversized input before scanning text for UTF-8 and separators.
		if len(p.Data) > maxBytes-size {
			return i, size, ErrTooLarge
		}
		if err := validate(p); err != nil {
			return i, size, fmt.Errorf("packet %d: %w", i, err)
		}
		nextSize := size
		if i > 0 {
			if nextSize == maxBytes {
				return i, size, ErrTooLarge
			}
			nextSize++
		}
		if nextSize == maxBytes {
			return i, size, ErrTooLarge
		}
		nextSize++ // Packet type or binary prefix.
		remaining := maxBytes - nextSize
		n := len(p.Data)
		if p.Frame == frame.Binary {
			// Check before EncodedLen so its integer arithmetic cannot overflow.
			if n > (remaining/4)*3 {
				return i, size, ErrTooLarge
			}
			n = base64.StdEncoding.EncodedLen(n)
		}
		if n > remaining {
			return i, size, ErrTooLarge
		}
		size = nextSize + n
	}
	return len(packets), size, nil
}

func encodePackets(packets []Packet, size int) []byte {
	out := make([]byte, 0, size)
	for i, p := range packets {
		if i > 0 {
			out = append(out, separator)
		}
		if p.Frame == frame.Binary {
			out = append(out, 'b')
			out = base64.StdEncoding.AppendEncode(out, p.Data)
		} else {
			out = append(out, p.Type.StringByte())
			out = append(out, p.Data...)
		}
	}
	return out
}

// Decode decodes one complete polling body of at most maxBytes wire bytes.
// maxBytes must be positive. Callers must also bound reads before buffering the
// HTTP body; this limit cannot prevent allocations made by the caller.
// DecodeReader applies the wire limit while reading an unbuffered body.
// It is not a heap limit: many tiny records allocate a much larger Packet slice.
// Like the JS polling decoder, Decode collects the complete batch in memory
// without an additional packet-count limit.
// Returned packet data owns its bytes and may be changed independently of body.
// On any error, no partial batch is returned. Empty records are invalid.
// Base64 must use the canonical padded standard alphabet, without whitespace.
func Decode(body []byte, maxBytes int) ([]Packet, error) {
	if maxBytes <= 0 {
		return nil, ErrInvalidLimit
	}
	if len(body) > maxBytes {
		return nil, ErrTooLarge
	}
	if len(body) == 0 {
		return nil, ErrInvalidPayload
	}

	// TODO: Reduce allocations for dense payloads, potentially by consuming packets
	// incrementally. Preserve byte-limit acceptance without adding a packet-count
	// cap; use BenchmarkDecode/dense-records to evaluate a future implementation.
	var packets []Packet
	for {
		record, rest, found := bytes.Cut(body, []byte{separator})
		p, err := decodePacket(record)
		if err != nil {
			return nil, fmt.Errorf("packet %d: %w", len(packets), err)
		}
		packets = append(packets, p)
		if !found {
			return packets, nil
		}
		body = rest
	}
}

func validate(p Packet) error {
	if p.Type < packet.OPEN || p.Type > packet.NOOP {
		return ErrInvalidPayload
	}
	switch p.Frame {
	case frame.String:
		if !utf8.Valid(p.Data) || bytes.IndexByte(p.Data, separator) >= 0 {
			return ErrInvalidPayload
		}
	case frame.Binary:
		if p.Type != packet.MESSAGE {
			return ErrInvalidPayload
		}
	default:
		return ErrInvalidPayload
	}
	return nil
}

func decodePacket(record []byte) (Packet, error) {
	if len(record) == 0 {
		return Packet{}, ErrInvalidPayload
	}
	if record[0] == 'b' {
		data := record[1:]
		// Go's base64 decoder ignores CR/LF even in Strict mode.
		if bytes.ContainsAny(data, "\r\n") {
			return Packet{}, ErrInvalidPayload
		}
		decoded := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
		n, err := base64.StdEncoding.Strict().Decode(decoded, data)
		if err != nil {
			return Packet{}, fmt.Errorf("%w: invalid base64", ErrInvalidPayload)
		}
		return Packet{Frame: frame.Binary, Type: packet.MESSAGE, Data: decoded[:n]}, nil
	}
	if record[0] < '0' || record[0] > '6' || !utf8.Valid(record[1:]) {
		return Packet{}, ErrInvalidPayload
	}
	return Packet{
		Frame: frame.String,
		Type:  packet.Type(record[0] - '0'),
		Data:  bytes.Clone(record[1:]),
	}, nil
}

// Package wire experiments with bounded Socket.IO protocol 5 message groups.
// It is not the public parser API and does not assemble transport streams.
package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Type uint8

const (
	Connect Type = iota
	Disconnect
	Event
	Ack
	ConnectError
	BinaryEvent
	BinaryAck
)
const MaxID uint64 = 1<<53 - 1

var (
	ErrInvalid            = errors.New("invalid Socket.IO packet")
	ErrLimit              = errors.New("invalid limits")
	ErrTooLarge           = errors.New("message exceeds byte limit")
	ErrAttachments        = errors.New("invalid attachment count or placeholder")
	ErrTooManyAttachments = errors.New("too many attachments")
	ErrDepth              = errors.New("JSON nesting exceeds limit")
)

// Limits applies to an entire envelope plus attachment bytes. Zero fields select
// bounded defaults; negative values are invalid. MaxDepth counts JSON containers.
type Limits struct{ MaxBytes, MaxAttachments, MaxDepth int }

func (l Limits) normalized() (Limits, error) {
	if l.MaxBytes < 0 || l.MaxAttachments < 0 || l.MaxDepth < 0 {
		return l, ErrLimit
	}
	if l.MaxBytes == 0 {
		l.MaxBytes = 1 << 20
	}
	if l.MaxAttachments == 0 {
		l.MaxAttachments = 64
	}
	if l.MaxDepth == 0 {
		l.MaxDepth = 64
	}
	return l, nil
}

// Packet retains JSON without decoding it into application types. Namespace ""
// means "/". ID is optional and bounded by JavaScript's largest safe integer.
// Attachments is nonzero only for BinaryEvent/BinaryAck. No fields alias Decode input.
type Packet struct {
	Type        Type            `json:"type"`
	Namespace   string          `json:"namespace"`
	ID          *uint64         `json:"id,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
	Attachments int             `json:"attachments,omitempty"`
}

func (p Packet) binary() bool { return p.Type == BinaryEvent || p.Type == BinaryAck }

// Encode validates and emits one complete text envelope. It preserves raw JSON
// formatting. Use EncodeGroup when binary bytes must also be validated/copied.
func Encode(p Packet, limits Limits) ([]byte, error) {
	l, err := limits.normalized()
	if err != nil {
		return nil, err
	}
	if len(p.Data) > l.MaxBytes || len(p.Namespace) > l.MaxBytes {
		return nil, ErrTooLarge
	}
	if err = validate(p, l); err != nil {
		return nil, err
	}
	header := strconv.Itoa(int(p.Type))
	if p.binary() {
		header += strconv.Itoa(p.Attachments) + "-"
	}
	if p.Namespace != "" && p.Namespace != "/" {
		header += p.Namespace + ","
	}
	if p.ID != nil {
		header += strconv.FormatUint(*p.ID, 10)
	}
	if len(header) > l.MaxBytes-len(p.Data) {
		return nil, ErrTooLarge
	}
	out := make([]byte, 0, len(header)+len(p.Data))
	out = append(out, header...)
	out = append(out, p.Data...)
	return out, nil
}

// Decode reads one complete text envelope. It returns no partial Packet on error.
func Decode(body []byte, limits Limits) (Packet, error) {
	l, err := limits.normalized()
	if err != nil {
		return Packet{}, err
	}
	if len(body) > l.MaxBytes {
		return Packet{}, ErrTooLarge
	}
	if len(body) == 0 || body[0] < '0' || body[0] > '6' || !utf8.Valid(body) {
		return Packet{}, ErrInvalid
	}
	p := Packet{Type: Type(body[0] - '0'), Namespace: "/"}
	pos := 1
	if p.binary() {
		end := bytes.IndexByte(body[pos:], '-')
		if end < 0 {
			return Packet{}, ErrAttachments
		}
		end += pos
		n, e := decimal(body[pos:end])
		if e != nil || n == 0 {
			return Packet{}, ErrAttachments
		}
		if n > uint64(l.MaxAttachments) {
			return Packet{}, ErrTooManyAttachments
		}
		p.Attachments = int(n)
		pos = end + 1
	}
	if pos < len(body) && body[pos] == '/' {
		end := bytes.IndexByte(body[pos:], ',')
		if end < 0 {
			return Packet{}, ErrInvalid
		}
		end += pos
		p.Namespace = string(body[pos:end])
		pos = end + 1
	}
	start := pos
	for pos < len(body) && body[pos] >= '0' && body[pos] <= '9' {
		pos++
	}
	if pos > start {
		n, e := decimal(body[start:pos])
		if e != nil || n > MaxID {
			return Packet{}, ErrInvalid
		}
		p.ID = &n
	}
	if pos < len(body) {
		p.Data = body[pos:]
	}
	if err = validate(p, l); err != nil {
		return Packet{}, err
	}
	p.Data = bytes.Clone(p.Data)
	return p, nil
}
func decimal(b []byte) (uint64, error) {
	if len(b) == 0 {
		return 0, ErrInvalid
	}
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, ErrInvalid
		}
	}
	n, e := strconv.ParseUint(string(b), 10, 64)
	if e != nil {
		return 0, ErrInvalid
	}
	return n, nil
}
func validate(p Packet, l Limits) error {
	if p.Type > BinaryAck || !utf8.ValidString(p.Namespace) || (p.Namespace != "" && (p.Namespace[0] != '/' || strings.ContainsAny(p.Namespace, ",\x00\r\n"))) || (p.ID != nil && *p.ID > MaxID) {
		return ErrInvalid
	}
	if p.binary() {
		if p.Attachments < 1 {
			return ErrAttachments
		}
		if p.Attachments > l.MaxAttachments {
			return ErrTooManyAttachments
		}
	} else if p.Attachments != 0 {
		return ErrAttachments
	}
	if len(p.Data) == 0 {
		if p.Type == Connect || p.Type == Disconnect {
			return nil
		}
		return ErrInvalid
	}
	if p.Type == Disconnect || !utf8.Valid(p.Data) || !json.Valid(p.Data) {
		return ErrInvalid
	}
	// Bound recursion before decoding a tree or visiting binary placeholders.
	dec := json.NewDecoder(bytes.NewReader(p.Data))
	dec.UseNumber()
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ErrInvalid
		}
		if d, ok := tok.(json.Delim); ok {
			if d == '[' || d == '{' {
				depth++
				if depth > l.MaxDepth {
					return ErrDepth
				}
			} else {
				depth--
			}
		}
	}
	raw := bytes.TrimSpace(p.Data)
	switch p.Type {
	case Connect:
		if raw[0] != '{' {
			return ErrInvalid
		}
	case ConnectError:
		if raw[0] != '{' && raw[0] != '"' {
			return ErrInvalid
		}
	case Event, BinaryEvent, Ack, BinaryAck:
		if raw[0] != '[' {
			return ErrInvalid
		}
		var args []json.RawMessage
		if json.Unmarshal(raw, &args) != nil {
			return ErrInvalid
		}
		if p.Type == Event || p.Type == BinaryEvent {
			if len(args) == 0 {
				return ErrInvalid
			}
			var name any
			d := json.NewDecoder(bytes.NewReader(args[0]))
			d.UseNumber()
			if d.Decode(&name) != nil {
				return ErrInvalid
			}
			switch v := name.(type) {
			case json.Number:
			case string:
				switch v {
				case "connect", "connect_error", "disconnect", "disconnecting", "newListener", "removeListener":
					return ErrInvalid
				}
			default:
				return ErrInvalid
			}
		}
	}
	if p.binary() {
		tree, err := tree(p.Data)
		if err != nil {
			return err
		}
		_, err = walk(tree, p.Attachments, nil)
		return err
	}
	return nil
}
func tree(data []byte) (any, error) {
	var v any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return v, nil
}

// walk validates placeholders, optionally replacing them with owned attachment
// bytes. Repeated references intentionally share one owned attachment, as in JS.
func walk(v any, count int, attachments [][]byte) (any, error) {
	switch x := v.(type) {
	case []any:
		for i, item := range x {
			next, err := walk(item, count, attachments)
			if err != nil {
				return nil, err
			}
			x[i] = next
		}
	case map[string]any:
		if x["_placeholder"] == true {
			n, ok := x["num"].(json.Number)
			if !ok {
				return nil, ErrAttachments
			}
			f, err := strconv.ParseFloat(string(n), 64)
			if err != nil || f < 0 || f >= float64(count) || f != float64(int(f)) {
				return nil, ErrAttachments
			}
			if attachments != nil {
				return attachments[int(f)], nil
			}
			return v, nil
		}
		for k, item := range x {
			next, err := walk(item, count, attachments)
			if err != nil {
				return nil, err
			}
			x[k] = next
		}
	}
	return v, nil
}

// Group is one complete message: its text packet and ordered binary attachments.
// There is no stream state, timeout, or transport framing in this experiment.
type Group struct {
	Packet      Packet
	Attachments [][]byte
}

// DecodeGroup validates exact attachment count and aggregate bytes, returning
// owned JSON and binary buffers. No partially validated group escapes on error.
func DecodeGroup(envelope []byte, attachments [][]byte, limits Limits) (Group, error) {
	l, err := limits.normalized()
	if err != nil {
		return Group{}, err
	}
	if len(attachments) > l.MaxAttachments {
		return Group{}, ErrTooManyAttachments
	}
	remaining := l.MaxBytes
	if len(envelope) > remaining {
		return Group{}, ErrTooLarge
	}
	remaining -= len(envelope)
	for _, b := range attachments {
		if len(b) > remaining {
			return Group{}, ErrTooLarge
		}
		remaining -= len(b)
	}
	p, err := Decode(envelope, l)
	if err != nil {
		return Group{}, err
	}
	if len(attachments) != p.Attachments {
		return Group{}, ErrAttachments
	}
	out := make([][]byte, len(attachments))
	for i, b := range attachments {
		out[i] = bytes.Clone(b)
	}
	return Group{p, out}, nil
}

// EncodeGroup emits independently owned wire buffers from an explicit placeholder
// packet. It does not discover binary values in typed Go structs or interfaces.
func EncodeGroup(g Group, limits Limits) ([]byte, [][]byte, error) {
	body, err := Encode(g.Packet, limits)
	if err != nil {
		return nil, nil, err
	}
	owned, err := DecodeGroup(body, g.Attachments, limits)
	if err != nil {
		return nil, nil, err
	}
	return body, owned.Attachments, nil
}

// Reconstruct returns a JSON tree with placeholders replaced by owned []byte
// values. Other numbers remain json.Number. Ordinary packets are supported too.
// Typed decoding, Args2 arity, and binary traversal are future integration work.
func Reconstruct(g Group, limits Limits) (any, error) {
	body, attachments, err := EncodeGroup(g, limits)
	if err != nil {
		return nil, err
	}
	p, err := Decode(body, limits)
	if err != nil {
		return nil, err
	}
	if len(p.Data) == 0 {
		return nil, nil
	}
	v, err := tree(p.Data)
	if err != nil {
		return nil, err
	}
	if !p.binary() {
		return v, nil
	}
	return walk(v, p.Attachments, attachments)
}

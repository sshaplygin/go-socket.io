package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Encode returns the message of p: the text envelope and one binary frame per
// attachment, in order. The envelope keeps the JSON spelling of p.Data. Encode copies
// every attachment, so the result is independent of p, and it never modifies p: a
// Packet that several connections share may be encoded concurrently.
//
// Namespace "" and "/" both select the default namespace, which the envelope omits.
// A Packet with attachments is sent as BINARY_EVENT (Event) or BINARY_ACK (Ack); any
// other type with attachments is ErrAttachments. The packet is validated as Decode
// validates a received one, including that every placeholder in Data names one of
// its attachments.
func Encode(p Packet, limits Limits) (text []byte, attachments [][]byte, err error) {
	l, err := limits.normalized()
	if err != nil {
		return nil, nil, err
	}
	text, err = encodeEnvelope(p, len(p.Attachments), l)
	if err != nil {
		return nil, nil, err
	}
	remaining := l.MaxEventBytes - len(text)
	for _, b := range p.Attachments {
		if len(b) > remaining {
			return nil, nil, ErrTooLarge
		}
		remaining -= len(b)
	}
	return text, cloneAttachments(p.Attachments), nil
}

// Decode reads one complete message: the text envelope and the binary frames that
// follow it. The number of attachments must equal the number the envelope announces.
// Nothing in the result aliases text or attachments, and a failed call returns the
// zero Packet. The default namespace is returned as "/".
//
// Unreferenced attachments and repeated placeholder indices are accepted, as by the
// Node.js parser; the count and byte limits still apply to them.
func Decode(text []byte, attachments [][]byte, limits Limits) (Packet, error) {
	l, err := limits.normalized()
	if err != nil {
		return Packet{}, err
	}
	if len(attachments) > l.MaxAttachments {
		return Packet{}, ErrTooManyAttachments
	}
	remaining := l.MaxEventBytes
	if len(text) > remaining {
		return Packet{}, ErrTooLarge
	}
	remaining -= len(text)
	for _, b := range attachments {
		if len(b) > remaining {
			return Packet{}, ErrTooLarge
		}
		remaining -= len(b)
	}
	p, n, err := parseEnvelope(text, l)
	if err != nil {
		return Packet{}, err
	}
	if len(attachments) != n {
		return Packet{}, ErrAttachments
	}
	p.Attachments = cloneAttachments(attachments)
	return p, nil
}

func cloneAttachments(in [][]byte) [][]byte {
	if len(in) == 0 {
		return nil
	}
	out := make([][]byte, len(in))
	for i, b := range in {
		out[i] = bytes.Clone(b)
	}
	return out
}

// encodeEnvelope validates a packet that announces n attachments and returns its text
// envelope. l is normalized.
func encodeEnvelope(p Packet, n int, l Limits) ([]byte, error) {
	if len(p.Data) > l.MaxEventBytes || len(p.Namespace) > l.MaxEventBytes {
		return nil, ErrTooLarge
	}
	if err := check(p.Type, p.Namespace, p.ID, p.Data, n, l); err != nil {
		return nil, err
	}
	code := byte(p.Type)
	if n > 0 {
		code += 3 // Event 2 -> 5, Ack 3 -> 6
	}
	header := make([]byte, 0, 32+len(p.Namespace))
	header = append(header, '0'+code)
	if n > 0 {
		header = strconv.AppendInt(header, int64(n), 10)
		header = append(header, '-')
	}
	if p.Namespace != "" && p.Namespace != "/" {
		header = append(header, p.Namespace...)
		header = append(header, ',')
	}
	if p.ID != nil {
		header = strconv.AppendUint(header, *p.ID, 10)
	}
	if len(header) > l.MaxEventBytes-len(p.Data) {
		return nil, ErrTooLarge
	}
	return append(header, p.Data...), nil
}

// parseEnvelope reads one text envelope and returns the packet, without
// attachments, and the number of attachments the header announces. Data is a copy.
// l is normalized.
func parseEnvelope(body []byte, l Limits) (Packet, int, error) {
	if len(body) > l.MaxEventBytes {
		return Packet{}, 0, ErrTooLarge
	}
	if len(body) == 0 || body[0] < '0' || body[0] > '6' || !utf8.Valid(body) {
		return Packet{}, 0, ErrInvalid
	}
	p := Packet{Type: Type(body[0] - '0'), Namespace: "/"}
	pos, n := 1, 0
	if body[0] >= '5' {
		p.Type -= 3 // BINARY_EVENT 5 -> Event, BINARY_ACK 6 -> Ack
		end := bytes.IndexByte(body[pos:], '-')
		if end < 0 {
			return Packet{}, 0, ErrAttachments
		}
		end += pos
		count, err := decimal(body[pos:end])
		if err != nil || count == 0 {
			return Packet{}, 0, ErrAttachments
		}
		if count > uint64(l.MaxAttachments) {
			return Packet{}, 0, ErrTooManyAttachments
		}
		n = int(count)
		pos = end + 1
	}
	if pos < len(body) && body[pos] == '/' {
		end := bytes.IndexByte(body[pos:], ',')
		if end < 0 {
			return Packet{}, 0, ErrInvalid
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
		id, err := decimal(body[start:pos])
		if err != nil || id > MaxID {
			return Packet{}, 0, ErrInvalid
		}
		p.ID = &id
	}
	if pos < len(body) {
		p.Data = body[pos:]
	}
	if err := check(p.Type, p.Namespace, p.ID, p.Data, n, l); err != nil {
		return Packet{}, 0, err
	}
	p.Data = bytes.Clone(p.Data)
	return p, n, nil
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
	n, err := strconv.ParseUint(string(b), 10, 64)
	if err != nil {
		return 0, ErrInvalid
	}
	return n, nil
}

// reservedEvents are the names Socket.IO reserves; an EVENT must not carry one.
var reservedEvents = map[string]bool{
	"connect": true, "connect_error": true, "disconnect": true,
	"disconnecting": true, "newListener": true, "removeListener": true,
}

// check validates the parts of a packet that announces n attachments. It reads data
// and does not retain it. l is normalized.
func check(t Type, ns string, id *uint64, data []byte, n int, l Limits) error {
	if t > ConnectError || !utf8.ValidString(ns) ||
		(ns != "" && (ns[0] != '/' || strings.ContainsAny(ns, ",\x00\r\n"))) ||
		(id != nil && *id > MaxID) {
		return ErrInvalid
	}
	if n > 0 {
		if t != Event && t != Ack {
			return ErrAttachments
		}
		if n > l.MaxAttachments {
			return ErrTooManyAttachments
		}
	}
	if len(data) == 0 {
		if t == Connect || t == Disconnect {
			return nil
		}
		return ErrInvalid
	}
	if t == Disconnect || !utf8.Valid(data) {
		return ErrInvalid
	}
	if err := validJSON(data); err != nil {
		return err
	}
	if err := checkDepth(data, l.MaxDepth); err != nil {
		return err
	}
	raw := bytes.TrimSpace(data)
	switch t {
	case Connect:
		if raw[0] != '{' {
			return ErrInvalid
		}
	case ConnectError:
		if raw[0] != '{' && raw[0] != '"' {
			return ErrInvalid
		}
	case Event, Ack:
		if raw[0] != '[' {
			return ErrInvalid
		}
		if t == Event {
			if err := checkEventName(raw); err != nil {
				return err
			}
		}
	}
	if n > 0 {
		_, err := walkPlaceholders(raw, n, nil)
		return err
	}
	return nil
}

// maxStdlibNesting is the nesting depth past which encoding/json refuses a document.
const maxStdlibNesting = 10000

// validJSON reports ErrInvalid for text that is not one JSON value. Text that nests
// arrays or objects deeper than encoding/json accepts (10000 levels) is ErrDepth, so
// that a limit breach is not reported as a malformed packet whatever Limits.MaxDepth
// is; such text is never valid for the parser, whose depth limit is far lower.
func validJSON(data []byte) error {
	if json.Valid(data) {
		return nil
	}
	depth, inString, escaped := 0, false, false
	for _, c := range data {
		switch {
		case escaped:
			escaped = false
		case inString:
			escaped = c == '\\'
			inString = c != '"'
		case c == '"':
			inString = true
		case c == '[' || c == '{':
			if depth++; depth > maxStdlibNesting {
				return ErrDepth
			}
		case c == ']' || c == '}':
			depth--
		}
	}
	return ErrInvalid
}

// checkDepth rejects valid JSON that nests arrays or objects deeper than max. It runs
// before any tree is built, so recursion over the tree is bounded.
func checkDepth(data []byte, max int) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return ErrInvalid
		}
		if d, ok := tok.(json.Delim); ok {
			if d == '[' || d == '{' {
				depth++
				if depth > max {
					return ErrDepth
				}
			} else {
				depth--
			}
		}
	}
}

// checkEventName requires the array to start with a string that is not a reserved
// name, or with a number, as the Node.js parser accepts.
func checkEventName(array []byte) error {
	dec := json.NewDecoder(bytes.NewReader(array))
	dec.UseNumber()
	if _, err := dec.Token(); err != nil || !dec.More() {
		return ErrInvalid
	}
	var name any
	if dec.Decode(&name) != nil {
		return ErrInvalid
	}
	switch v := name.(type) {
	case json.Number:
		return nil
	case string:
		if reservedEvents[v] {
			return ErrInvalid
		}
		return nil
	}
	return ErrInvalid
}

package codec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/vmihailenco/msgpack/v5"
)

// jsonNode is a parsed JSON value that keeps the order of object keys, so the
// MessagePack output follows the order of the input as a JavaScript encoder follows
// insertion order.
type jsonNode struct {
	kind byte // 'n' null, 'b' bool, '#' number, 's' string, '[' array, '{' object
	b    bool
	s    string // string value or number text
	keys []string
	kids []*jsonNode
}

// parseJSON parses exactly one JSON value from data and nothing after it.
func parseJSON(data []byte, maxDepth int) (*jsonNode, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	n, err := readNode(dec, 0, maxDepth)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: data after the JSON value", ErrInvalid)
	}
	return n, nil
}

func readNode(dec *json.Decoder, depth, maxDepth int) (*jsonNode, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%w: JSON nesting deeper than %d", ErrLimit, maxDepth)
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: JSON: %v", ErrInvalid, err)
	}
	switch v := tok.(type) {
	case nil:
		return &jsonNode{kind: 'n'}, nil
	case bool:
		return &jsonNode{kind: 'b', b: v}, nil
	case json.Number:
		return &jsonNode{kind: '#', s: string(v)}, nil
	case string:
		return &jsonNode{kind: 's', s: v}, nil
	case json.Delim:
		n := &jsonNode{kind: byte(v)}
		for dec.More() {
			if v == '{' {
				kt, err := dec.Token()
				if err != nil {
					return nil, fmt.Errorf("%w: JSON: %v", ErrInvalid, err)
				}
				key, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("%w: JSON object key %v", ErrInvalid, kt)
				}
				n.keys = append(n.keys, key)
			}
			kid, err := readNode(dec, depth+1, maxDepth)
			if err != nil {
				return nil, err
			}
			n.kids = append(n.kids, kid)
		}
		if _, err := dec.Token(); err != nil { // the closing delimiter
			return nil, fmt.Errorf("%w: JSON: %v", ErrInvalid, err)
		}
		return n, nil
	}
	return nil, fmt.Errorf("%w: JSON token %v", ErrInvalid, tok)
}

// placeholderNum reports whether n is exactly {"_placeholder":true,"num":N}, in
// either key order, and returns N.
func (n *jsonNode) placeholderNum() (int, bool) {
	if n.kind != '{' || len(n.keys) != 2 {
		return 0, false
	}
	var flag, num *jsonNode
	for i, k := range n.keys {
		switch k {
		case "_placeholder":
			flag = n.kids[i]
		case "num":
			num = n.kids[i]
		}
	}
	if flag == nil || num == nil || flag.kind != 'b' || !flag.b || num.kind != '#' {
		return 0, false
	}
	i, err := strconv.Atoi(num.s)
	if err != nil || i < 0 {
		return 0, false
	}
	return i, true
}

// mpWriter writes a jsonNode tree as MessagePack the way notepack.io does: the
// smallest integer encoding, integral numbers as integers, other numbers as float64,
// binary attachments in place of placeholders.
type mpWriter struct {
	enc  *msgpack.Encoder
	atts [][]byte
	used []bool
}

func (w *mpWriter) node(n *jsonNode) error {
	switch n.kind {
	case 'n':
		return w.enc.EncodeNil()
	case 'b':
		return w.enc.EncodeBool(n.b)
	case 's':
		return w.enc.EncodeString(n.s)
	case '#':
		return w.number(n.s)
	case '[':
		if err := w.enc.EncodeArrayLen(len(n.kids)); err != nil {
			return err
		}
		for _, k := range n.kids {
			if err := w.node(k); err != nil {
				return err
			}
		}
		return nil
	}
	if i, ok := n.placeholderNum(); ok {
		if i >= len(w.atts) {
			return fmt.Errorf("%w: placeholder %d, %d attachments", ErrInvalid, i, len(w.atts))
		}
		if w.used[i] {
			return fmt.Errorf("%w: placeholder %d used twice", ErrInvalid, i)
		}
		w.used[i] = true
		a := w.atts[i]
		if a == nil {
			a = []byte{} // EncodeBytes writes nil for a nil slice
		}
		return w.enc.EncodeBytes(a)
	}
	if err := w.enc.EncodeMapLen(len(n.kids)); err != nil {
		return err
	}
	for i, k := range n.kids {
		if err := w.enc.EncodeString(n.keys[i]); err != nil {
			return err
		}
		if err := w.node(k); err != nil {
			return err
		}
	}
	return nil
}

func (w *mpWriter) number(s string) error {
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return w.enc.EncodeInt(i)
	}
	if u, err := strconv.ParseUint(s, 10, 64); err == nil {
		return w.enc.EncodeUint(u)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return fmt.Errorf("%w: number %q", ErrInvalid, s)
	}
	if f == math.Trunc(f) && math.Abs(f) <= 1<<53 {
		return w.enc.EncodeInt(int64(f))
	}
	return w.enc.EncodeFloat64(f)
}

// allUsed fails when an attachment has no placeholder.
func (w *mpWriter) allUsed() error {
	for i, u := range w.used {
		if !u {
			return fmt.Errorf("%w: attachment %d has no placeholder in the data", ErrInvalid, i)
		}
	}
	return nil
}

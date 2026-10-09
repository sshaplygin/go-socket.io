package parser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
)

// placeholderKey marks the object that stands for an attachment in a payload.
const placeholderKey = "_placeholder"

// Placeholder returns the JSON object that stands for attachment index in a payload:
// {"_placeholder":true,"num":index}.
func Placeholder(index int) json.RawMessage {
	b := make([]byte, 0, 40)
	b = append(b, `{"_placeholder":true,"num":`...)
	b = strconv.AppendInt(b, int64(index), 10)
	return append(b, '}')
}

// walkPlaceholders decodes raw into a generic tree and checks that every placeholder
// in it names an integer index in [0, count). With repl non-nil, each placeholder is
// replaced in the returned tree by repl(index). The tree keeps numbers as
// json.Number and is private to the caller. raw must be valid JSON of bounded depth.
func walkPlaceholders(raw []byte, count int, repl func(index int) any) (any, error) {
	var tree any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&tree); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return walk(tree, count, repl)
}

func walk(v any, count int, repl func(index int) any) (any, error) {
	switch x := v.(type) {
	case []any:
		for i, item := range x {
			next, err := walk(item, count, repl)
			if err != nil {
				return nil, err
			}
			x[i] = next
		}
	case map[string]any:
		if x[placeholderKey] == true {
			n, ok := x["num"].(json.Number)
			if !ok {
				return nil, ErrAttachments
			}
			f, err := strconv.ParseFloat(string(n), 64)
			if err != nil || f < 0 || f >= float64(count) || f != float64(int(f)) {
				return nil, ErrAttachments
			}
			if repl != nil {
				return repl(int(f)), nil
			}
			return v, nil
		}
		for k, item := range x {
			next, err := walk(item, count, repl)
			if err != nil {
				return nil, err
			}
			x[k] = next
		}
	}
	return v, nil
}

// hasPlaceholder is a cheap pre-check: false means raw holds no placeholder.
func hasPlaceholder(raw []byte) bool {
	return bytes.Contains(raw, []byte(placeholderKey))
}

// marshalTree encodes a tree without escaping HTML characters.
func marshalTree(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Validate checks that a holds at most l.MaxAttachments attachments and l.MaxEventBytes
// bytes in all, that every value is valid JSON nested no deeper than l.MaxDepth, and
// that every placeholder names an attachment. It returns ErrTooManyAttachments,
// ErrTooLarge, ErrInvalid, ErrDepth or ErrAttachments. It does not modify a.
func (a Arguments) Validate(limits Limits) error {
	l, err := limits.normalized()
	if err != nil {
		return err
	}
	if len(a.Attachments) > l.MaxAttachments {
		return ErrTooManyAttachments
	}
	remaining := l.MaxEventBytes
	for _, v := range a.Values {
		if len(v) > remaining {
			return ErrTooLarge
		}
		remaining -= len(v)
	}
	for _, b := range a.Attachments {
		if len(b) > remaining {
			return ErrTooLarge
		}
		remaining -= len(b)
	}
	for _, v := range a.Values {
		if !json.Valid(v) {
			return ErrInvalid
		}
		if err := checkDepth(v, l.MaxDepth); err != nil {
			return err
		}
		if hasPlaceholder(v) {
			if _, err := walkPlaceholders(v, len(a.Attachments), nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// Concat returns the arguments of parts in order, with the attachments of each part
// appended to the previous ones and the placeholders of later parts renumbered. It is
// how a codec that expands to several positional arguments, such as Args2, joins the
// arguments of its elements. A value that holds a placeholder is re-serialised with
// its object keys sorted; other values are copied unchanged. The result shares no
// memory with parts.
func Concat(parts ...Arguments) (Arguments, error) {
	var out Arguments
	for _, part := range parts {
		offset := len(out.Attachments)
		for _, v := range part.Values {
			if offset > 0 && hasPlaceholder(v) {
				shifted, err := renumber(v, len(part.Attachments), func(i int) int { return i + offset })
				if err != nil {
					return Arguments{}, err
				}
				v = shifted
			}
			out.Values = append(out.Values, bytes.Clone(v))
		}
		out.Attachments = append(out.Attachments, cloneAttachments(part.Attachments)...)
	}
	return out, nil
}

// Slice returns the arguments Values[from:to] with the attachments they reference,
// renumbered in ascending order of their original index. It is the inverse of Concat
// for a codec that reads one element out of several positional arguments. It returns
// ErrArity for a range outside Values and ErrAttachments for a placeholder that names
// no attachment. The result shares no memory with a.
func (a Arguments) Slice(from, to int) (Arguments, error) {
	if from < 0 || to < from || to > len(a.Values) {
		return Arguments{}, ErrArity
	}
	used := map[int]bool{}
	for _, v := range a.Values[from:to] {
		if !hasPlaceholder(v) {
			continue
		}
		if _, err := walkPlaceholders(v, len(a.Attachments), func(i int) any {
			used[i] = true
			return nil
		}); err != nil {
			return Arguments{}, err
		}
	}
	order := make([]int, 0, len(used))
	for i := range used {
		order = append(order, i)
	}
	slices.Sort(order)
	rank := make(map[int]int, len(order))
	var out Arguments
	for newIndex, old := range order {
		rank[old] = newIndex
		out.Attachments = append(out.Attachments, bytes.Clone(a.Attachments[old]))
	}
	for _, v := range a.Values[from:to] {
		if hasPlaceholder(v) {
			shifted, err := renumber(v, len(a.Attachments), func(i int) int { return rank[i] })
			if err != nil {
				return Arguments{}, err
			}
			v = shifted
		}
		out.Values = append(out.Values, bytes.Clone(v))
	}
	return out, nil
}

// renumber rewrites every placeholder index of raw with remap.
func renumber(raw json.RawMessage, count int, remap func(int) int) (json.RawMessage, error) {
	tree, err := walkPlaceholders(raw, count, func(i int) any {
		return map[string]any{placeholderKey: true, "num": json.Number(strconv.Itoa(remap(i)))}
	})
	if err != nil {
		return nil, err
	}
	return marshalTree(tree)
}

// EventPacket returns the EVENT packet that sends args under name. A nil id requests
// no acknowledgement. The packet owns copies of everything it holds; Encode validates
// it, including the placeholders against args.Attachments. An empty or invalid value in
// args is ErrInvalid.
func EventPacket(namespace string, id *uint64, name string, args Arguments) (Packet, error) {
	title, err := json.Marshal(name)
	if err != nil {
		return Packet{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return arrayPacket(Event, namespace, id, title, args)
}

// AckPacket returns the ACK packet that answers acknowledgement id with args, under
// the same rules as EventPacket.
func AckPacket(namespace string, id uint64, args Arguments) (Packet, error) {
	return arrayPacket(Ack, namespace, &id, nil, args)
}

func arrayPacket(t Type, namespace string, id *uint64, first json.RawMessage, args Arguments) (Packet, error) {
	var data []byte
	data = append(data, '[')
	if first != nil {
		data = append(data, first...)
	}
	for i, v := range args.Values {
		if !json.Valid(v) {
			return Packet{}, ErrInvalid
		}
		if i > 0 || first != nil {
			data = append(data, ',')
		}
		data = append(data, v...)
	}
	data = append(data, ']')
	p := Packet{Type: t, Namespace: namespace, Data: data, Attachments: cloneAttachments(args.Attachments)}
	if id != nil {
		v := *id
		p.ID = &v
	}
	return p, nil
}

// EventArguments splits an EVENT packet into its event name and positional
// arguments. A numeric first element, which the Node.js parser accepts, is returned as
// its JSON text. The result is owned by the caller; p is not modified or retained. It
// returns ErrInvalid for another type or a payload that is not an array whose first
// element is a name.
func EventArguments(p Packet) (name string, args Arguments, err error) {
	if p.Type != Event {
		return "", Arguments{}, ErrInvalid
	}
	elems, err := splitArray(p.Data)
	if err != nil || len(elems) == 0 {
		return "", Arguments{}, ErrInvalid
	}
	var n any
	dec := json.NewDecoder(bytes.NewReader(elems[0]))
	dec.UseNumber()
	if dec.Decode(&n) != nil {
		return "", Arguments{}, ErrInvalid
	}
	switch v := n.(type) {
	case string:
		name = v
	case json.Number:
		name = string(v)
	default:
		return "", Arguments{}, ErrInvalid
	}
	return name, Arguments{Values: elems[1:], Attachments: cloneAttachments(p.Attachments)}, nil
}

// AckArguments returns the positional arguments of an ACK packet, owned by the
// caller. It returns ErrInvalid for another type or a payload that is not an array.
func AckArguments(p Packet) (Arguments, error) {
	if p.Type != Ack {
		return Arguments{}, ErrInvalid
	}
	elems, err := splitArray(p.Data)
	if err != nil {
		return Arguments{}, ErrInvalid
	}
	return Arguments{Values: elems, Attachments: cloneAttachments(p.Attachments)}, nil
}

// splitArray returns copies of the elements of a JSON array, in their original
// spelling.
func splitArray(data []byte) ([]json.RawMessage, error) {
	var elems []json.RawMessage
	if err := json.Unmarshal(data, &elems); err != nil || elems == nil {
		return nil, ErrInvalid
	}
	return elems, nil
}

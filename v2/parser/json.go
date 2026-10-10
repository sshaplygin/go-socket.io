package parser

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

var (
	binaryType      = reflect.TypeOf((*BinaryValue)(nil)).Elem()
	jsonMarshaler   = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshaler   = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	holdsBinaryMemo sync.Map // reflect.Type -> bool
	structFieldMemo sync.Map // reflect.Type -> structFieldsResult
)

// JSON returns the ArgumentCodec that sends a T as exactly one positional JSON
// argument, with encoding/json's spelling and struct tags.
//
// Encode replaces every BinaryValue, including one nested in a struct, slice, array,
// map, pointer or interface, by a placeholder and appends its bytes to
// Arguments.Attachments in the order the value is traversed (struct fields in
// declaration order, map keys sorted). Decode checks the placeholders against the
// attachments and decodes each placeholder as the bytes of its attachment, so it fills
// a []byte or a named byte-slice field such as socketio.Binary; a placeholder that
// meets a string or an interface decodes to the base64 text encoding/json gives bytes.
//
// Encode copies the bytes it keeps and does not modify its argument; Decode returns
// owned values and does not modify or retain its argument. A placeholder that is
// referenced more than once decodes to one copy of its attachment per reference, so
// Decode counts every reference against Limits.MaxEventBytes: the bytes of the value
// plus the bytes of all references, repeated ones included, must not exceed it, or
// Decode returns ErrTooLarge. Both enforce limits (zero fields select the defaults of
// Limits) and return ErrArity unless exactly one argument is present.
//
// Encode supports the types encoding/json does, with these exceptions that apply only
// to a value that holds a BinaryValue: a map key must have the string kind, a struct
// must not use the ",string" or ",omitzero" tag options, and two fields must not
// share a JSON name. Such a value returns ErrUnsupported. A type with its own
// MarshalJSON or MarshalText is sent as that method produces it, and its bytes are
// not searched for attachments.
func JSON[T any](limits Limits) ArgumentCodec[T] {
	l, lerr := limits.normalized()
	return ArgumentCodec[T]{
		Encode: func(v T) (Arguments, error) {
			if lerr != nil {
				return Arguments{}, lerr
			}
			return encodeJSON(&v, l)
		},
		Decode: func(a Arguments) (T, error) {
			var zero T
			if lerr != nil {
				return zero, lerr
			}
			if len(a.Values) != 1 {
				return zero, ErrArity
			}
			if err := a.Validate(l); err != nil {
				return zero, err
			}
			raw := []byte(a.Values[0])
			if hasPlaceholder(raw) {
				// Every reference expands to its own copy of the attachment, so a
				// repeated reference counts again. Validate bounded the message once;
				// the expansion is bounded by the same MaxEventBytes.
				budget, overflow := l.MaxEventBytes-len(raw), false
				tree, err := walkPlaceholders(raw, len(a.Attachments), func(i int) any {
					if overflow || len(a.Attachments[i]) > budget {
						overflow = true
						return nil
					}
					budget -= len(a.Attachments[i])
					return a.Attachments[i]
				})
				if err != nil {
					return zero, err
				}
				if overflow {
					return zero, ErrTooLarge
				}
				if raw, err = marshalTree(tree); err != nil {
					return zero, err
				}
			}
			var out T
			if err := json.Unmarshal(raw, &out); err != nil {
				return zero, fmt.Errorf("%w: %v", ErrInvalid, err)
			}
			return out, nil
		},
	}
}

// encodeJSON converts *v, which must be a pointer so that pointer-receiver
// BinaryValue methods of addressable values are found.
func encodeJSON[T any](v *T, l Limits) (Arguments, error) {
	d := deconstructor{l: l}
	if err := d.value(reflect.ValueOf(v).Elem(), 0); err != nil {
		return Arguments{}, err
	}
	if len(d.buf) > l.MaxEventBytes-d.size {
		return Arguments{}, ErrTooLarge
	}
	if err := checkDepth(d.buf, l.MaxDepth); err != nil {
		return Arguments{}, err
	}
	return Arguments{Values: []json.RawMessage{d.buf}, Attachments: d.attachments}, nil
}

type deconstructor struct {
	l           Limits
	buf         []byte
	attachments [][]byte
	size        int // bytes of attachments so far
}

func (d *deconstructor) value(rv reflect.Value, depth int) error {
	if !rv.IsValid() {
		d.buf = append(d.buf, "null"...)
		return nil
	}
	t := rv.Type()
	switch rv.Kind() {
	case reflect.Interface:
		if rv.IsNil() {
			d.buf = append(d.buf, "null"...)
			return nil
		}
		return d.value(rv.Elem(), depth)
	case reflect.Pointer:
		if rv.IsNil() {
			d.buf = append(d.buf, "null"...)
			return nil
		}
	}
	if bv, ok := binaryOf(rv); ok {
		return d.attach(bv.SocketIOBinary())
	}
	if !holdsBinary(t) || customMarshaler(t) {
		return d.leaf(rv)
	}
	if depth >= d.l.MaxDepth {
		return ErrDepth
	}
	switch rv.Kind() {
	case reflect.Pointer:
		return d.value(rv.Elem(), depth)
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			d.buf = append(d.buf, "null"...)
			return nil
		}
		d.buf = append(d.buf, '[')
		for i := 0; i < rv.Len(); i++ {
			if i > 0 {
				d.buf = append(d.buf, ',')
			}
			if err := d.value(rv.Index(i), depth+1); err != nil {
				return err
			}
		}
		d.buf = append(d.buf, ']')
		return nil
	case reflect.Map:
		return d.mapValue(rv, depth)
	case reflect.Struct:
		return d.structValue(rv, depth)
	}
	return d.leaf(rv)
}

// binaryOf finds the BinaryValue behind rv: a value whose type implements the
// interface, or an addressable value whose pointer type does.
func binaryOf(rv reflect.Value) (BinaryValue, bool) {
	t := rv.Type()
	if t.Implements(binaryType) {
		if rv.CanInterface() {
			bv, ok := rv.Interface().(BinaryValue)
			return bv, ok
		}
		return nil, false
	}
	if t.Kind() != reflect.Pointer && rv.CanAddr() && reflect.PointerTo(t).Implements(binaryType) && rv.Addr().CanInterface() {
		bv, ok := rv.Addr().Interface().(BinaryValue)
		return bv, ok
	}
	return nil, false
}

func (d *deconstructor) attach(b []byte) error {
	if len(d.attachments) >= d.l.MaxAttachments {
		return ErrTooManyAttachments
	}
	if len(b) > d.l.MaxEventBytes-d.size {
		return ErrTooLarge
	}
	d.size += len(b)
	d.attachments = append(d.attachments, append([]byte{}, b...))
	d.buf = append(d.buf, Placeholder(len(d.attachments)-1)...)
	return nil
}

// leaf appends the encoding/json spelling of a value that holds no BinaryValue to
// replace.
func (d *deconstructor) leaf(rv reflect.Value) error {
	if !rv.CanInterface() {
		return fmt.Errorf("%w: unexported field of type %s", ErrUnsupported, rv.Type())
	}
	b, err := json.Marshal(rv.Interface())
	if err != nil {
		var ute *json.UnsupportedTypeError
		var uve *json.UnsupportedValueError
		if errors.As(err, &ute) || errors.As(err, &uve) {
			return fmt.Errorf("%w: %v", ErrUnsupported, err)
		}
		return err
	}
	if len(b) > d.l.MaxEventBytes {
		return ErrTooLarge
	}
	d.buf = append(d.buf, b...)
	return nil
}

func (d *deconstructor) mapValue(rv reflect.Value, depth int) error {
	if rv.IsNil() {
		d.buf = append(d.buf, "null"...)
		return nil
	}
	if rv.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("%w: map key type %s holds a binary value", ErrUnsupported, rv.Type().Key())
	}
	keys := rv.MapKeys()
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	d.buf = append(d.buf, '{')
	for i, k := range keys {
		if i > 0 {
			d.buf = append(d.buf, ',')
		}
		name, _ := json.Marshal(k.String())
		d.buf = append(d.buf, name...)
		d.buf = append(d.buf, ':')
		if err := d.value(rv.MapIndex(k), depth+1); err != nil {
			return err
		}
	}
	d.buf = append(d.buf, '}')
	return nil
}

func (d *deconstructor) structValue(rv reflect.Value, depth int) error {
	fields, err := fieldsOf(rv.Type())
	if err != nil {
		return err
	}
	d.buf = append(d.buf, '{')
	first := true
	for _, f := range fields {
		fv, err := rv.FieldByIndexErr(f.index)
		if err != nil {
			continue // a nil embedded pointer contributes no fields
		}
		if f.omitEmpty && isEmpty(fv) {
			continue
		}
		if !first {
			d.buf = append(d.buf, ',')
		}
		first = false
		d.buf = append(d.buf, f.quoted...)
		d.buf = append(d.buf, ':')
		if err := d.value(fv, depth+1); err != nil {
			return err
		}
	}
	d.buf = append(d.buf, '}')
	return nil
}

func isEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}

func customMarshaler(t reflect.Type) bool {
	if t.Kind() == reflect.Interface {
		return false
	}
	for _, m := range []reflect.Type{jsonMarshaler, textMarshaler} {
		if t.Implements(m) || reflect.PointerTo(t).Implements(m) {
			return true
		}
	}
	return false
}

// holdsBinary reports whether a value of type t may contain a BinaryValue, so that
// the walker, not encoding/json, must produce it. An interface may hold anything.
func holdsBinary(t reflect.Type) bool {
	if v, ok := holdsBinaryMemo.Load(t); ok {
		return v.(bool)
	}
	r := holdsBinaryIn(t, map[reflect.Type]bool{})
	holdsBinaryMemo.Store(t, r)
	return r
}

func holdsBinaryIn(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t.Implements(binaryType) || (t.Kind() != reflect.Interface && reflect.PointerTo(t).Implements(binaryType)) {
		return true
	}
	switch t.Kind() {
	case reflect.Interface:
		return true
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return holdsBinaryIn(t.Elem(), seen)
	case reflect.Struct:
		if seen[t] {
			return false
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			if f := t.Field(i); (f.IsExported() || f.Anonymous) && holdsBinaryIn(f.Type, seen) {
				return true
			}
		}
	}
	return false
}

type field struct {
	index     []int
	quoted    []byte // the JSON-encoded field name
	omitEmpty bool
}

type structFieldsResult struct {
	fields []field
	err    error
}

func fieldsOf(t reflect.Type) ([]field, error) {
	if v, ok := structFieldMemo.Load(t); ok {
		r := v.(structFieldsResult)
		return r.fields, r.err
	}
	names := map[string]bool{}
	fields, err := collectFields(t, nil, names, map[reflect.Type]bool{})
	structFieldMemo.Store(t, structFieldsResult{fields, err})
	return fields, err
}

func collectFields(t reflect.Type, prefix []int, names map[string]bool, active map[reflect.Type]bool) ([]field, error) {
	if active[t] {
		return nil, nil
	}
	active[t] = true
	defer delete(active, t)
	var out []field
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		tag, hasTag := sf.Tag.Lookup("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		omitEmpty := false
		for _, o := range strings.Split(opts, ",") {
			switch o {
			case "omitempty":
				omitEmpty = true
			case "string", "omitzero":
				return nil, fmt.Errorf("%w: tag option %q on %s.%s with a binary value", ErrUnsupported, o, t, sf.Name)
			}
		}
		index := append(append([]int{}, prefix...), i)
		if sf.Anonymous && name == "" {
			ft := sf.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				inner, err := collectFields(ft, index, names, active)
				if err != nil {
					return nil, err
				}
				out = append(out, inner...)
				continue
			}
		}
		if !sf.IsExported() {
			continue
		}
		if name == "" || !hasTag {
			name = sf.Name
		}
		if names[name] {
			return nil, fmt.Errorf("%w: duplicate JSON name %q in %s with a binary value", ErrUnsupported, name, t)
		}
		names[name] = true
		quoted, _ := json.Marshal(name)
		out = append(out, field{index: index, quoted: bytes.Clone(quoted), omitEmpty: omitEmpty})
	}
	return out, nil
}

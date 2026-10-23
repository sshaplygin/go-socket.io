package parser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// payloadEncoder reads original values. Omission and custom marshaling decisions
// therefore observe the caller's values, before any binary representation exists.
// Only Buffers actually visited while writing JSON become attachments.
type payloadEncoder struct {
	bytes.Buffer
	buffers [][]byte
	active  map[jsonVisit]bool
	depth   int
}

type jsonVisit struct {
	typ    reflect.Type
	ptr    uintptr
	length int
}

// The JSONv2 implementation emits a literal replacement rune, while legacy
// encoding/json escapes it. Keep invalid UTF-8 aligned with the active version.
var invalidUTF8JSON = func() string {
	data, _ := json.Marshal("\xff")
	return string(data[1 : len(data)-1])
}()

func (e *payloadEncoder) value(v reflect.Value, quoted bool) error {
	if !v.IsValid() || (v.Kind() == reflect.Pointer && v.IsNil()) {
		e.WriteString("null")
		return nil
	}
	if v.Type() == bufferType {
		if len(e.buffers) == maxAttachments {
			return errTooManyAttachments
		}
		buffer := v.Interface().(Buffer)
		e.WriteString(`{"_placeholder":true,"num":`)
		e.WriteString(strconv.Itoa(len(e.buffers)))
		e.WriteByte('}')
		e.buffers = append(e.buffers, bytes.Clone(buffer.Data))
		return nil
	}
	if data, custom, err := marshalJSONValue(v); custom {
		if err == nil {
			e.Write(data)
		}
		return err
	}
	if !quoted && isPlainJSONType(v.Type()) {
		switch v.Kind() {
		case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array:
			return e.standard(v, false)
		}
	}
	e.depth++
	defer func() { e.depth-- }()
	if e.depth > 1000 {
		return &json.UnsupportedValueError{Value: v, Str: "value exceeds maximum nesting depth"}
	}
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
		if !v.IsNil() {
			visit := jsonVisit{typ: v.Type(), ptr: v.Pointer()}
			if v.Kind() == reflect.Slice {
				visit.length = v.Len()
			}
			if e.active[visit] {
				return &json.UnsupportedValueError{Value: v, Str: "encountered a cycle"}
			}
			if e.active == nil {
				e.active = make(map[jsonVisit]bool)
			}
			e.active[visit] = true
			defer delete(e.active, visit)
		}
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			e.WriteString("null")
			return nil
		}
		return e.value(v.Elem(), quoted)
	case reflect.Struct:
		return e.object(v)
	case reflect.Map:
		return e.mapping(v)
	case reflect.Slice:
		// encoding/json treats byte slices as base64 unless their elements
		// have a JSON or text marshaler.
		element := v.Type().Elem()
		if element.Kind() == reflect.Uint8 && !implementsJSONMethod(reflect.PointerTo(element), jsonMarshalMethods) {
			return e.standard(v, false)
		}
		if v.IsNil() {
			e.WriteString("null")
			return nil
		}
		fallthrough
	case reflect.Array:
		e.WriteByte('[')
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				e.WriteByte(',')
			}
			if err := e.value(v.Index(i), false); err != nil {
				return err
			}
		}
		e.WriteByte(']')
		return nil
	default:
		return e.scalar(v, quoted)
	}
}

func (e *payloadEncoder) scalar(v reflect.Value, quoted bool) error {
	if quoted || v.Type() == reflect.TypeOf(json.Number("")) {
		return e.standard(v, quoted)
	}
	var scratch [64]byte
	var data []byte
	switch v.Kind() {
	case reflect.String:
		e.string(v.String())
		return nil
	case reflect.Bool:
		data = strconv.AppendBool(scratch[:0], v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		data = strconv.AppendInt(scratch[:0], v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		data = strconv.AppendUint(scratch[:0], v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		value := v.Float()
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return &json.UnsupportedValueError{Value: v, Str: strconv.FormatFloat(value, 'g', -1, v.Type().Bits())}
		}
		format := byte('f')
		abs := math.Abs(value)
		if v.Kind() == reflect.Float32 {
			if abs != 0 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) {
				format = 'e'
			}
		} else if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
			format = 'e'
		}
		data = strconv.AppendFloat(scratch[:0], value, format, -1, v.Type().Bits())
		if n := len(data); format == 'e' && n >= 4 && data[n-3] == '-' && data[n-2] == '0' {
			data[n-2] = data[n-1]
			data = data[:n-1]
		}
	default:
		return e.standard(v, false)
	}
	e.Write(data)
	return nil
}

// string follows encoding/json's default HTML and invalid-UTF-8 escaping.
func (e *payloadEncoder) string(value string) {
	const hex = "0123456789abcdef"
	e.WriteByte('"')
	start := 0
	for i := 0; i < len(value); {
		c := value[i]
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(value[i:])
			if (r == utf8.RuneError && size == 1) || r == '\u2028' || r == '\u2029' {
				e.WriteString(value[start:i])
				switch r {
				case '\u2028':
					e.WriteString(`\u2028`)
				case '\u2029':
					e.WriteString(`\u2029`)
				default:
					e.WriteString(invalidUTF8JSON)
				}
				i += size
				start = i
			} else {
				i += size
			}
			continue
		}
		if c >= 0x20 && c != '"' && c != '\\' && c != '<' && c != '>' && c != '&' {
			i++
			continue
		}
		e.WriteString(value[start:i])
		switch c {
		case '"', '\\':
			e.WriteByte('\\')
			e.WriteByte(c)
		case '\b':
			e.WriteString(`\b`)
		case '\f':
			e.WriteString(`\f`)
		case '\n':
			e.WriteString(`\n`)
		case '\r':
			e.WriteString(`\r`)
		case '\t':
			e.WriteString(`\t`)
		default:
			e.WriteString(`\u00`)
			e.WriteByte(hex[c>>4])
			e.WriteByte(hex[c&15])
		}
		i++
		start = i
	}
	e.WriteString(value[start:])
	e.WriteByte('"')
}

func (e *payloadEncoder) standard(v reflect.Value, quoted bool) error {
	data, err := json.Marshal(v.Interface())
	if err != nil {
		return err
	}
	if quoted {
		data, err = json.Marshal(string(data))
		if err != nil {
			return err
		}
	}
	e.Write(data)
	return nil
}

func (e *payloadEncoder) object(v reflect.Value) error {
	e.WriteByte('{')
	first := true
	for _, field := range jsonFields(v.Type()) {
		value := jsonFieldValue(v, field.path)
		if !value.IsValid() || (field.omitEmpty && isJSONEmpty(value)) || (field.omitZero && isJSONZero(value)) {
			continue
		}
		if !first {
			e.WriteByte(',')
		}
		first = false
		e.Write(field.name)
		e.WriteByte(':')
		if err := e.value(value, field.quoted); err != nil {
			return err
		}
	}
	e.WriteByte('}')
	return nil
}

func (e *payloadEncoder) mapping(v reflect.Value) error {
	keyType := v.Type().Key()
	switch keyType.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
	default:
		if !implementsJSONMethod(keyType, jsonTextMethods) {
			return &json.UnsupportedTypeError{Type: v.Type()}
		}
	}
	if v.IsNil() {
		e.WriteString("null")
		return nil
	}
	type entry struct {
		name  string
		value reflect.Value
	}
	entries := make([]entry, 0, v.Len())
	for it := v.MapRange(); it.Next(); {
		name, err := jsonMapKey(it.Key())
		if err != nil {
			return err
		}
		entries = append(entries, entry{name, it.Value()})
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.name, b.name) })
	e.WriteByte('{')
	for i, entry := range entries {
		if i > 0 {
			e.WriteByte(',')
		}
		e.string(entry.name)
		e.WriteByte(':')
		if err := e.value(entry.value, false); err != nil {
			return err
		}
	}
	e.WriteByte('}')
	return nil
}

func jsonMapKey(v reflect.Value) (string, error) {
	if v.Kind() == reflect.String && !stringJSONKeysUseMethods {
		return v.String(), nil
	}
	if implementsJSONMethod(v.Type(), jsonTextMethods) {
		if v.Kind() == reflect.Pointer && v.IsNil() {
			return "", nil
		}
		data, err := marshalJSONText(v)
		if err != nil {
			return "", fmt.Errorf("JSON map key: %w", err)
		}
		return string(data), nil
	}
	if v.Kind() == reflect.String {
		return v.String(), nil
	}
	if v.Kind() >= reflect.Int && v.Kind() <= reflect.Int64 {
		return strconv.FormatInt(v.Int(), 10), nil
	}
	return strconv.FormatUint(v.Uint(), 10), nil
}

// marshalJSONValue preserves pointer-receiver addressability and lets the
// standard library validate, compact and escape custom marshaled output.
func marshalJSONValue(v reflect.Value) ([]byte, bool, error) {
	t := v.Type()
	if t.Kind() == reflect.Pointer && t.Elem() == bufferType {
		return nil, false, nil
	}
	if v.Kind() == reflect.Interface {
		if !v.IsNil() {
			concrete := v.Elem().Type()
			if concrete == bufferType || (concrete.Kind() == reflect.Pointer && concrete.Elem() == bufferType) {
				return nil, false, nil
			}
		}
		if implementsJSONMethod(t, jsonMarshalMethods) {
			// Keep the static interface type: its marshaler may intentionally
			// accept a nil pointer receiver, unlike a plain nil pointer value.
			array := reflect.New(reflect.ArrayOf(1, t)).Elem()
			array.Index(0).Set(v)
			data, err := json.Marshal(array.Interface())
			if err != nil {
				return nil, true, err
			}
			return data[1 : len(data)-1], true, nil
		}
		return nil, false, nil
	}
	for _, marshaler := range jsonMarshalMethods {
		if v.CanAddr() && reflect.PointerTo(t).Implements(marshaler) {
			data, err := json.Marshal(v.Addr().Interface())
			return data, true, err
		}
		if t.Implements(marshaler) {
			data, err := json.Marshal(v.Interface())
			return data, true, err
		}
	}
	return nil, false, nil
}

func isJSONEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Interface, reflect.Pointer:
		return v.IsZero()
	}
	return false
}

func implementsJSONMethod(t reflect.Type, methods []reflect.Type) bool {
	for _, method := range methods {
		if t.Implements(method) {
			return true
		}
	}
	return false
}

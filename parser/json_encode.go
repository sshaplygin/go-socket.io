package parser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
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
		return e.standard(v, quoted)
	}
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
		name, _ := json.Marshal(entry.name)
		e.Write(name)
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

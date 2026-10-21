//go:build go1.25 && goexperiment.jsonv2

package parser

import (
	"encoding"
	"encoding/json"
	"reflect"
)

var (
	textAppender       = reflect.TypeOf((*encoding.TextAppender)(nil)).Elem()
	jsonMarshalMethods = []reflect.Type{jsonStreamingMarshaler, jsonMarshaler, textAppender, textMarshaler}
	jsonTextMethods    = []reflect.Type{textAppender, textMarshaler}
	jsonFieldMethods   = []reflect.Type{
		jsonStreamingMarshaler, jsonMarshaler, textAppender, textMarshaler,
		jsonStreamingUnmarshaler, reflect.TypeOf((*json.Unmarshaler)(nil)).Elem(),
		reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem(),
	}
)

// JSONv2 excludes named unexported embedded fields whose methods cannot be
// called through reflection, including decode-only methods and omitzero.
func ignorePrivateJSONField(t reflect.Type, omitZero bool) bool {
	pointer := reflect.PointerTo(t)
	return implementsJSONMethod(t, jsonFieldMethods) || implementsJSONMethod(pointer, jsonFieldMethods) ||
		(omitZero && (t.Implements(isZeroType) || pointer.Implements(isZeroType)))
}

func marshalJSONText(v reflect.Value) ([]byte, error) {
	if v.Type().Implements(textAppender) {
		return v.Interface().(encoding.TextAppender).AppendText(nil)
	}
	return v.Interface().(encoding.TextMarshaler).MarshalText()
}

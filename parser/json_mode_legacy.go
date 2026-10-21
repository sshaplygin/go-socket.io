//go:build !goexperiment.jsonv2

package parser

import (
	"encoding"
	"reflect"
)

const quoteNamedJSONPointers = false
const stringJSONKeysUseMethods = false

var jsonMarshalMethods = []reflect.Type{jsonMarshaler, textMarshaler}
var jsonTextMethods = []reflect.Type{textMarshaler}

func ignorePrivateJSONField(reflect.Type, bool) bool { return false }

func marshalJSONText(v reflect.Value) ([]byte, error) {
	return v.Interface().(encoding.TextMarshaler).MarshalText()
}

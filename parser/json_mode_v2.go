//go:build go1.27 && goexperiment.jsonv2

package parser

import (
	jsonv2 "encoding/json/v2"
	"reflect"
)

// Go 1.27 follows named pointers for ,string and bypasses text methods on
// string-kind map keys, matching the original encoder for those keys.
const quoteNamedJSONPointers = true
const stringJSONKeysUseMethods = false

var jsonStreamingMarshaler = reflect.TypeOf((*jsonv2.MarshalerTo)(nil)).Elem()
var jsonStreamingUnmarshaler = reflect.TypeOf((*jsonv2.UnmarshalerFrom)(nil)).Elem()

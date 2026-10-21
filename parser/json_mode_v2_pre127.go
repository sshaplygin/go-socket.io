//go:build go1.25 && !go1.27 && goexperiment.jsonv2

package parser

import (
	jsonv2 "encoding/json/v2"
	"reflect"
)

// The early JSONv2 experiment ignores ,string on named pointers and calls
// text methods on string-kind map keys.
const quoteNamedJSONPointers = false
const stringJSONKeysUseMethods = true

var jsonStreamingMarshaler = reflect.TypeOf((*jsonv2.MarshalerTo)(nil)).Elem()
var jsonStreamingUnmarshaler = reflect.TypeOf((*jsonv2.UnmarshalerFrom)(nil)).Elem()

package parser

import (
	"reflect"
	"sync"
)

var plainJSONTypes sync.Map // map[reflect.Type]bool

// A plain subtree can be handed to encoding/json without hiding attachments,
// interface values, addressable pointer methods or traversal cycles.
func isPlainJSONType(t reflect.Type) bool {
	if plain, ok := plainJSONTypes.Load(t); ok {
		return plain.(bool)
	}
	plain := checkPlainJSONType(t, make(map[reflect.Type]bool))
	actual, _ := plainJSONTypes.LoadOrStore(t, plain)
	return actual.(bool)
}

func checkPlainJSONType(t reflect.Type, active map[reflect.Type]bool) bool {
	if t == bufferType || active[t] || implementsJSONMethod(t, jsonMarshalMethods) || implementsJSONMethod(reflect.PointerTo(t), jsonMarshalMethods) {
		return false
	}
	active[t] = true
	defer delete(active, t)
	switch t.Kind() {
	case reflect.Pointer, reflect.Interface:
		return false
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if !checkPlainJSONType(t.Field(i).Type, active) {
				return false
			}
		}
		return true
	case reflect.Map:
		return checkPlainJSONType(t.Key(), active) && checkPlainJSONType(t.Elem(), active)
	case reflect.Array, reflect.Slice:
		return checkPlainJSONType(t.Elem(), active)
	default:
		return true
	}
}

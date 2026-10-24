package parser

import (
	"reflect"
	"sync"
)

// plainJSONTypes caches, per reflect.Type, whether a value of that type may be
// handed to encoding/json in a single call. The answer is conservative: false
// only selects the attachment-aware traversal, which is always correct, so a
// type is plain only when every type reachable from it is plain. Keys are
// reflect.Type values, so the cache is bounded by the number of distinct types
// the program encodes. Concurrent Prepare calls may check the same type at the
// same time; they compute the same answer and LoadOrStore keeps the first.
var plainJSONTypes sync.Map // map[reflect.Type]bool

// A plain subtree can be handed to encoding/json without hiding attachments,
// interface values, addressable pointer methods or traversal cycles.
//
// Addressable pointer methods: the attachment-aware traversal calls a MarshalJSON
// or MarshalText method declared on *T for an addressable T value, as
// encoding/json does for addressable values. json.Marshal(v.Interface()) copies
// the value and loses addressability, so it would skip such a method; a type T
// whose *T has a marshaling method is therefore not plain.
//
// Recursive types are not plain: their slices and maps can form value cycles,
// which the traversal reports as errors.
func isPlainJSONType(t reflect.Type) bool {
	if plain, ok := plainJSONTypes.Load(t); ok {
		return plain.(bool)
	}
	return checkPlainJSONType(t, make(map[reflect.Type]bool))
}

// checkPlainJSONType stores the result of every type it visits, so each distinct
// type is checked once even when many paths share it. Storing sub-results is
// safe because none depends on where the walk started: a type returns false
// through active only when it reaches a type that is still being checked, which
// puts both on a cycle, so the type is recursive and not plain from any start.
func checkPlainJSONType(t reflect.Type, active map[reflect.Type]bool) bool {
	if plain, ok := plainJSONTypes.Load(t); ok {
		return plain.(bool)
	}
	if active[t] {
		return false
	}
	active[t] = true
	plain := plainJSONTypeFields(t, active)
	delete(active, t)
	actual, _ := plainJSONTypes.LoadOrStore(t, plain)
	return actual.(bool)
}

func plainJSONTypeFields(t reflect.Type, active map[reflect.Type]bool) bool {
	if t == bufferType || implementsJSONMethod(t, jsonMarshalMethods) || implementsJSONMethod(reflect.PointerTo(t), jsonMarshalMethods) {
		return false
	}
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

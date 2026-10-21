//go:build go1.24

package parser

import "reflect"

type jsonZeroer interface{ IsZero() bool }

var isZeroType = reflect.TypeOf((*jsonZeroer)(nil)).Elem()

// isJSONZero evaluates omission on the original field, with encoding/json's
// nil-receiver guards and boxing for pointer methods on unaddressable values.
func isJSONZero(v reflect.Value) bool {
	t := v.Type()
	if t.Implements(isZeroType) {
		if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
			return true
		}
		if v.Kind() == reflect.Interface && v.Elem().Kind() == reflect.Pointer && v.Elem().IsNil() {
			return true
		}
		return v.Interface().(jsonZeroer).IsZero()
	}
	if reflect.PointerTo(t).Implements(isZeroType) {
		if !v.CanAddr() {
			boxed := reflect.New(t).Elem()
			boxed.Set(v)
			v = boxed
		}
		return v.Addr().Interface().(jsonZeroer).IsZero()
	}
	return v.IsZero()
}

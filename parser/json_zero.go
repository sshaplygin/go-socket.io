//go:build go1.24

package parser

import "reflect"

var isZeroType = reflect.TypeOf((*interface{ IsZero() bool })(nil)).Elem()

// Numbering a zero Buffer would make it nonzero. Preserve default omitzero
// decisions before copying. User-defined IsZero methods are evaluated only by
// encoding/json; finishAttachments removes any buffers their result omits.
func isJSONZero(v reflect.Value) bool {
	if hasJSONZeroMethod(v) {
		return false
	}
	return v.IsZero()
}

func hasJSONZeroMethod(v reflect.Value) bool {
	t := v.Type()
	return t.Implements(isZeroType) || reflect.PointerTo(t).Implements(isZeroType)
}

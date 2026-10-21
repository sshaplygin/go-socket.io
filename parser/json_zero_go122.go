//go:build !go1.24

package parser

import "reflect"

// Go versions before 1.24 ignore the omitzero JSON tag option.
func isJSONZero(reflect.Value) bool { return false }

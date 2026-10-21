//go:build !go1.27 || !goexperiment.jsonv2

package parser

// The original and pre-Go-1.27 experimental JSON implementations ignore
// ,string on named pointers.
const quoteNamedJSONPointers = false

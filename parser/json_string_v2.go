//go:build go1.27 && goexperiment.jsonv2

package parser

// Go 1.27's JSONv2 implementation follows named pointers for ,string.
const quoteNamedJSONPointers = true

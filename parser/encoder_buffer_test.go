package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Placeholder numbers belong to the packet, not to the Buffer: the same *Buffer twice in one
// packet gets two numbers and two binary frames, and the Buffer stays as the caller made it.
func TestEncodeSharedBufferPositions(t *testing.T) {
	shared := &Buffer{Data: []byte{7, 8}}
	other := &Buffer{Data: []byte{9}}

	for _, tc := range []struct {
		name string
		args []interface{}
		want []string
	}{
		{"after another", []interface{}{other, shared}, []string{`52-["e",{"_placeholder":true,"num":0},{"_placeholder":true,"num":1}]` + "\n", "\t", "\a\b"}},
		{"twice", []interface{}{shared, shared}, []string{`52-["e",{"_placeholder":true,"num":0},{"_placeholder":true,"num":1}]` + "\n", "\a\b", "\a\b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := fakeWriter{}
			require.NoError(t, NewEncoder(&w).Encode(Header{Type: Event}, append([]interface{}{"e"}, tc.args...)))

			var got []string
			for _, d := range w.data {
				got = append(got, d.String())
			}
			require.Equal(t, tc.want, got)
			require.Equal(t, Buffer{Data: []byte{7, 8}}, *shared)
			require.Equal(t, Buffer{Data: []byte{9}}, *other)
		})
	}
}

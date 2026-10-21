package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type (
	embeddedInner struct{ B *Buffer }
	embeddedOuter struct {
		embeddedInner
		N int
	}
	embeddedPtrOuter struct {
		*embeddedInner
		N int
	}
	bufferList   []*Buffer
	embeddedList struct{ bufferList }
)

// Repeated Buffer values receive separate placeholders without modifying the
// caller. JSON-ignored fields produce no attachments, while visible fields of
// embedded structs and pointers are encoded without modifying the original.
func TestEncodeBufferShapes(t *testing.T) {
	ph := func(n int) string { return `{"_placeholder":true,"num":` + string(rune('0'+n)) + `}` }
	for _, tc := range []struct {
		name string
		arg  func(b *Buffer) []interface{}
		want []string
		err  error
	}{
		{"twice", func(b *Buffer) []interface{} { return []interface{}{&Buffer{Data: []byte{9}}, b, b} },
			[]string{`53-["e",` + ph(0) + "," + ph(1) + "," + ph(2) + "]\n", "\t", "\x01", "\x01"}, nil},
		{"embedded struct", func(b *Buffer) []interface{} { return []interface{}{embeddedOuter{embeddedInner{b}, 1}} },
			[]string{`51-["e",{"B":` + ph(0) + ",\"N\":1}]\n", "\x01"}, nil},
		{"embedded struct pointer", func(b *Buffer) []interface{} { return []interface{}{&embeddedOuter{embeddedInner{b}, 1}} },
			[]string{`51-["e",{"B":` + ph(0) + ",\"N\":1}]\n", "\x01"}, nil},
		{"field", func(b *Buffer) []interface{} { return []interface{}{struct{ b *Buffer }{b}} }, []string{"2[\"e\",{}]\n"}, nil},
		{"embedded pointer", func(b *Buffer) []interface{} { return []interface{}{embeddedPtrOuter{&embeddedInner{b}, 1}} },
			[]string{`51-["e",{"B":` + ph(0) + ",\"N\":1}]\n", "\x01"}, nil},
		{"embedded slice", func(b *Buffer) []interface{} { return []interface{}{embeddedList{bufferList{b}}} }, []string{"2[\"e\",{}]\n"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &Buffer{Data: []byte{1}}
			w := fakeWriter{}
			err := NewEncoder(&w).Encode(Header{Type: Event}, append([]interface{}{"e"}, tc.arg(b)...))
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				var got []string
				for _, d := range w.data {
					got = append(got, d.String())
				}
				require.Equal(t, tc.want, got)
			}
			require.Equal(t, Buffer{Data: []byte{1}}, *b, "the encoder wrote to the caller's Buffer")
		})
	}
}

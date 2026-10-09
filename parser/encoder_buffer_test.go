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

type embeddedInner struct{ B *Buffer }

type embeddedOuter struct {
	embeddedInner
	N int
}

type embeddedPtrOuter struct {
	*embeddedInner
	N int
}

// A Buffer in the exported field of an embedded unexported struct is numbered, as on v1.x, and
// the caller's value stays as it was.
func TestEncodeEmbeddedUnexportedStruct(t *testing.T) {
	buf := &Buffer{Data: []byte{1}}
	outer := embeddedOuter{embeddedInner{buf}, 1}

	for name, arg := range map[string]interface{}{"value": outer, "pointer": &outer} {
		t.Run(name, func(t *testing.T) {
			w := fakeWriter{}
			require.NoError(t, NewEncoder(&w).Encode(Header{Type: Event}, []interface{}{"e", arg}))
			require.Len(t, w.data, 2)
			require.Equal(t, `51-["e",{"B":{"_placeholder":true,"num":0},"N":1}]`+"\n", w.data[0].String())
			require.Equal(t, "\x01", w.data[1].String())
			require.Same(t, buf, outer.B)
			require.Equal(t, Buffer{Data: []byte{1}}, *buf)
		})
	}
}

// A Buffer behind an unexported field, or behind an embedded pointer to an unexported struct,
// cannot be numbered without writing to the caller's value: Encode returns errUnsupportedBuffer.
// On v1.x the unexported field panicked in reflect and the embedded pointer wrote into the Buffer.
func TestEncodeUnexportedBuffer(t *testing.T) {
	buf := &Buffer{Data: []byte{1}}

	for name, arg := range map[string]interface{}{
		"field":            struct{ b *Buffer }{buf},
		"embedded pointer": embeddedPtrOuter{&embeddedInner{buf}, 1},
	} {
		t.Run(name, func(t *testing.T) {
			err := NewEncoder(&fakeWriter{}).Encode(Header{Type: Event}, []interface{}{"e", arg})
			require.ErrorIs(t, err, errUnsupportedBuffer)
			require.Equal(t, Buffer{Data: []byte{1}}, *buf)
		})
	}
}

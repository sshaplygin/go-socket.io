package parser

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var packetVectors = []struct {
	packets []Packet
	frames  []Frame
}{
	{nil, nil},
	{[]Packet{
		{String, OPEN, []byte{}},
	}, []Frame{
		{String, []byte("0")},
	},
	},
	{[]Packet{
		{String, MESSAGE, []byte("hello 你好")},
	}, []Frame{
		{String, []byte("4hello 你好")},
	},
	},
	{[]Packet{
		{Binary, MESSAGE, []byte("hello 你好")},
	}, []Frame{
		{Binary, []byte{0x04, 'h', 'e', 'l', 'l', 'o', ' ', 0xe4, 0xbd, 0xa0, 0xe5, 0xa5, 0xbd}},
	},
	},
	{[]Packet{
		{String, OPEN, []byte{}},
		{Binary, MESSAGE, []byte("hello\n")},
		{String, MESSAGE, []byte("你好\n")},
		{String, PING, []byte("probe")},
	}, []Frame{
		{String, []byte("0")},
		{Binary, []byte{0x04, 'h', 'e', 'l', 'l', 'o', '\n'}},
		{String, []byte("4你好\n")},
		{String, []byte("2probe")},
	},
	},
	{[]Packet{
		{Binary, MESSAGE, []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}},
		{String, MESSAGE, []byte("hello")},
		{String, CLOSE, []byte{}},
	}, []Frame{
		{Binary, []byte{4, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}},
		{String, []byte("4hello")},
		{String, []byte("1")},
	},
	},
}

func TestPacketDecoder(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	for _, test := range packetVectors {
		r := NewFakeConnReader(test.frames)
		decoder := NewPacketDecoder(r)
		var output []Packet
		for {
			ft, pt, fr, err := decoder.NextReader()
			if err != nil {
				should.Equal(io.EOF, err)
				break
			}

			b, err := io.ReadAll(fr)
			must.NoError(err)

			err = fr.Close()
			must.NoError(err)

			output = append(output, Packet{
				FType: ft,
				PType: pt,
				Data:  b,
			})
		}
		should.Equal(test.packets, output)
	}
}

func BenchmarkDecoder(b *testing.B) {
	decoder := NewPacketDecoder(NewFakeConstReader())

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, fr, err := decoder.NextReader()
		if err != nil {
			b.Error(err)
		}

		err = fr.Close()
		if err != nil {
			b.Error(err)
		}

		_, _, fr, err = decoder.NextReader()
		if err != nil {
			b.Error(err)
		}

		err = fr.Close()
		if err != nil {
			b.Error(err)
		}
	}
}

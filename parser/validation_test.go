package parser

import (
	"bytes"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type validationFrame struct {
	typ  FrameType
	data string
}
type validationFrames struct {
	frames []validationFrame
	closed int
}

func (f *validationFrames) NextReader() (FrameType, io.ReadCloser, error) {
	if len(f.frames) == 0 {
		return 0, nil, io.EOF
	}
	next := f.frames[0]
	f.frames = f.frames[1:]
	return next.typ, &validationReader{Reader: bytes.NewBufferString(next.data), close: func() { f.closed++ }}, nil
}

type validationReader struct {
	io.Reader
	close func()
}

func (r *validationReader) Close() error { r.close(); return nil }
func validationDecoder(packet string) (*Decoder, *validationFrames) {
	f := &validationFrames{frames: []validationFrame{{Text, packet}}}
	return NewDecoder(f), f
}

func TestMaxAttachments(t *testing.T) {
	for _, packet := range []string{`511-["e"]`, `5999999999999-["e"]`, `518446744073709551616-["e"]`} {
		t.Run(packet, func(t *testing.T) {
			d, _ := validationDecoder(packet)
			defer func() { _ = d.Close() }()
			start := time.Now()
			var h Header
			var event string
			err := d.DecodeHeader(&h, &event)
			require.ErrorContains(t, err, "too many attachments")
			require.Less(t, time.Since(start), time.Second)
		})
	}
	t.Run("ten", func(t *testing.T) {
		d, f := validationDecoder(`510-["e",{"_placeholder":true,"num":9}]`)
		for i := 0; i < 10; i++ {
			f.frames = append(f.frames, validationFrame{Binary, "bytes"})
		}
		var h Header
		var event string
		require.NoError(t, d.DecodeHeader(&h, &event))
		args, err := d.DecodeArgs([]reflect.Type{reflect.TypeOf(&Buffer{})})
		require.NoError(t, err)
		require.Equal(t, []byte("bytes"), args[0].Interface().(*Buffer).Data)
		require.Equal(t, 11, f.closed)
	})
}

func TestPayloadValidation(t *testing.T) {
	// Payload shapes follow socket.io-parser@3.4.5 isPayloadValid, including
	// its allowance for absent payloads and object-shaped CONNECT/ERROR data.
	for _, tc := range []struct {
		packet, event string
		valid         bool
	}{
		{`0`, "", true}, {`0{}`, "", true}, {`0[]`, "", true}, {`0null`, "", true}, {`0true`, "", false},
		{`1`, "", true}, {`1{}`, "", false}, {`1null`, "", false},
		{`2`, "", true}, {`2["e"]`, "e", true}, {`2["a,b",1]`, "a,b", true}, {`2[4,1]`, "4", true}, {`2[-0]`, "0", true}, {`2[1e400]`, "Infinity", true}, {`2[-1e400]`, "-Infinity", true}, {`2[1e-7]`, "1e-7", true}, {`2[1e21]`, "1e+21", true}, {`2[1e-6]`, "0.000001", true},
		{`2[]`, "", false}, {`2{}`, "", false}, {`2[true]`, "", false}, {`2[null]`, "", false}, {`2["e",]`, "", false}, {`2["e"]x`, "", false},
		{`3[]`, "", true}, {`3{}`, "", false}, {`3null`, "", false},
		{`4"error"`, "", true}, {`4{}`, "", true}, {`4[]`, "", true}, {`4null`, "", true}, {`4true`, "", false},
		{`50-["e"]`, "", false}, {`5-["e"]`, "", false}, {`51["e"]`, "", false},
		{`51-["e",{"_placeholder":true,"num":1}]`, "", false},
		{`51-["e",{"_placeholder":true,"num":-1}]`, "", false},
		{`51-["e",{"_placeholder":true,"num":0.5}]`, "", false},
		{`51-["e",{"_placeholder":true}]`, "", false},
	} {
		t.Run(tc.packet, func(t *testing.T) {
			d, _ := validationDecoder(tc.packet)
			defer func() { _ = d.Close() }()
			var h Header
			var event string
			err := d.DecodeHeader(&h, &event)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.event, event)
			_, err = d.DecodeArgs(nil)
			require.NoError(t, err)
		})
	}
}

func TestWrongTypeConsumesBinaryAttachments(t *testing.T) {
	d, f := validationDecoder(`51-["typed",{"_placeholder":true,"num":0}]`)
	f.frames = append(f.frames, validationFrame{Binary, "bytes"}, validationFrame{Text, `2["typed",42]`})
	var h Header
	var event string
	require.NoError(t, d.DecodeHeader(&h, &event))
	_, err := d.DecodeArgs([]reflect.Type{reflect.TypeOf(0)})
	require.Error(t, err)
	require.NoError(t, d.DecodeHeader(&h, &event))
	args, err := d.DecodeArgs([]reflect.Type{reflect.TypeOf(0)})
	require.NoError(t, err)
	require.Equal(t, 42, args[0].Interface())
	require.Equal(t, 3, f.closed)
}

func TestDecodeNullArgument(t *testing.T) {
	d, _ := validationDecoder(`2["nil",null]`)
	var h Header
	var event string
	require.NoError(t, d.DecodeHeader(&h, &event))
	args, err := d.DecodeArgs([]reflect.Type{reflect.TypeOf((*string)(nil))})
	require.NoError(t, err)
	require.True(t, args[0].IsNil())
}

package parser

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type temporaryPayloadError struct{ error }

func (temporaryPayloadError) Temporary() bool { return true }

type fakeWriterFeeder struct {
	w           io.Writer
	returnError error
	passingErr  error
}

func (f *fakeWriterFeeder) AcquirePayloadWriter() (io.Writer, error) {
	if oe, ok := f.returnError.(interface {
		error
		Temporary() bool
	}); ok && oe.Temporary() {
		f.returnError = nil
		return nil, oe
	}
	return f.w, f.returnError
}

func (f *fakeWriterFeeder) ReleasePayloadWriter(err error) error {
	f.passingErr = err
	return f.returnError
}

func TestPayloadEncoder(t *testing.T) {
	assert := assert.New(t)
	must := require.New(t)
	buf := bytes.NewBuffer(nil)
	f := &fakeWriterFeeder{
		w: buf,
	}

	for _, test := range payloadVectors {
		buf.Reset()
		e := NewPayloadEncoder(f, WithBinarySupport(test.supportBinary))

		for _, packet := range test.packets {
			fw, err := e.NextWriter(packet.ft, packet.pt)
			must.Nil(err)

			_, err = fw.Write(packet.data)
			must.Nil(err)
			must.Nil(fw.Close())
		}

		assert.Equal(test.data, buf.Bytes())
	}
}

func TestPayloadEncoderOptionsDefaultAndOverride(t *testing.T) {
	text := WithBinarySupport(false)
	for _, tc := range []struct {
		name string
		opts []PayloadEncoderOption
		want []byte
	}{
		{"default binary", nil, []byte{0, 3, 255, '4', 'h', 'i'}},
		{"text", []PayloadEncoderOption{text}, []byte("3:4hi")},
		{"last wins", []PayloadEncoderOption{WithBinarySupport(true), text}, []byte("3:4hi")},
		{"reused", []PayloadEncoderOption{text}, []byte("3:4hi")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			encoder := NewPayloadEncoder(&fakeWriterFeeder{w: &output}, tc.opts...)
			writer, err := encoder.NextWriter(String, MESSAGE)
			require.NoError(t, err)
			_, err = io.WriteString(writer, "hi")
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			require.Equal(t, tc.want, output.Bytes())
		})
	}
}

func TestPayloadEncoderBeginError(t *testing.T) {
	assert := assert.New(t)
	buf := bytes.NewBuffer(nil)
	f := &fakeWriterFeeder{
		w: buf,
	}
	e := NewPayloadEncoder(f, WithBinarySupport(true))

	buf.Reset()
	targetErr := temporaryPayloadError{errors.New("payload: paused")}
	f.returnError = targetErr

	_, err := e.NextWriter(Binary, OPEN)
	assert.Equal(targetErr, err)
}

type errorWrite struct {
	err error
}

func (f *errorWrite) Write(p []byte) (int, error) {
	return 0, f.err
}

func TestPayloadEncoderEndError(t *testing.T) {
	assert := assert.New(t)
	must := require.New(t)

	werr := errors.New("write error")
	f := &fakeWriterFeeder{
		w: &errorWrite{
			err: werr,
		},
	}
	e := NewPayloadEncoder(f, WithBinarySupport(true))

	targetErr := errors.New("error")

	fw, err := e.NextWriter(Binary, OPEN)
	must.Nil(err)

	f.returnError = targetErr

	assert.Equal(targetErr, fw.Close())
	assert.Equal(f.passingErr, werr)
}

func TestPayloadEncoderNOOP(t *testing.T) {
	assert := assert.New(t)

	payloadVectors := []struct {
		supportBinary bool
		data          []byte
	}{
		{true, []byte{0x00, 0x01, 0xff, '6'}},
		{false, []byte("1:6")},
	}

	for _, test := range payloadVectors {
		e := NewPayloadEncoder(nil, WithBinarySupport(test.supportBinary))
		assert.Equal(test.data, e.NOOP())
	}

	// NOOP should be thread-safe
	var wg sync.WaitGroup
	max := 100
	wg.Add(max)

	for i := 0; i < max; i++ {
		go func(i int) {
			defer wg.Done()
			e := NewPayloadEncoder(nil, WithBinarySupport(i&0x1 == 0))
			e.NOOP()
		}(i)
	}

	wg.Wait()
}

func BenchmarkStringEncoder(b *testing.B) {
	must := require.New(b)
	packets := []payloadVectorPacket{
		{String, OPEN, []byte{}},
		{String, MESSAGE, []byte("你好\n")},
		{String, PING, []byte("probe")},
	}
	e := NewPayloadEncoder(&fakeWriterFeeder{
		w: io.Discard,
	}, WithBinarySupport(false))

	// warm up for memory allocation
	for _, p := range packets {
		f, err := e.NextWriter(p.ft, p.pt)
		must.Nil(err)
		_, err = f.Write(p.data)
		must.Nil(err)
		err = f.Close()
		must.Nil(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, p := range packets {
			f, err := e.NextWriter(p.ft, p.pt)
			require.NoError(b, err)

			_, err = f.Write(p.data)
			require.NoError(b, err)

			err = f.Close()
			require.NoError(b, err)
		}
	}
}

func BenchmarkB64Encoder(b *testing.B) {
	must := require.New(b)
	packets := []payloadVectorPacket{
		{Binary, OPEN, []byte{}},
		{Binary, MESSAGE, []byte("你好\n")},
		{Binary, PING, []byte("probe")},
	}
	e := NewPayloadEncoder(&fakeWriterFeeder{
		w: io.Discard,
	}, WithBinarySupport(false))

	// warm up for memory allocation
	for _, p := range packets {
		f, err := e.NextWriter(p.ft, p.pt)
		must.NoError(err)

		_, err = f.Write(p.data)
		must.NoError(err)

		err = f.Close()
		must.NoError(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, p := range packets {
			f, err := e.NextWriter(p.ft, p.pt)
			require.NoError(b, err)

			_, err = f.Write(p.data)
			require.NoError(b, err)

			err = f.Close()
			require.NoError(b, err)
		}
	}
}

func BenchmarkBinaryEncoder(b *testing.B) {
	must := require.New(b)

	packets := []payloadVectorPacket{
		{String, OPEN, []byte{}},
		{Binary, MESSAGE, []byte("你好\n")},
		{String, PING, []byte("probe")},
	}
	e := NewPayloadEncoder(&fakeWriterFeeder{
		w: io.Discard,
	}, WithBinarySupport(true))

	// warm up for memory allocation
	for _, p := range packets {
		f, err := e.NextWriter(p.ft, p.pt)
		must.Nil(err)

		_, err = f.Write(p.data)
		must.Nil(err)

		err = f.Close()
		must.Nil(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, p := range packets {
			f, err := e.NextWriter(p.ft, p.pt)
			require.NoError(b, err)

			_, err = f.Write(p.data)
			require.NoError(b, err)

			err = f.Close()
			require.NoError(b, err)
		}
	}
}

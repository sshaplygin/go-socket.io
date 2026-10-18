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
		e := PayloadEncoder{
			supportBinary: test.supportBinary,
			feeder:        f,
		}

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

func TestPayloadEncoderBeginError(t *testing.T) {
	assert := assert.New(t)
	buf := bytes.NewBuffer(nil)
	f := &fakeWriterFeeder{
		w: buf,
	}
	e := PayloadEncoder{
		supportBinary: true,
		feeder:        f,
	}

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
	e := PayloadEncoder{
		supportBinary: true,
		feeder:        f,
	}

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
		e := PayloadEncoder{
			supportBinary: test.supportBinary,
		}
		assert.Equal(test.data, e.NOOP())
	}

	// NOOP should be thread-safe
	var wg sync.WaitGroup
	max := 100
	wg.Add(max)

	for i := 0; i < max; i++ {
		go func(i int) {
			defer wg.Done()
			e := PayloadEncoder{
				supportBinary: i&0x1 == 0,
			}
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
	e := PayloadEncoder{
		supportBinary: false,
		feeder: &fakeWriterFeeder{
			w: io.Discard,
		},
	}

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
	e := PayloadEncoder{
		supportBinary: false,
		feeder: &fakeWriterFeeder{
			w: io.Discard,
		},
	}

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
	e := PayloadEncoder{
		supportBinary: true,
		feeder: &fakeWriterFeeder{
			w: io.Discard,
		},
	}

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

package parser

import (
	"bytes"
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
)

type fakeReader struct {
	r io.Reader
}

func (r fakeReader) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

type fakeReaderFeeder struct {
	data          []byte
	supportBinary bool
	returnError   error
	sendError     error
	getCounter    int
	putCounter    int
}

func (f *fakeReaderFeeder) AcquirePayloadReader() (io.Reader, bool, error) {
	f.getCounter++
	return fakeReader{bytes.NewReader(f.data)}, f.supportBinary, f.returnError
}

func (f *fakeReaderFeeder) ReleasePayloadReader(err error) error {
	f.putCounter++
	f.sendError = err
	return f.returnError
}

func TestPayloadDecoder(t *testing.T) {
	assert := assert.New(t)
	must := require.New(t)

	for _, test := range payloadVectors {
		feeder := fakeReaderFeeder{
			data:          test.data,
			supportBinary: test.supportBinary,
		}
		d := NewPayloadDecoder(&feeder)
		var packets []payloadVectorPacket

		for i := 0; i < len(test.packets); i++ {
			ft, pt, fr, err := d.NextReader()
			if err != nil {
				must.Equal(io.EOF, err)
				break
			}
			data, err := io.ReadAll(fr)
			must.Nil(err)
			packet := payloadVectorPacket{
				ft:   ft,
				pt:   pt,
				data: data,
			}
			err = fr.Close()
			must.Nil(err)
			packets = append(packets, packet)
		}

		assert.Equal(test.packets, packets)
		assert.Equal(feeder.getCounter, 1)
		assert.Equal(feeder.putCounter, 1)
	}
}

func TestPayloadDecoderNextReaderError(t *testing.T) {
	assert := assert.New(t)

	feeder := fakeReaderFeeder{
		data:          []byte{0x00, 0x01, 0xff, '0'},
		supportBinary: true,
	}
	d := NewPayloadDecoder(&feeder)

	targetErr := errors.New("error")
	feeder.returnError = targetErr
	_, _, _, err := d.NextReader()
	assert.Equal(targetErr, err)
}

func BenchmarkStringDecoder(b *testing.B) {
	feeder := fakeReaderFeeder{
		data:          []byte("4:4你好\n6:2probe"),
		supportBinary: false,
	}
	d := NewPayloadDecoder(&feeder)
	buf := make([]byte, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 2; j++ {
			_, _, r, err := d.NextReader()
			require.NoError(b, err)

			_, err = r.Read(buf)
			require.NoError(b, err)

			err = r.Close()
			require.NoError(b, err)
		}
	}
}

func BenchmarkB64Decoder(b *testing.B) {
	feeder := fakeReaderFeeder{
		data:          []byte("10:b4aGVsbG8K6:2probe"),
		supportBinary: false,
	}
	d := NewPayloadDecoder(&feeder)
	buf := make([]byte, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 2; j++ {
			_, _, r, err := d.NextReader()
			require.NoError(b, err)

			_, err = r.Read(buf)
			require.NoError(b, err)

			err = r.Close()
			require.NoError(b, err)
		}
	}
}

func BenchmarkBinaryDecoder(b *testing.B) {
	feeder := fakeReaderFeeder{
		data: []byte{
			0x01, 0x07, 0xff, 0x04, 'h', 'e', 'l', 'l', 'o', '\n',
			0x00, 0x08, 0xff, '4', 0xe4, 0xbd, 0xa0, 0xe5, 0xa5, 0xbd, '\n',
			0x00, 0x06, 0xff, '2', 'p', 'r', 'o', 'b', 'e',
		},
		supportBinary: true,
	}
	d := NewPayloadDecoder(&feeder)
	buf := make([]byte, 10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 3; j++ {
			_, _, r, err := d.NextReader()
			require.NoError(b, err)

			_, err = r.Read(buf)
			require.NoError(b, err)

			err = r.Close()
			require.NoError(b, err)
		}
	}
}

// Covers G25
func TestReviewG25InvalidPayloadLengths(t *testing.T) {
	for _, data := range []string{":4", "5:4x", "18446744073709551618:4x"} {
		t.Run(data, func(t *testing.T) {
			f := &fakeReaderFeeder{data: []byte(data)}
			d := NewPayloadDecoder(f)
			_, _, r, err := d.NextReader()
			if err == nil {
				_, err = io.ReadAll(r)
				closeErr := r.Close()
				if err == nil {
					err = closeErr
				}
			}
			require.Error(t, err, "reference engine.io-parser rejects this input")
		})
	}
}

// Covers G25
func TestReviewG25ZeroLengthSkipsEmptyPacket(t *testing.T) {
	f := &fakeReaderFeeder{data: []byte("0:1:4")}
	d := NewPayloadDecoder(f)
	_, pt, r, err := d.NextReader()
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	require.Equal(t, MESSAGE, pt, "reference skips the zero-length packet and delivers MESSAGE")
}

// Malformed framing fuzz coverage complements the G25 regression tests.
func FuzzMalformedPollingPayload(f *testing.F) {
	for _, seed := range []string{":4", "5:4x", "18446744073709551618:4x", "0:1:4", "2:4é", "1:b"} {
		f.Add([]byte(seed), false)
	}
	f.Add([]byte{0, 1, 255, 52}, true)
	f.Fuzz(func(t *testing.T, data []byte, binary bool) {
		source := &singlePayloadSource{data: data, binary: binary}
		d := NewPayloadDecoder(source)
		for i := 0; i <= len(data)+1; i++ {
			_, _, r, err := d.NextReader()
			if err != nil {
				return
			}
			_, err = io.Copy(io.Discard, r)
			closeErr := r.Close()
			if err != nil || closeErr != nil {
				return
			}
		}
		t.Fatal("decoder failed to make progress")
	})
}

type singlePayloadSource struct {
	data        []byte
	binary      bool
	acquired    bool
	releasedErr error
	releases    int
}

func (s *singlePayloadSource) AcquirePayloadReader() (io.Reader, bool, error) {
	if s.acquired {
		return nil, false, io.EOF
	}
	s.acquired = true
	return bytes.NewReader(s.data), s.binary, nil
}

func (s *singlePayloadSource) ReleasePayloadReader(err error) error {
	s.releasedErr = err
	s.releases++
	return err
}

// Covers G25
// Covers G27
func TestEmptyAndTruncatedPayloadBoundaries(t *testing.T) {
	for _, data := range [][]byte{{0}, {0, 1}, {0, 1, 255}, {0, 0, 255}, {1, 255}} {
		d := NewPayloadDecoder(&singlePayloadSource{data: data, binary: true})
		_, _, _, err := d.NextReader()
		require.Error(t, err)
		require.NotEqual(t, io.EOF, err, "partial binary framing must not be treated as an empty POST")
	}
	for _, binary := range []bool{false, true} {
		data := []byte("0:")
		if binary {
			data = nil
		}
		source := &singlePayloadSource{data: data, binary: binary}
		d := NewPayloadDecoder(source)
		_, _, _, err := d.NextReader()
		require.Equal(t, io.EOF, err)
		require.Equal(t, 1, source.releases)
		require.Nil(t, source.releasedErr)
	}
}

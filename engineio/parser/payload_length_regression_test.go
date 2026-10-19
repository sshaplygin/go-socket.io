package parser

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	defer r.Close()
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

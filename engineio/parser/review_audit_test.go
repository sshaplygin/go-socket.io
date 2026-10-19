package parser

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// Covers: G25
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

// Covers: G25
func TestReviewG25ZeroLengthSkipsEmptyPacket(t *testing.T) {
	f := &fakeReaderFeeder{data: []byte("0:1:4")}
	d := NewPayloadDecoder(f)
	_, pt, r, err := d.NextReader()
	require.NoError(t, err)
	defer r.Close()
	require.Equal(t, MESSAGE, pt, "reference skips the zero-length packet and delivers MESSAGE")
}

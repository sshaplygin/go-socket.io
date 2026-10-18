package parser_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/parser"
)

type failingPayloadReader struct {
	data string
	err  error
}

func (r *failingPayloadReader) Read(b []byte) (int, error) {
	if r.data == "" {
		return 0, r.err
	}
	n := copy(b, r.data)
	r.data = r.data[n:]
	return n, nil
}

type payloadLease struct {
	readers            []io.Reader
	reader             io.Reader
	releaseErr         error
	acquires, releases int
	releasedWith       error
}

func (s *payloadLease) AcquirePayloadReader() (io.Reader, bool, error) {
	s.acquires++
	if len(s.readers) > 0 {
		r := s.readers[0]
		s.readers = s.readers[1:]
		return r, false, nil
	}
	return s.reader, false, nil
}
func (s *payloadLease) ReleasePayloadReader(err error) error {
	s.releases++
	s.releasedWith = err
	return s.releaseErr
}

func TestPayloadDecoderReleaseIsTerminalOnError(t *testing.T) {
	readErr := errors.New("payload read failed")
	releaseErr := errors.New("payload release failed")
	for _, tc := range []struct {
		name             string
		reader           func() io.Reader
		releaseErr       error
		acquisitionFails bool
		want             error
	}{
		{"body", func() io.Reader { return &failingPayloadReader{data: "2:4", err: readErr} }, nil, false, readErr},
		{"release", func() io.Reader { return strings.NewReader("1:4") }, releaseErr, false, releaseErr},
		{"release precedence", func() io.Reader { return &failingPayloadReader{data: "2:4", err: readErr} }, releaseErr, false, releaseErr},
		{"first header", func() io.Reader { return strings.NewReader("x:") }, nil, true, nil},
		{"next header", func() io.Reader { return strings.NewReader("1:4x:") }, nil, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &payloadLease{reader: tc.reader(), releaseErr: tc.releaseErr}
			decoder := parser.NewPayloadDecoder(source)
			_, _, reader, err := decoder.NextReader()
			if !tc.acquisitionFails {
				require.NoError(t, err)
				err = reader.Close()
			}
			require.Error(t, err)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.EqualError(t, err, "invalid payload")
			}
			if tc.name == "release precedence" {
				require.ErrorIs(t, source.releasedWith, readErr)
			}
			first := err
			require.Equal(t, 1, source.acquires)
			require.Equal(t, 1, source.releases)
			for i := 0; i < 2; i++ {
				_, _, reader, err = decoder.NextReader()
				require.Nil(t, reader)
				require.ErrorIs(t, err, first)
				require.ErrorIs(t, decoder.Close(), first)
				var one [1]byte
				n, readErr := decoder.Read(one[:])
				require.Zero(t, n)
				require.ErrorIs(t, readErr, first)
				require.Equal(t, 1, source.acquires, "failed decoder must not acquire a new payload")
				require.Equal(t, 1, source.releases, "released source must not be released again")
			}
		})
	}
}

func TestPayloadDecoderSuccessfulReleaseAllowsNextPayload(t *testing.T) {
	source := &payloadLease{readers: []io.Reader{strings.NewReader("2:4a"), strings.NewReader("2:4b")}}
	decoder := parser.NewPayloadDecoder(source)
	require.NoError(t, decoder.Close())
	require.Zero(t, source.acquires)
	require.Zero(t, source.releases)
	for i, want := range []string{"a", "b"} {
		_, _, reader, err := decoder.NextReader()
		require.NoError(t, err)
		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, want, string(body))
		require.NoError(t, reader.Close())
		require.NoError(t, decoder.Close())
		require.Equal(t, i+1, source.acquires)
		require.Equal(t, i+1, source.releases)
	}
}

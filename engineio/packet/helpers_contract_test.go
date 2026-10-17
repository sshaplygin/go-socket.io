package packet

import (
	"io"
	"testing"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/stretchr/testify/require"
)

func TestDiscardWriterAcceptsEncodedFrames(t *testing.T) {
	encoder := NewEncoder(&FakeDiscardWriter{})
	for _, ft := range []frame.Type{frame.String, frame.Binary} {
		writer, err := encoder.NextWriter(ft, MESSAGE)
		require.NoError(t, err)
		body := []byte("discarded body 你好\x00")
		n, err := writer.Write(body)
		require.NoError(t, err)
		require.Equal(t, len(body), n)
		require.NoError(t, writer.Close())
	}
}

func TestFakeFrameSupportsStreamingRead(t *testing.T) {
	frames := NewFakeConnWriter()
	writer, err := frames.NextWriter(frame.Binary)
	require.NoError(t, err)
	body := []byte{0, 4, 255, 'a', 'b'}
	n, err := writer.Write(body)
	require.NoError(t, err)
	require.Equal(t, len(body), n)
	reader, ok := writer.(io.Reader)
	require.True(t, ok, "fake frames support reading buffered bytes")
	prefix := make([]byte, 2)
	_, err = io.ReadFull(reader, prefix)
	require.NoError(t, err)
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, body, append(prefix, rest...))
	n, err = reader.Read(make([]byte, 1))
	require.Zero(t, n)
	require.ErrorIs(t, err, io.EOF)
	require.NoError(t, writer.Close())
}

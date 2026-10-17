package packet

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestConstantFrameEmptyRead(t *testing.T) {
	frames := NewFakeConstReader()
	_, reader, err := frames.NextReader()
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()
	require.NotPanics(t, func() {
		n, readErr := reader.Read(nil)
		require.Zero(t, n)
		require.NoError(t, readErr)
	})
	buf := make([]byte, 1)
	n, err := reader.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, MESSAGE.StringByte(), buf[0], "an empty read must not consume the frame prefix")
}

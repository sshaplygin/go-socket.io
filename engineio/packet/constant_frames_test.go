package packet

import (
	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestConstantFramesDecodeAsMessages(t *testing.T) {
	decoder := NewDecoder(NewFakeConstReader())
	for _, want := range []frame.Type{frame.String, frame.Binary, frame.String, frame.Binary} {
		ft, pt, reader, err := decoder.NextReader()
		require.NoError(t, err)
		require.Equal(t, want, ft)
		require.Equal(t, MESSAGE, pt, "frame prefixes must agree with the declared frame type")
		require.NoError(t, reader.Close())
	}
}

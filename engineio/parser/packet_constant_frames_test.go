package parser

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestConstantFramesDecodeAsMessages(t *testing.T) {
	decoder := NewPacketDecoder(NewFakeConstReader())
	for _, want := range []FrameType{String, Binary, String, Binary} {
		ft, pt, reader, err := decoder.NextReader()
		require.NoError(t, err)
		require.Equal(t, want, ft)
		require.Equal(t, MESSAGE, pt, "frame prefixes must agree with the declared frame type")
		require.NoError(t, reader.Close())
	}
}

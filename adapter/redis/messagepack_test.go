package redis

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestBroadcastRejectsMalformedContainers(t *testing.T) {
	b := &broadcast{nsp: "/", key: "socket.io#/#", uid: "local"}
	for _, data := range [][]byte{
		{0x91, 0xdf, 0x7f, 0xff, 0xff, 0xff},            // map length without entries
		{0x91, 0xdd, 0x7f, 0xff, 0xff, 0xff},            // nested array length without elements
		{0x93, 0xa1, 'x', 0xc6, 0xff, 0xff, 0xff, 0xff}, // binary length without data
		append(bytes.Repeat([]byte{0x91}, 1002), 0xc0),
	} {
		require.Error(t, b.onMessage("socket.io#/#", data))
	}
}

func TestMessagePackAcceptedValues(t *testing.T) {
	// Cover wire representations used by msgpack, including its timestamp extension.
	for _, value := range []any{
		nil, true, false, int8(-128), int16(-300), int32(-70000), int64(-1 << 40),
		uint8(255), uint16(1000), uint32(70000), uint64(1 << 40), float32(1.5), float64(3.5),
		"value", string(bytes.Repeat([]byte{'s'}, 300)), []byte{0, 255}, time.Unix(1, 2),
		[]any{nil, "value", map[string]any{"items": []any{1, true}}},
	} {
		data, err := msgpack.Marshal(value)
		require.NoError(t, err)
		require.NoError(t, validateMessagePack(data), "value: %v", value)
		require.Error(t, validateMessagePack(append(data, 0xc0)), "trailing value")
		if len(data) > 1 {
			require.Error(t, validateMessagePack(data[:len(data)-1]), "truncated value: %v", value)
		}
	}
}

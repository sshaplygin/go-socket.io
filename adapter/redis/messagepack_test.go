package redis

import (
	"bytes"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/sshaplygin/go-socket.io/adapter"
)

func TestBroadcastRejectsMalformedContainers(t *testing.T) {
	b := &broadcast{nsp: "/", key: "socket.io#/#", uid: "local"}
	for _, data := range [][]byte{
		{0x91, 0xdf, 0x7f, 0xff, 0xff, 0xff},            // map length without entries
		{0x91, 0xdd, 0x7f, 0xff, 0xff, 0xff},            // nested array length without elements
		{0x93, 0xa1, 'x', 0xc6, 0xff, 0xff, 0xff, 0xff}, // binary length without data
	} {
		require.Error(t, b.onMessage("socket.io#/#", data))
	}
}

// TestBroadcastNestingLimit checks that a well-formed broadcast nested past 1000
// levels is rejected before decoding and reaches no socket, while a nested broadcast
// within the limits is delivered.
func TestBroadcastNestingLimit(t *testing.T) {
	b := newTestRedisBroadcast(t, miniredis.RunT(t))
	c := newRedisTestConn("member")
	b.Join("room", c)
	// The envelope, its packet map and the packet's data array hold the argument at
	// depth 3, so an argument of n nested arrays puts its innermost value at 3+n.
	// The local encoder refuses such nesting, so the argument is put into the
	// decoded envelope of a shallow broadcast, as another publisher could send it.
	broadcast := func(arrays int) []byte {
		var arg interface{} = "leaf"
		for i := 0; i < arrays; i++ {
			arg = []interface{}{arg}
		}
		data, err := encodeBroadcast("peer", "/", "deep", adapter.Options{}, []interface{}{"shallow"})
		require.NoError(t, err)
		var envelope []interface{}
		require.NoError(t, msgpack.Unmarshal(data, &envelope))
		envelope[1].(map[string]interface{})["data"].([]interface{})[1] = arg
		data, err = msgpack.Marshal(envelope)
		require.NoError(t, err)
		return data
	}
	require.NoError(t, b.onMessage("socket.io#/#", broadcast(100)))
	require.Len(t, c.events, 1, "the nested broadcast was not delivered")
	<-c.events
	// 997 arrays pass the MessagePack check and are refused later by the Socket.IO
	// encoder's own nesting limit; 998 are refused before decoding.
	require.ErrorIs(t, b.onMessage("socket.io#/#", broadcast(998)), errMalformedMessagePack)
	require.Empty(t, c.events, "the broadcast past the limit was delivered")
}

// TestMessagePackNestingLimit checks the documented bound on nesting: 1000 nested
// containers are accepted and 1001 are rejected before decoding, for arrays and maps.
func TestMessagePackNestingLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		open   []byte
		levels int
	}{
		{"array", []byte{0x91}, 1000},
		{"map", []byte{0x81, 0xa1, 'k'}, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nested := func(levels int) []byte {
				return append(bytes.Repeat(tc.open, levels), 0xc0)
			}
			require.NoError(t, validateMessagePack(nested(tc.levels)))
			require.ErrorIs(t, validateMessagePack(nested(tc.levels+1)), errMalformedMessagePack)
		})
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

package parser

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPacketType(t *testing.T) {
	var packetVectors = []struct {
		b       byte
		fType   FrameType
		pType   PacketType
		strByte byte
		binByte byte
		str     string
	}{
		{0, Binary, OPEN, '0', 0, "open"},
		{1, Binary, CLOSE, '1', 1, "close"},
		{2, Binary, PING, '2', 2, "ping"},
		{3, Binary, PONG, '3', 3, "pong"},
		{4, Binary, MESSAGE, '4', 4, "message"},
		{5, Binary, UPGRADE, '5', 5, "upgrade"},
		{6, Binary, NOOP, '6', 6, "noop"},

		{'0', String, OPEN, '0', 0, "open"},
		{'1', String, CLOSE, '1', 1, "close"},
		{'2', String, PING, '2', 2, "ping"},
		{'3', String, PONG, '3', 3, "pong"},
		{'4', String, MESSAGE, '4', 4, "message"},
		{'5', String, UPGRADE, '5', 5, "upgrade"},
		{'6', String, NOOP, '6', 6, "noop"},
	}

	for i, test := range packetVectors {
		typ := ByteToPacketType(test.b, test.fType)

		require.Equal(t, test.pType, typ, fmt.Sprintf(`types not equal by case: %d`, i))

		assert.Equal(t, test.strByte, typ.StringByte(), fmt.Sprintf(`string byte not equal by case: %d`, i))
		assert.Equal(t, test.binByte, typ.BinaryByte(), fmt.Sprintf(`bytes not equal by case: %d`, i))
		assert.Equal(t, test.str, typ.String(), fmt.Sprintf(`strings not equal by case: %d`, i))
	}
}

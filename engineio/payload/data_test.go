package payload

import (
	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

type Packet struct {
	ft   frame.Type
	pt   packet.Type
	data []byte
}

var tests = []struct {
	supportBinary bool
	data          []byte
	packets       []Packet
}{
	{true, []byte{0x00, 0x01, 0xff, '0'}, []Packet{
		{frame.String, packet.OPEN, []byte{}},
	},
	},
	// In a binary payload the length counts bytes (UTF-8 for strings), as in
	// engine.io-parser 2.2.1 encodePayloadAsBinary: "4hello 你好" is 13 bytes.
	{true, []byte{0x00, 0x01, 0x03, 0xff, '4', 'h', 'e', 'l', 'l', 'o', ' ', 0xe4, 0xbd, 0xa0, 0xe5, 0xa5, 0xbd},
		[]Packet{
			{frame.String, packet.MESSAGE, []byte("hello 你好")},
		},
	},
	{true, []byte{0x01, 0x01, 0x03, 0xff, 0x04, 'h', 'e', 'l', 'l', 'o', ' ', 0xe4, 0xbd, 0xa0, 0xe5, 0xa5, 0xbd}, []Packet{
		{frame.Binary, packet.MESSAGE, []byte("hello 你好")},
	},
	},
	{true, []byte{
		0x01, 0x07, 0xff, 0x04, 'h', 'e', 'l', 'l', 'o', '\n',
		0x00, 0x08, 0xff, '4', 0xe4, 0xbd, 0xa0, 0xe5, 0xa5, 0xbd, '\n',
		0x00, 0x06, 0xff, '2', 'p', 'r', 'o', 'b', 'e',
	}, []Packet{
		{frame.Binary, packet.MESSAGE, []byte("hello\n")},
		{frame.String, packet.MESSAGE, []byte("你好\n")},
		{frame.String, packet.PING, []byte("probe")},
	},
	},
	// engine.io-parser 2.2.1 encodePayload(packets, true) of "€👍a", <Buffer ff 80>, ping "probe"
	{true, []byte{
		0x00, 0x09, 0xff, '4', 0xe2, 0x82, 0xac, 0xf0, 0x9f, 0x91, 0x8d, 'a',
		0x01, 0x03, 0xff, 0x04, 0xff, 0x80,
		0x00, 0x06, 0xff, '2', 'p', 'r', 'o', 'b', 'e',
	}, []Packet{
		{frame.String, packet.MESSAGE, []byte("€👍a")},
		{frame.Binary, packet.MESSAGE, []byte{0xff, 0x80}},
		{frame.String, packet.PING, []byte("probe")},
	},
	},
	{false, []byte("1:0"), []Packet{
		{frame.String, packet.OPEN, []byte{}},
	},
	},
	{false, []byte("9:4hello 你好"), []Packet{
		{frame.String, packet.MESSAGE, []byte("hello 你好")},
	},
	},
	{false, []byte("18:b4aGVsbG8g5L2g5aW9"), []Packet{
		{frame.Binary, packet.MESSAGE, []byte("hello 你好")},
	},
	},
	{false, []byte("10:b4aGVsbG8K4:4你好\n6:2probe"), []Packet{
		{frame.Binary, packet.MESSAGE, []byte("hello\n")},
		{frame.String, packet.MESSAGE, []byte("你好\n")},
		{frame.String, packet.PING, []byte("probe")},
	},
	},
	// ↓ is 3 bytes, JavaScript `.length` 1 See https://socket.io/docs/v4/engine-io-protocol/#from-v3-to-v4
	{false, []byte("6:412↓453:41↓"), []Packet{
		{frame.String, packet.MESSAGE, []byte("12↓45")},
		{frame.String, packet.MESSAGE, []byte("1↓")},
	},
	},
	// 🇩🇪 is 8 bytes, 2 unicode chars JavaScript `.length` 4
	{false, []byte("6:4hello6:4🇩🇪a5:41234"), []Packet{
		{frame.String, packet.MESSAGE, []byte("hello")},
		{frame.String, packet.MESSAGE, []byte("🇩🇪a")},
		{frame.String, packet.MESSAGE, []byte("1234")},
	},
	},
	// € is 3 bytes, JavaScript `.length` 1
	{false, []byte("2:4h3:4€a2:41"), []Packet{
		{frame.String, packet.MESSAGE, []byte("h")},
		{frame.String, packet.MESSAGE, []byte("€a")},
		{frame.String, packet.MESSAGE, []byte("1")},
	},
	},
	//👍 is 4 bytes, JavaScript `.length` 2
	{false, []byte("2:4h4:4👍a2:41"), []Packet{
		{frame.String, packet.MESSAGE, []byte("h")},
		{frame.String, packet.MESSAGE, []byte("👍a")},
		{frame.String, packet.MESSAGE, []byte("1")},
	},
	},
}

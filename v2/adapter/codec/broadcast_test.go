package codec_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/sshaplygin/go-socket.io/v2/adapter/codec"
	"github.com/sshaplygin/go-socket.io/v2/parser"
	"github.com/vmihailenco/msgpack/v5"
)

// pack builds a broadcast message from generic values with the MessagePack library.
func pack(t testing.TB, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	enc.UseCompactInts(true)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func okOpts() map[string]any {
	return map[string]any{"rooms": []any{}, "except": []any{}, "flags": map[string]any{}}
}

func TestBroadcastRoundtrip(t *testing.T) {
	id := uint64(7)
	timeout, yes, no := int64(500), true, false
	cases := map[string]codec.Broadcast{
		"event": {UID: "u", Packet: parser.Packet{Type: parser.Event, Namespace: "/chat", Data: json.RawMessage(`["a",1,-2,1.5,null,true,"s",{"k":[]}]`)},
			Options: codec.Options{Rooms: strs("r1", "r2"), Except: strs("x"), Flags: &codec.Flags{Volatile: &yes, Compress: &no, Timeout: &timeout}}},
		"ack with id": {UID: "u", Packet: parser.Packet{Type: parser.Ack, Namespace: "/", ID: &id, Data: json.RawMessage(`["ok"]`)},
			Options: codec.Options{Flags: &codec.Flags{}}},
		"two attachments": {UID: "u", Packet: parser.Packet{Type: parser.Event, Namespace: "/",
			Data:        json.RawMessage(`["f",{"a":{"_placeholder":true,"num":0},"b":{"_placeholder":true,"num":1}}]`),
			Attachments: [][]byte{{1}, {}}}, Options: codec.Options{Flags: &codec.Flags{}}},
		"connect without data": {UID: "u", Packet: parser.Packet{Type: parser.Connect, Namespace: "/x"}, Options: codec.Options{Flags: &codec.Flags{}}},
		"large ints": {UID: "u", Packet: parser.Packet{Type: parser.Event, Namespace: "/",
			Data: json.RawMessage(`["n",18446744073709551615,-9223372036854775808,4294967296,65536,256,128,-129,-33]`)},
			Options: codec.Options{Flags: &codec.Flags{}}},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			wire, err := codec.EncodeBroadcast(in)
			if err != nil {
				t.Fatal(err)
			}
			out, err := codec.DecodeBroadcast(wire, codec.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			again, err := codec.EncodeBroadcast(out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(wire, again) {
				t.Errorf("encoding is not stable\n%q\n%q", wire, again)
			}
			if out.Packet.Data != nil && in.Packet.Data != nil {
				// Compare the JSON values, not the text.
				var a, b any
				if err := json.Unmarshal(in.Packet.Data, &a); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(out.Packet.Data, &b); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(a, b) {
					t.Errorf("data %s -> %s", in.Packet.Data, out.Packet.Data)
				}
				out.Packet.Data, in.Packet.Data = nil, nil
			}
			if len(in.Packet.Attachments) == 0 {
				in.Packet.Attachments = nil
			}
			in.Options.Rooms, in.Options.Except = orEmpty(in.Options.Rooms), orEmpty(in.Options.Except)
			if !reflect.DeepEqual(in, out) {
				t.Errorf("roundtrip\n in %+v\nout %+v", in, out)
			}
		})
	}
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func TestEncodeBroadcastBytes(t *testing.T) {
	// The expected bytes are what notepack.io writes for the same values: smallest
	// integer encodings, str8 from 32 bytes, doubles for non-integers.
	b := codec.Broadcast{UID: "u", Packet: parser.Packet{Type: parser.Event,
		Data: json.RawMessage(`["e",300,-200,0.5,3.0,` + `"` + strings.Repeat("x", 40) + `"]`)}}
	got, err := codec.EncodeBroadcast(b)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x93, 0xa1, 'u', 0x83, 0xa4, 't', 'y', 'p', 'e', 0x02, 0xa4, 'd', 'a', 't', 'a', 0x96,
		0xa1, 'e', 0xcd, 0x01, 0x2c, 0xd1, 0xff, 0x38, 0xcb, 0x3f, 0xe0, 0, 0, 0, 0, 0, 0, 0x03, 0xd9, 40}
	want = append(want, bytes.Repeat([]byte{'x'}, 40)...)
	want = append(want, 0xa3, 'n', 's', 'p', 0xa1, '/', 0x83, 0xa5, 'r', 'o', 'o', 'm', 's', 0x90, 0xa6, 'e', 'x', 'c', 'e', 'p', 't', 0x90, 0xa5, 'f', 'l', 'a', 'g', 's', 0x80)
	if !bytes.Equal(got, want) {
		t.Errorf("got  %x\nwant %x", got, want)
	}
}

func TestEncodeBroadcastDefaults(t *testing.T) {
	// An empty namespace is "/", nil room lists are arrays, HTML characters are not escaped.
	wire, err := codec.EncodeBroadcast(codec.Broadcast{UID: "u", Packet: parser.Packet{Type: parser.Event, Data: json.RawMessage(`["<&>"]`)}})
	if err != nil {
		t.Fatal(err)
	}
	var v []any
	if err := msgpack.Unmarshal(wire, &v); err != nil {
		t.Fatal(err)
	}
	pkt := v[1].(map[string]any)
	opts := v[2].(map[string]any)
	if pkt["nsp"] != "/" || !reflect.DeepEqual(pkt["data"], []any{"<&>"}) ||
		!reflect.DeepEqual(opts["rooms"], []any{}) || !reflect.DeepEqual(opts["except"], []any{}) || !reflect.DeepEqual(opts["flags"], map[string]any{}) {
		t.Errorf("%v", v)
	}
}

func TestEncodeBroadcastRejects(t *testing.T) {
	ev := func(data string, atts ...[]byte) codec.Broadcast {
		return codec.Broadcast{UID: "u", Packet: parser.Packet{Type: parser.Event, Data: json.RawMessage(data), Attachments: atts}}
	}
	deep := strings.Repeat("[", 2000) + strings.Repeat("]", 2000)
	cases := map[string]codec.Broadcast{
		"empty uid":                {Packet: parser.Packet{Type: parser.Connect}},
		"type out of range":        {UID: "u", Packet: parser.Packet{Type: parser.ConnectError + 1}},
		"event without data":       {UID: "u", Packet: parser.Packet{Type: parser.Event}},
		"event with object data":   ev(`{"a":1}`),
		"invalid JSON":             ev(`["a",`),
		"trailing JSON":            ev(`["a"] []`),
		"placeholder out of range": ev(`["a",{"_placeholder":true,"num":0}]`),
		"placeholder twice":        ev(`["a",{"_placeholder":true,"num":0},{"_placeholder":true,"num":0}]`, []byte{1}),
		"attachment unreferenced":  ev(`["a"]`, []byte{1}),
		"too deep":                 ev(deep),
		"number out of range":      ev(`["a",1e999]`),
	}
	for name, b := range cases {
		if _, err := codec.EncodeBroadcast(b); err == nil || (!errors.Is(err, codec.ErrInvalid) && !errors.Is(err, codec.ErrLimit)) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func packet(extra map[string]any) map[string]any {
	m := map[string]any{"type": 2, "data": []any{"e"}, "nsp": "/"}
	for k, v := range extra {
		if v == nil {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	return m
}

func TestDecodeBroadcastMapping(t *testing.T) {
	// Packet types 5 and 6 map to the base types; an absent nsp is "/"; an id is kept.
	b, err := codec.DecodeBroadcast(pack(t, []any{"u", packet(map[string]any{"type": 5, "nsp": nil, "id": 9}), okOpts()}), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Packet.Type != parser.Event || b.Packet.Namespace != "/" || b.Packet.ID == nil || *b.Packet.ID != 9 {
		t.Errorf("%+v", b.Packet)
	}
	b, err = codec.DecodeBroadcast(pack(t, []any{"u", packet(map[string]any{"type": 6, "data": []any{}}), okOpts()}), codec.Limits{})
	if err != nil || b.Packet.Type != parser.Ack {
		t.Errorf("%+v %v", b, err)
	}
	// Opts without flags, as a different peer might send them.
	b, err = codec.DecodeBroadcast(pack(t, []any{"u", packet(nil), map[string]any{"rooms": []any{"r"}, "except": []any{}}}), codec.Limits{})
	if err != nil || b.Options.Flags != nil || !reflect.DeepEqual(b.Options.Rooms, strs("r")) {
		t.Errorf("%+v %v", b, err)
	}
	// Unknown keys are ignored; an attachment-free packet has nil Attachments.
	b, err = codec.DecodeBroadcast(pack(t, []any{"u", packet(map[string]any{"options": map[string]any{"compress": true}}), okOpts()}), codec.Limits{})
	if err != nil || b.Packet.Attachments != nil {
		t.Errorf("%+v %v", b, err)
	}
}

func TestDecodeBroadcastMalformed(t *testing.T) {
	bigStr := strings.Repeat("a", 300)
	cases := map[string][]byte{
		"empty":                    nil,
		"not an array":             pack(t, map[string]any{"a": 1}),
		"two elements":             pack(t, []any{"u", packet(nil)}),
		"four elements":            pack(t, []any{"u", packet(nil), okOpts(), 1}),
		"uid not a string":         pack(t, []any{1, packet(nil), okOpts()}),
		"empty uid":                pack(t, []any{"", packet(nil), okOpts()}),
		"packet not a map":         pack(t, []any{"u", []any{1}, okOpts()}),
		"packet without type":      pack(t, []any{"u", packet(map[string]any{"type": nil}), okOpts()}),
		"type 7":                   pack(t, []any{"u", packet(map[string]any{"type": 7}), okOpts()}),
		"negative type":            pack(t, []any{"u", packet(map[string]any{"type": -1}), okOpts()}),
		"fractional type":          pack(t, []any{"u", packet(map[string]any{"type": 2.5}), okOpts()}),
		"nsp not a string":         pack(t, []any{"u", packet(map[string]any{"nsp": 1}), okOpts()}),
		"event data not an array":  pack(t, []any{"u", packet(map[string]any{"data": "x"}), okOpts()}),
		"event without data":       pack(t, []any{"u", packet(map[string]any{"data": nil}), okOpts()}),
		"negative id":              pack(t, []any{"u", packet(map[string]any{"id": -1}), okOpts()}),
		"opts not a map":           pack(t, []any{"u", packet(nil), []any{}}),
		"rooms not an array":       pack(t, []any{"u", packet(nil), map[string]any{"rooms": "r"}}),
		"numeric room":             pack(t, []any{"u", packet(nil), map[string]any{"rooms": []any{1}}}),
		"binary in opts":           pack(t, []any{"u", packet(nil), map[string]any{"rooms": []any{}, "flags": map[string]any{"x": []byte{1}}}}),
		"binary key":               {0x93, 0xa1, 'u', 0x81, 0xc4, 0x01, 0x01, 0x01, 0x80},
		"integer key":              {0x93, 0xa1, 'u', 0x81, 0x01, 0x01, 0x80},
		"extension type":           {0x93, 0xa1, 'u', 0x81, 0xa4, 'd', 'a', 't', 'a', 0xd4, 0x01, 0x00, 0x80},
		"reserved code":            {0x93, 0xa1, 'u', 0xc1},
		"invalid UTF-8 string":     {0x93, 0xa2, 0xff, 0xfe, 0x80, 0x80},
		"NaN":                      {0x93, 0xa1, 'u', 0x81, 0xa4, 'd', 'a', 't', 'a', 0xcb, 0x7f, 0xf8, 0, 0, 0, 0, 0, 0, 0x80},
		"+Inf float32":             {0x93, 0xa1, 'u', 0x81, 0xa4, 'd', 'a', 't', 'a', 0xca, 0x7f, 0x80, 0, 0, 0x80},
		"truncated":                pack(t, []any{"u", packet(nil), okOpts()})[:12],
		"trailing byte":            append(pack(t, []any{"u", packet(nil), okOpts()}), 0x00),
		"declared long string":     {0x93, 0xdb, 0xff, 0xff, 0xff, 0xff},
		"declared long binary":     {0x93, 0xa1, 'u', 0x81, 0xa4, 'd', 'a', 't', 'a', 0xc6, 0xff, 0xff, 0xff, 0xff},
		"declared long array":      {0x93, 0xa1, 'u', 0x81, 0xa4, 'd', 'a', 't', 'a', 0xdd, 0xff, 0xff, 0xff, 0xff},
		"declared long map":        {0x93, 0xa1, 'u', 0xdf, 0xff, 0xff, 0xff, 0xff},
		"long string, short input": append([]byte{0x93, 0xda, 0x01, 0x2c}, bigStr[:10]...),
	}
	for name, msg := range cases {
		_, err := codec.DecodeBroadcast(msg, codec.Limits{})
		if !errors.Is(err, codec.ErrMalformed) {
			t.Errorf("%s: want ErrMalformed, got %v", name, err)
		}
	}
}

func TestDecodeBroadcastLimits(t *testing.T) {
	msg := pack(t, []any{"u", packet(map[string]any{"data": []any{"e", []byte{1}, []byte{2}}}), okOpts()})
	if _, err := codec.DecodeBroadcast(msg, codec.Limits{}); err != nil {
		t.Fatal(err)
	}
	if _, err := codec.DecodeBroadcast(msg, codec.Limits{MaxMessageBytes: len(msg) - 1}); !errors.Is(err, codec.ErrLimit) {
		t.Errorf("size: %v", err)
	}
	if _, err := codec.DecodeBroadcast(msg, codec.Limits{MaxMessageBytes: len(msg)}); err != nil {
		t.Errorf("size at the limit: %v", err)
	}
	if _, err := codec.DecodeBroadcast(msg, codec.Limits{MaxAttachments: 1}); !errors.Is(err, codec.ErrLimit) {
		t.Errorf("attachments: %v", err)
	}
	if _, err := codec.DecodeBroadcast(msg, codec.Limits{MaxAttachments: 2}); err != nil {
		t.Errorf("attachments at the limit: %v", err)
	}
	if _, err := codec.DecodeBroadcast(msg, codec.Limits{MaxDepth: 2}); !errors.Is(err, codec.ErrLimit) {
		t.Errorf("depth: %v", err)
	}
	if _, err := codec.DecodeBroadcast(msg, codec.Limits{MaxDepth: 3}); err != nil {
		t.Errorf("depth at the limit: %v", err)
	}
	if _, err := codec.DecodeBroadcast(msg, codec.Limits{MaxDepth: -1}); !errors.Is(err, codec.ErrInvalid) {
		t.Errorf("negative limit: %v", err)
	}

	// MaxDepth is capped: a limit above the ceiling is refused, so that no caller
	// can configure a recursion deep enough to overflow the stack.
	if _, err := codec.DecodeBroadcast(msg, codec.Limits{MaxDepth: codec.MaxDepthCeiling}); err != nil {
		t.Errorf("depth at the ceiling: %v", err)
	}
	if _, err := codec.DecodeBroadcast(msg, codec.Limits{MaxDepth: codec.MaxDepthCeiling + 1}); !errors.Is(err, codec.ErrInvalid) {
		t.Errorf("depth above the ceiling: %v", err)
	}
	// 4M nested arrays took the process down with a stack overflow at MaxDepth 1<<30.
	huge := append([]byte{0x93, 0xa1, 'u', 0x81, 0xa4, 'd', 'a', 't', 'a'}, bytes.Repeat([]byte{0x91}, 4<<20)...)
	lim := codec.Limits{MaxMessageBytes: 8 << 20, MaxDepth: 1 << 30}
	if _, err := codec.DecodeBroadcast(huge, lim); !errors.Is(err, codec.ErrInvalid) {
		t.Errorf("huge MaxDepth on deep nesting: %v", err)
	}
	lim.MaxDepth = codec.MaxDepthCeiling
	if _, err := codec.DecodeBroadcast(huge, lim); !errors.Is(err, codec.ErrLimit) {
		t.Errorf("deep nesting at the ceiling: %v", err)
	}

	// A 100000-deep nesting needs 100000 bytes: it must be refused, not recursed into.
	deep := append([]byte{0x93, 0xa1, 'u', 0x81, 0xa4, 'd', 'a', 't', 'a'}, bytes.Repeat([]byte{0x91}, 100000)...)
	if _, err := codec.DecodeBroadcast(deep, codec.Limits{}); !errors.Is(err, codec.ErrLimit) {
		t.Errorf("deep nesting: %v", err)
	}
}

func TestDecodeBroadcastNumbers(t *testing.T) {
	data := []any{"n", uint64(math.MaxUint64), int64(math.MinInt64), int8(-33), float32(0.5), 0.1, 1e300, math.Copysign(0, -1)}
	b, err := codec.DecodeBroadcast(pack(t, []any{"u", packet(map[string]any{"data": data}), okOpts()}), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	want := `["n",18446744073709551615,-9223372036854775808,-33,0.5,0.1,1e+300,-0]`
	if string(b.Packet.Data) != want {
		t.Errorf("got  %s\nwant %s", b.Packet.Data, want)
	}
	if !json.Valid(b.Packet.Data) {
		t.Error("invalid JSON")
	}
}

func TestDecodeBroadcastResultIsOwned(t *testing.T) {
	msg := pack(t, []any{"u", packet(map[string]any{"data": []any{"e", []byte{1, 2, 3}}}), map[string]any{"rooms": []any{"room"}, "except": []any{}}})
	b, err := codec.DecodeBroadcast(msg, codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range msg {
		msg[i] = 0xff
	}
	if b.UID != "u" || string(b.Packet.Data) != `["e",{"_placeholder":true,"num":0}]` ||
		!bytes.Equal(b.Packet.Attachments[0], []byte{1, 2, 3}) || b.Options.Rooms[0] != "room" {
		t.Errorf("decoded value aliases the input: %+v", b)
	}
}

func TestEncodeBroadcastDoesNotRetainInput(t *testing.T) {
	att := []byte{1, 2, 3}
	b := codec.Broadcast{UID: "u", Packet: parser.Packet{Type: parser.Event,
		Data: json.RawMessage(`["e",{"_placeholder":true,"num":0}]`), Attachments: [][]byte{att}}}
	wire, err := codec.EncodeBroadcast(b)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := append([]byte(nil), wire...)
	att[0] = 9
	if !bytes.Equal(wire, snapshot) {
		t.Error("the encoded message aliases an attachment")
	}
}

func TestDecodeBroadcastRejectsPlaceholderLookalike(t *testing.T) {
	// A map shaped like {"_placeholder":true,"num":N} would read back as a binary
	// placeholder; Node never sends one, so it is refused rather than guessed at.
	for _, m := range []map[string]any{
		{"_placeholder": true, "num": 0},
		{"num": 3, "_placeholder": true},
	} {
		msg := pack(t, []any{"u", packet(map[string]any{"data": []any{"e", m}}), okOpts()})
		if _, err := codec.DecodeBroadcast(msg, codec.Limits{}); !errors.Is(err, codec.ErrMalformed) {
			t.Errorf("%v: %v", m, err)
		}
	}
	// Near misses are ordinary maps.
	for _, m := range []map[string]any{
		{"_placeholder": true, "num": 1.5},
		{"_placeholder": false, "num": 0},
		{"_placeholder": true, "num": 0, "x": 1},
		{"_placeholder": true},
	} {
		msg := pack(t, []any{"u", packet(map[string]any{"data": []any{"e", m}}), okOpts()})
		if _, err := codec.DecodeBroadcast(msg, codec.Limits{}); err != nil {
			t.Errorf("%v: %v", m, err)
		}
	}
}

func TestDecodeBroadcastAllWidths(t *testing.T) {
	// Every integer width, string, binary, array and map header form, written without
	// the compact encoding so that the wide codes are used.
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	long := strings.Repeat("é", 200) // 400 bytes: str16
	steps := []func() error{
		func() error { return enc.EncodeArrayLen(3) },
		func() error { return enc.EncodeString("u") },
		func() error { return enc.EncodeMapLen(3) },
		func() error { return enc.EncodeString("type") },
		func() error { return enc.EncodeUint8(2) },
		func() error { return enc.EncodeString("nsp") },
		func() error { return enc.EncodeString("/") },
		func() error { return enc.EncodeString("data") },
		func() error { return enc.EncodeArrayLen(20) }, // array16: 16 and more elements
		func() error { return enc.EncodeString("e") },
		func() error { return enc.EncodeInt8(-5) },
		func() error { return enc.EncodeInt16(-300) },
		func() error { return enc.EncodeInt32(-70000) },
		func() error { return enc.EncodeInt64(-5000000000) },
		func() error { return enc.EncodeUint8(200) },
		func() error { return enc.EncodeUint16(60000) },
		func() error { return enc.EncodeUint32(4000000000) },
		func() error { return enc.EncodeUint64(5000000000) },
		func() error { return enc.EncodeString("a\n\r\t\x01\"\\<&>") },
		func() error { return enc.EncodeString(strings.Repeat("x", 40)) }, // str8
		func() error { return enc.EncodeString(long) },
		func() error { return enc.EncodeBytes(bytes.Repeat([]byte{1}, 300)) }, // bin16
		func() error { return enc.EncodeNil() },
		func() error { return enc.EncodeBool(true) },
		func() error { return enc.EncodeBool(false) },
		func() error { return enc.EncodeFloat32(1.5) },
		func() error { return enc.EncodeFloat64(2.5) },
		func() error { return enc.EncodeMapLen(1) },
		func() error { return enc.EncodeString("k") },
		func() error { return enc.EncodeArrayLen(0) },
		func() error { return enc.EncodeMapLen(0) },
	}
	for _, s := range steps {
		if err := s(); err != nil {
			t.Fatal(err)
		}
	}
	buf.Write(pack(t, map[string]any{"rooms": []any{}, "except": []any{}, "flags": map[string]any{}}))
	b, err := codec.DecodeBroadcast(buf.Bytes(), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	var data []any
	if err := json.Unmarshal(b.Packet.Data, &data); err != nil {
		t.Fatalf("%v: %s", err, b.Packet.Data)
	}
	if data[2] != float64(-300) || data[7] != float64(4000000000) || data[9] != "a\n\r\t\x01\"\\<&>" || data[12] == nil || len(b.Packet.Attachments[0]) != 300 {
		t.Errorf("%s", b.Packet.Data)
	}
	if !strings.Contains(string(b.Packet.Data), `"a\n\r\t\u0001\"\\<&>"`) {
		t.Errorf("escaping: %s", b.Packet.Data)
	}
}

func TestDecodeBroadcastRejectsOrphanAttachment(t *testing.T) {
	// A binary value that does not end up as a placeholder in the packet data would
	// decode into an attachment the encoder refuses, so the decoder refuses it first.
	for name, msg := range map[string]string{
		// binary under a key the packet does not use
		"unknown key": "\x93\xaa0000000000\x83\xa4type\x00\xa50000\x00\xc4\x00\xa3nsp\xa1/\x80",
		// the second "data" key replaces the first, together with its placeholder
		"duplicate data key": "\x93\xaa0000000000\x84\xa4type\x02\xa4data\x92\xa1a\xc4\x01x\xa4data\x91\xa1a\xa3nsp\xa1/\x80",
	} {
		if _, err := codec.DecodeBroadcast([]byte(msg), codec.Limits{}); !errors.Is(err, codec.ErrMalformed) {
			t.Errorf("%s: want ErrMalformed, got %v", name, err)
		}
	}
}

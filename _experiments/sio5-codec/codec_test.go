package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func id(n uint64) *uint64 { return &n }
func TestEnvelopes(t *testing.T) {
	cases := []struct {
		wire string
		p    Packet
	}{
		{"0", Packet{Type: Connect, Namespace: "/"}},
		{`0/admin,{"token":"秘密"}`, Packet{Type: Connect, Namespace: "/admin", Data: json.RawMessage(`{"token":"秘密"}`)}},
		{"1/admin,", Packet{Type: Disconnect, Namespace: "/admin"}},
		{`2/chat,12["message",{"n":1},null]`, Packet{Type: Event, Namespace: "/chat", ID: id(12), Data: json.RawMessage(`["message",{"n":1},null]`)}},
		{`39007199254740991[]`, Packet{Type: Ack, Namespace: "/", ID: id(MaxID), Data: json.RawMessage(`[]`)}},
		{`4{"message":"denied","data":{"code":403}}`, Packet{Type: ConnectError, Namespace: "/", Data: json.RawMessage(`{"message":"denied","data":{"code":403}}`)}},
		{`4"denied"`, Packet{Type: ConnectError, Namespace: "/", Data: json.RawMessage(`"denied"`)}},
		{`51-/bin,0["file",{"_placeholder":true,"num":0}]`, Packet{Type: BinaryEvent, Namespace: "/bin", ID: id(0), Attachments: 1, Data: json.RawMessage(`["file",{"_placeholder":true,"num":0}]`)}},
		{`61-2[{"_placeholder":true,"num":0}]`, Packet{Type: BinaryAck, Namespace: "/", ID: id(2), Attachments: 1, Data: json.RawMessage(`[{"_placeholder":true,"num":0}]`)}},
		{`2[42,"numeric event"]`, Packet{Type: Event, Namespace: "/", Data: json.RawMessage(`[42,"numeric event"]`)}},
	}
	for _, tc := range cases {
		t.Run(tc.wire, func(t *testing.T) {
			got, err := Decode([]byte(tc.wire), Limits{})
			if err != nil || !reflect.DeepEqual(got, tc.p) {
				t.Fatalf("Decode = %#v, %v", got, err)
			}
			encoded, err := Encode(tc.p, Limits{})
			if err != nil || string(encoded) != tc.wire {
				t.Fatalf("Encode=%s %v", encoded, err)
			}
		})
	}
}
func TestMalformed(t *testing.T) {
	cases := []string{"", "7", "0null", "0[]", "1{}", "2", "3", "4", "2[]", "2[true]", `2["connect"]`, `2["disconnecting"]`, `3{}`, `4[]`, `2["x"]garbage`, "5-[]", `50-["x"]`, `5+1-["x"]`, `51.0-["x"]`, `51e0-["x"]`, `599999999999999999999999-["x"]`, `51-["x",{"_placeholder":true,"num":1}]`, `51-["x",{"_placeholder":true,"num":0.5}]`, `51-["x",{"_placeholder":true,"num":"0"}]`, `51-["x",{"_placeholder":true}]`, `51-["x",{"_placeholder":true,"num":-1}]`, `2/room`, `39007199254740992[]`, `3999999999999999999999[]`, `2["x",`, "2[\"\xff\"]", "1/a\n,", "2/room,[null]"}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			p, err := Decode([]byte(body), Limits{})
			if err == nil || !reflect.DeepEqual(p, Packet{}) {
				t.Fatalf("got partial/success %#v %v", p, err)
			}
		})
	}
}
func TestLimitsAndOwnership(t *testing.T) {
	raw := []byte(`51-["x",{"_placeholder":true,"num":0}]`)
	bin := []byte{0, 255, 4}
	g, err := DecodeGroup(raw, [][]byte{bin}, Limits{MaxBytes: len(raw) + len(bin)})
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = '0'
	bin[0] = 9
	if g.Packet.Type != BinaryEvent || g.Attachments[0][0] != 0 {
		t.Fatal("aliased input")
	}
	encoded, attachments, err := EncodeGroup(g, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	encoded[0] = '0'
	attachments[0][0] = 8
	if g.Packet.Type != BinaryEvent || g.Attachments[0][0] != 0 {
		t.Fatal("aliased output")
	}
	_, err = DecodeGroup([]byte(`51-["x",{"_placeholder":true,"num":0}]`), [][]byte{bin}, Limits{MaxBytes: len(raw) + len(bin) - 1})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	_, err = DecodeGroup([]byte(`51-["x"]`), nil, Limits{})
	if !errors.Is(err, ErrAttachments) {
		t.Fatal(err)
	}
	_, err = DecodeGroup([]byte(`2["x"]`), [][]byte{{}}, Limits{})
	if !errors.Is(err, ErrAttachments) {
		t.Fatal(err)
	}
	_, err = Decode([]byte(`565-["x"]`), Limits{})
	if !errors.Is(err, ErrTooManyAttachments) {
		t.Fatal(err)
	}
	_, err = Decode([]byte(`2["x",[]]`), Limits{MaxDepth: 1})
	if !errors.Is(err, ErrDepth) {
		t.Fatal(err)
	}
	_, err = Decode([]byte(`2["x",[]]`), Limits{MaxDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []Limits{{MaxBytes: -1}, {MaxAttachments: -1}, {MaxDepth: -1}} {
		if _, err := Decode([]byte("0"), l); !errors.Is(err, ErrLimit) {
			t.Fatal(err)
		}
		if _, err := Encode(Packet{}, l); !errors.Is(err, ErrLimit) {
			t.Fatal(err)
		}
	}
}
func TestReconstruct(t *testing.T) {
	g := Group{Packet: Packet{Type: BinaryEvent, Attachments: 1, Data: json.RawMessage(`["x",{"nested":[{"_placeholder":true,"num":0}]},{"_placeholder":true,"num":0},9007199254740993]`)}, Attachments: [][]byte{{0, 255}}}
	v, err := Reconstruct(g, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	args := v.([]any)
	b := args[1].(map[string]any)["nested"].([]any)[0].([]byte)
	if !bytes.Equal(b, []byte{0, 255}) || args[3] != json.Number("9007199254740993") {
		t.Fatalf("%#v", args)
	}
	b[0] = 8
	if g.Attachments[0][0] != 0 {
		t.Fatal("reconstruction aliased caller")
	}
	if args[2].([]byte)[0] != 8 {
		t.Fatal("duplicate references should share owned buffer")
	}
	ordinary := Group{Packet: Packet{Type: Event, Data: json.RawMessage(`["x",{"_placeholder":true,"num":99}]`)}}
	if _, err = Reconstruct(ordinary, Limits{}); err != nil {
		t.Fatal(err)
	}
}
func TestEncodeInvalid(t *testing.T) {
	for _, p := range []Packet{{Type: 7}, {Namespace: "bad"}, {Namespace: "/a,b"}, {ID: id(MaxID + 1)}, {Attachments: 1}, {Type: BinaryAck}, {Type: Event, Data: json.RawMessage(`[]`)}, {Type: Connect, Data: json.RawMessage(`{`)}} {
		if _, err := Encode(p, Limits{}); err == nil {
			t.Fatalf("accepted %#v", p)
		}
	}
	if _, err := Encode(Packet{Type: Connect, Data: json.RawMessage(`{}`)}, Limits{MaxBytes: 2}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err := Encode(Packet{Namespace: "/" + strings.Repeat("a", 10)}, Limits{MaxBytes: 2}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
}
func FuzzEnvelope(f *testing.F) {
	for _, b := range []string{"0", `2["x"]`, `51-["x",{"_placeholder":true,"num":0}]`, `3[]`, ""} {
		f.Add([]byte(b))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		l := Limits{MaxBytes: 4096, MaxAttachments: 8, MaxDepth: 16}
		p, err := Decode(b, l)
		if err != nil {
			return
		}
		out, err := Encode(p, l)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Decode(out, l)
		if err != nil || !reflect.DeepEqual(p, again) {
			t.Fatalf("roundtrip: %v", err)
		}
	})
}
func FuzzGroup(f *testing.F) {
	f.Add([]byte(`51-["x",{"_placeholder":true,"num":0}]`), []byte{0, 255})
	f.Fuzz(func(t *testing.T, b, attachment []byte) {
		l := Limits{MaxBytes: 4096, MaxAttachments: 8, MaxDepth: 16}
		g, err := DecodeGroup(b, [][]byte{attachment}, l)
		if err != nil {
			return
		}
		if _, err := Reconstruct(g, l); err != nil {
			t.Fatal(err)
		}
	})
}

package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func raws(s ...string) []json.RawMessage {
	out := make([]json.RawMessage, len(s))
	for i, v := range s {
		out[i] = json.RawMessage(v)
	}
	return out
}

func TestEventAndAckPackets(t *testing.T) {
	args := Arguments{Values: raws(`{"a":1}`, ` "two" `, `{"_placeholder":true,"num":0}`), Attachments: [][]byte{{9, 8}}}
	p, err := EventPacket("/chat", id(4), "msg", args)
	if err != nil {
		t.Fatal(err)
	}
	if p.Type != Event || p.Namespace != "/chat" || *p.ID != 4 || len(p.Attachments) != 1 {
		t.Fatalf("%#v", p)
	}
	if string(p.Data) != `["msg",{"a":1}, "two" ,{"_placeholder":true,"num":0}]` {
		t.Fatalf("data %s", p.Data)
	}
	text, atts, err := Encode(p, Limits{})
	if err != nil || string(text) != `51-/chat,4["msg",{"a":1}, "two" ,{"_placeholder":true,"num":0}]` {
		t.Fatalf("%s %v", text, err)
	}
	back, err := Decode(text, atts, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	name, got, err := EventArguments(back)
	if err != nil || name != "msg" || !reflect.DeepEqual(got.Attachments, args.Attachments) || len(got.Values) != 3 || string(got.Values[1]) != `"two"` {
		t.Fatalf("%q %#v %v", name, got, err)
	}

	// The builders copy: mutating the inputs afterwards changes nothing.
	args.Attachments[0][0] = 0
	if p.Attachments[0][0] != 9 {
		t.Fatal("EventPacket aliased the attachments")
	}
	ack, err := AckPacket("", 0, Arguments{Values: raws(`null`, `1`)})
	if err != nil || ack.Type != Ack || ack.ID == nil || *ack.ID != 0 || string(ack.Data) != `[null,1]` {
		t.Fatalf("%#v %v", ack, err)
	}
	a, err := AckArguments(ack)
	if err != nil || len(a.Values) != 2 || a.Attachments != nil {
		t.Fatalf("%#v %v", a, err)
	}
}

func TestPacketBuildersReject(t *testing.T) {
	if _, err := EventPacket("/", nil, "x", Arguments{Values: raws(`1,2`)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("injected value: %v", err)
	}
	if _, err := EventPacket("/", nil, "x", Arguments{Values: raws(``)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty value: %v", err)
	}
	p, err := EventPacket("/", nil, "connect", Arguments{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Encode(p, Limits{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reserved name: %v", err)
	}
	p, err = EventPacket("/", nil, "x", Arguments{Values: raws(`{"_placeholder":true,"num":1}`), Attachments: [][]byte{{}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Encode(p, Limits{}); !errors.Is(err, ErrAttachments) {
		t.Fatalf("dangling placeholder: %v", err)
	}
}

func TestSplitRejects(t *testing.T) {
	for _, p := range []Packet{
		{Type: Ack, Data: json.RawMessage(`["x"]`)},
		{Type: Event, Data: json.RawMessage(`[]`)},
		{Type: Event, Data: json.RawMessage(`[null]`)},
		{Type: Event, Data: json.RawMessage(`{}`)},
		{Type: Event},
	} {
		if _, _, err := EventArguments(p); !errors.Is(err, ErrInvalid) {
			t.Errorf("EventArguments(%#v) = %v", p, err)
		}
	}
	if _, err := AckArguments(Packet{Type: Event, Data: json.RawMessage(`[]`)}); !errors.Is(err, ErrInvalid) {
		t.Error(err)
	}
	if _, err := AckArguments(Packet{Type: Ack, Data: json.RawMessage(`null`)}); !errors.Is(err, ErrInvalid) {
		t.Error(err)
	}
	name, _, err := EventArguments(Packet{Type: Event, Data: json.RawMessage(`[42,"v"]`)})
	if err != nil || name != "42" {
		t.Errorf("numeric name = %q, %v", name, err)
	}
}

func TestSplitOwnership(t *testing.T) {
	p := Packet{Type: Event, Data: json.RawMessage(`["x",{"k":1}]`), Attachments: [][]byte{{1}}}
	_, a, err := EventArguments(p)
	if err != nil {
		t.Fatal(err)
	}
	a.Values[0][2] = 'Z'
	a.Attachments[0][0] = 7
	if string(p.Data) != `["x",{"k":1}]` || p.Attachments[0][0] != 1 {
		t.Fatalf("EventArguments result aliases the packet: %s %v", p.Data, p.Attachments)
	}
}

func TestValidate(t *testing.T) {
	ph := func(n int) string { return string(Placeholder(n)) }
	cases := []struct {
		name string
		a    Arguments
		l    Limits
		want error
	}{
		{"ok", Arguments{Values: raws(`1`, ph(1)), Attachments: [][]byte{{}, {1}}}, Limits{}, nil},
		{"dangling", Arguments{Values: raws(ph(2)), Attachments: [][]byte{{}, {}}}, Limits{}, ErrAttachments},
		{"negative", Arguments{Values: raws(`{"_placeholder":true,"num":-1}`), Attachments: [][]byte{{}}}, Limits{}, ErrAttachments},
		{"fractional", Arguments{Values: raws(`{"_placeholder":true,"num":0.5}`), Attachments: [][]byte{{}}}, Limits{}, ErrAttachments},
		{"no num", Arguments{Values: raws(`{"_placeholder":true}`), Attachments: [][]byte{{}}}, Limits{}, ErrAttachments},
		{"no attachments", Arguments{Values: raws(ph(0))}, Limits{}, ErrAttachments},
		{"bad json", Arguments{Values: raws(`{`)}, Limits{}, ErrInvalid},
		{"empty value", Arguments{Values: raws(``)}, Limits{}, ErrInvalid},
		{"depth", Arguments{Values: raws(`[[1]]`)}, Limits{MaxDepth: 1}, ErrDepth},
		{"depth ok", Arguments{Values: raws(`[[1]]`)}, Limits{MaxDepth: 2}, nil},
		{"count", Arguments{Attachments: [][]byte{{}, {}}}, Limits{MaxAttachments: 1}, ErrTooManyAttachments},
		{"bytes", Arguments{Values: raws(`"abc"`), Attachments: [][]byte{{1, 2}}}, Limits{MaxEventBytes: 6}, ErrTooLarge},
		{"bytes exact", Arguments{Values: raws(`"abc"`), Attachments: [][]byte{{1, 2}}}, Limits{MaxEventBytes: 7}, nil},
		{"limits", Arguments{}, Limits{MaxDepth: -1}, ErrLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.a.Validate(tc.l)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
		})
	}
}

// escapedPlaceholder spells the key of Placeholder(n) with JSON escapes; it decodes to
// the same object, so every layer must treat it as the plain form.
func escapedPlaceholder(n int) string {
	return `{"\u005fplaceholder":true,"\u006eum":` + strconv.Itoa(n) + `}`
}

func TestEscapedPlaceholderKey(t *testing.T) {
	t.Run("Validate", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			a    Arguments
			want error
		}{
			{"in range", Arguments{Values: raws(escapedPlaceholder(0)), Attachments: [][]byte{{1}}}, nil},
			{"out of range", Arguments{Values: raws(escapedPlaceholder(9)), Attachments: [][]byte{{1}}}, ErrAttachments},
			{"no attachments", Arguments{Values: raws(escapedPlaceholder(0))}, ErrAttachments},
			{"nested", Arguments{Values: raws(`{"a":[` + escapedPlaceholder(3) + `]}`), Attachments: [][]byte{{1}}}, ErrAttachments},
		} {
			if err := tc.a.Validate(Limits{}); tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("%s: Validate = %v, want %v", tc.name, err, tc.want)
			}
		}
	})
	t.Run("Decode", func(t *testing.T) {
		p, err := Decode([]byte(`51-["e",`+escapedPlaceholder(0)+`]`), [][]byte{{7, 8}}, Limits{})
		if err != nil {
			t.Fatal(err)
		}
		_, args, err := EventArguments(p)
		if err != nil {
			t.Fatal(err)
		}
		b, err := JSON[[]byte](Limits{}).Decode(args)
		if err != nil || !bytes.Equal(b, []byte{7, 8}) {
			t.Fatalf("JSON[[]byte] = %v, %v", b, err)
		}
		m, err := JSON[map[string]any](Limits{}).Decode(Arguments{Values: raws(`{"k":` + escapedPlaceholder(0) + `}`), Attachments: [][]byte{{7, 8}}})
		if err != nil || m["k"] != "BwA=" && m["k"] != "Bwg=" {
			t.Fatalf("JSON[map] = %v, %v", m, err)
		}
	})
	t.Run("Concat", func(t *testing.T) {
		first := Arguments{Values: raws(`1`), Attachments: [][]byte{{10}}}
		second := Arguments{Values: raws(escapedPlaceholder(0)), Attachments: [][]byte{{20}}}
		joined, err := Concat(first, second)
		if err != nil {
			t.Fatal(err)
		}
		if string(joined.Values[1]) != `{"_placeholder":true,"num":1}` {
			t.Fatalf("renumbered: %s", joined.Values[1])
		}
		if _, err := Concat(first, Arguments{Values: raws(escapedPlaceholder(5)), Attachments: [][]byte{{20}}}); !errors.Is(err, ErrAttachments) {
			t.Fatalf("Concat dangling = %v", err)
		}
	})
	t.Run("Slice", func(t *testing.T) {
		a := Arguments{Values: raws(`1`, escapedPlaceholder(2)), Attachments: [][]byte{{10}, {11}, {12}}}
		got, err := a.Slice(1, 2)
		if err != nil {
			t.Fatal(err)
		}
		if string(got.Values[0]) != `{"_placeholder":true,"num":0}` || !reflect.DeepEqual(got.Attachments, [][]byte{{12}}) {
			t.Fatalf("%s %v", got.Values[0], got.Attachments)
		}
		bad := Arguments{Values: raws(escapedPlaceholder(3)), Attachments: [][]byte{{1}}}
		if _, err := bad.Slice(0, 1); !errors.Is(err, ErrAttachments) {
			t.Fatalf("Slice dangling = %v", err)
		}
	})
}

func TestConcatAndSlice(t *testing.T) {
	first := Arguments{Values: raws(`{"f":`+string(Placeholder(0))+`}`, `1`), Attachments: [][]byte{{10}}}
	second := Arguments{Values: raws(`[` + string(Placeholder(1)) + `,` + string(Placeholder(0)) + `,"<&>"]`), Attachments: [][]byte{{20}, {21}, {22}}}
	joined, err := Concat(first, second)
	if err != nil {
		t.Fatal(err)
	}
	if len(joined.Values) != 3 || len(joined.Attachments) != 4 {
		t.Fatalf("%#v", joined)
	}
	if err := joined.Validate(Limits{}); err != nil {
		t.Fatal(err)
	}
	if string(joined.Values[0]) != `{"f":`+string(Placeholder(0))+`}` {
		t.Fatalf("first part rewritten: %s", joined.Values[0])
	}
	if string(joined.Values[2]) != `[{"_placeholder":true,"num":2},{"_placeholder":true,"num":1},"<&>"]` {
		t.Fatalf("second part: %s", joined.Values[2])
	}
	// Slice undoes Concat for the second part; the unreferenced attachment {22} of
	// the part is not carried over.
	back, err := joined.Slice(2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if string(back.Values[0]) != `[{"_placeholder":true,"num":1},{"_placeholder":true,"num":0},"<&>"]` ||
		!reflect.DeepEqual(back.Attachments, [][]byte{{20}, {21}}) {
		t.Fatalf("%s %v", back.Values[0], back.Attachments)
	}
	// Ownership: results share nothing with the inputs.
	joined.Values[1][0] = 'X'
	joined.Attachments[0][0] = 99
	if string(first.Values[1]) != `1` || first.Attachments[0][0] != 10 {
		t.Fatal("Concat aliased its input")
	}
	back.Attachments[0][0] = 77
	if joined.Attachments[1][0] != 20 {
		t.Fatal("Slice aliased its input")
	}
	empty, err := Concat()
	if err != nil || len(empty.Values) != 0 || len(empty.Attachments) != 0 {
		t.Fatalf("%#v %v", empty, err)
	}
	for _, r := range [][2]int{{-1, 1}, {2, 1}, {0, 4}} {
		if _, err := joined.Slice(r[0], r[1]); !errors.Is(err, ErrArity) {
			t.Errorf("Slice%v = %v", r, err)
		}
	}
	bad := Arguments{Values: raws(string(Placeholder(3))), Attachments: [][]byte{{}}}
	if _, err := bad.Slice(0, 1); !errors.Is(err, ErrAttachments) {
		t.Errorf("Slice dangling = %v", err)
	}
	if _, err := Concat(first, bad); !errors.Is(err, ErrAttachments) {
		t.Errorf("Concat dangling renumber = %v", err)
	}
}

func TestPlaceholderHelper(t *testing.T) {
	if got := string(Placeholder(12)); got != `{"_placeholder":true,"num":12}` {
		t.Fatal(got)
	}
	if !strings.Contains(string(Placeholder(0)), placeholderKey) {
		t.Fatal("placeholderKey is not what Placeholder writes")
	}
}

func FuzzArguments(f *testing.F) {
	f.Add([]byte(`{"a":1}`), []byte(string(Placeholder(0))), []byte{1, 2})
	f.Add([]byte(`[`+string(Placeholder(1))+`]`), []byte(`null`), []byte{})
	f.Fuzz(func(t *testing.T, v1, v2, att []byte) {
		l := Limits{MaxEventBytes: 4096, MaxAttachments: 8, MaxDepth: 16}
		a := Arguments{Values: raws(string(v1), string(v2)), Attachments: [][]byte{att}}
		codec := JSON[any](l)
		if err := a.Validate(l); err != nil {
			// Whatever Validate rejects, the codec must reject without panicking.
			_, _ = codec.Decode(Arguments{Values: a.Values[:1], Attachments: a.Attachments})
			return
		}
		double := Limits{MaxEventBytes: 2 * l.MaxEventBytes, MaxAttachments: 2 * l.MaxAttachments, MaxDepth: l.MaxDepth}
		joined, err := Concat(a, a)
		if err != nil {
			t.Fatalf("Concat: %v", err)
		}
		if err := joined.Validate(double); err != nil {
			t.Fatalf("Concat result invalid: %v", err)
		}
		back, err := joined.Slice(2, 4)
		if err != nil {
			t.Fatalf("Slice: %v", err)
		}
		if err := back.Validate(l); err != nil {
			t.Fatalf("Slice result invalid: %v", err)
		}
		for i := range back.Values {
			one, err := back.Slice(i, i+1)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := codec.Decode(one); err != nil && !errors.Is(err, ErrInvalid) {
				t.Fatalf("Decode: %v", err)
			}
		}
	})
}

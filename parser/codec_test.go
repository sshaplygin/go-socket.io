package parser

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
		n    int // attachments the envelope announces
	}{
		{"0", Packet{Type: Connect, Namespace: "/"}, 0},
		{`0/admin,{"token":"秘密"}`, Packet{Type: Connect, Namespace: "/admin", Data: json.RawMessage(`{"token":"秘密"}`)}, 0},
		{"1/admin,", Packet{Type: Disconnect, Namespace: "/admin"}, 0},
		{`2/chat,12["message",{"n":1},null]`, Packet{Type: Event, Namespace: "/chat", ID: id(12), Data: json.RawMessage(`["message",{"n":1},null]`)}, 0},
		{`39007199254740991[]`, Packet{Type: Ack, Namespace: "/", ID: id(MaxID), Data: json.RawMessage(`[]`)}, 0},
		{`4{"message":"denied","data":{"code":403}}`, Packet{Type: ConnectError, Namespace: "/", Data: json.RawMessage(`{"message":"denied","data":{"code":403}}`)}, 0},
		{`4"denied"`, Packet{Type: ConnectError, Namespace: "/", Data: json.RawMessage(`"denied"`)}, 0},
		{`51-/bin,0["file",{"_placeholder":true,"num":0}]`, Packet{Type: Event, Namespace: "/bin", ID: id(0), Data: json.RawMessage(`["file",{"_placeholder":true,"num":0}]`)}, 1},
		{`61-2[{"_placeholder":true,"num":0}]`, Packet{Type: Ack, Namespace: "/", ID: id(2), Data: json.RawMessage(`[{"_placeholder":true,"num":0}]`)}, 1},
		{`2[42,"numeric event"]`, Packet{Type: Event, Namespace: "/", Data: json.RawMessage(`[42,"numeric event"]`)}, 0},
	}
	l, err := Limits{}.normalized()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.wire, func(t *testing.T) {
			got, n, err := parseEnvelope([]byte(tc.wire), l)
			if err != nil || n != tc.n || !reflect.DeepEqual(got, tc.p) {
				t.Fatalf("parseEnvelope = %#v, %d, %v", got, n, err)
			}
			encoded, err := encodeEnvelope(tc.p, tc.n, l)
			if err != nil || string(encoded) != tc.wire {
				t.Fatalf("encodeEnvelope=%s %v", encoded, err)
			}
			// The same message through the public complete-message API.
			var frames [][]byte
			for i := 0; i < tc.n; i++ {
				frames = append(frames, []byte{byte(i), 255})
			}
			decoded, err := Decode([]byte(tc.wire), frames, Limits{})
			want := tc.p
			want.Attachments = frames
			if err != nil || !reflect.DeepEqual(decoded, want) {
				t.Fatalf("Decode = %#v, %v", decoded, err)
			}
			text, out, err := Encode(want, Limits{})
			if err != nil || string(text) != tc.wire || !reflect.DeepEqual(out, frames) {
				t.Fatalf("Encode = %s, %v, %v", text, out, err)
			}
		})
	}
}

func TestNamespaceNormalization(t *testing.T) {
	for _, ns := range []string{"", "/"} {
		text, _, err := Encode(Packet{Type: Connect, Namespace: ns}, Limits{})
		if err != nil || string(text) != "0" {
			t.Fatalf("Encode(%q) = %s, %v", ns, text, err)
		}
	}
	p, err := Decode([]byte("0"), nil, Limits{})
	if err != nil || p.Namespace != "/" {
		t.Fatalf("Decode default namespace = %#v, %v", p, err)
	}
}

func TestMalformed(t *testing.T) {
	cases := []string{"", "7", "0null", "0[]", "1{}", "2", "3", "4", "2[]", "2[true]", `2["connect"]`, `2["disconnecting"]`, `3{}`, `4[]`, `2["x"]garbage`, "5-[]", `50-["x"]`, `5+1-["x"]`, `51.0-["x"]`, `51e0-["x"]`, `599999999999999999999999-["x"]`, `51-["x",{"_placeholder":true,"num":1}]`, `51-["x",{"_placeholder":true,"num":0.5}]`, `51-["x",{"_placeholder":true,"num":"0"}]`, `51-["x",{"_placeholder":true}]`, `51-["x",{"_placeholder":true,"num":-1}]`, `2/room`, `39007199254740992[]`, `3999999999999999999999[]`, `2["x",`, "2[\"\xff\"]", "1/a\n,", "2/room,[null]"}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			l, _ := Limits{}.normalized()
			p, _, err := parseEnvelope([]byte(body), l)
			if err == nil || !reflect.DeepEqual(p, Packet{}) {
				t.Fatalf("got partial/success %#v %v", p, err)
			}
			// A message built from the same envelope, with every attachment the header
			// could announce supplied, is rejected too.
			p, err = Decode([]byte(body), [][]byte{{0}}, Limits{})
			if err == nil || !reflect.DeepEqual(p, Packet{}) {
				t.Fatalf("Decode got partial/success %#v %v", p, err)
			}
		})
	}
}
func TestLimitsAndOwnership(t *testing.T) {
	raw := []byte(`51-["x",{"_placeholder":true,"num":0}]`)
	bin := []byte{0, 255, 4}
	g, err := Decode(raw, [][]byte{bin}, Limits{MaxEventBytes: len(raw) + len(bin)})
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = '0'
	bin[0] = 9
	if g.Type != Event || len(g.Attachments) != 1 || g.Attachments[0][0] != 0 {
		t.Fatal("aliased input")
	}
	encoded, attachments, err := Encode(g, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	encoded[0] = '0'
	attachments[0][0] = 8
	if g.Type != Event || g.Attachments[0][0] != 0 {
		t.Fatal("aliased output")
	}
	_, err = Decode([]byte(`51-["x",{"_placeholder":true,"num":0}]`), [][]byte{bin}, Limits{MaxEventBytes: len(raw) + len(bin) - 1})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	_, err = Decode([]byte(`51-["x"]`), nil, Limits{})
	if !errors.Is(err, ErrAttachments) {
		t.Fatal(err)
	}
	_, err = Decode([]byte(`2["x"]`), [][]byte{{}}, Limits{})
	if !errors.Is(err, ErrAttachments) {
		t.Fatal(err)
	}
	_, err = Decode([]byte(`565-["x"]`), nil, Limits{})
	if !errors.Is(err, ErrTooManyAttachments) {
		t.Fatal(err)
	}
	_, err = Decode([]byte(`2["x",[]]`), nil, Limits{MaxDepth: 1})
	if !errors.Is(err, ErrDepth) {
		t.Fatal(err)
	}
	_, err = Decode([]byte(`2["x",[]]`), nil, Limits{MaxDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []Limits{{MaxEventBytes: -1}, {MaxAttachments: -1}, {MaxDepth: -1}, {AttachmentTimeout: -1}} {
		if _, err := Decode([]byte("0"), nil, l); !errors.Is(err, ErrLimit) {
			t.Fatal(err)
		}
		if _, _, err := Encode(Packet{}, l); !errors.Is(err, ErrLimit) {
			t.Fatal(err)
		}
	}
}

// TestExactBoundaries pins that a message of exactly the limit is accepted and one
// byte more is not, for the envelope, the attachments and the count.
func TestExactBoundaries(t *testing.T) {
	text := []byte(`51-["x",{"_placeholder":true,"num":0}]`)
	att := [][]byte{{1, 2, 3, 4}}
	exact := len(text) + 4
	if _, err := Decode(text, att, Limits{MaxEventBytes: exact}); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(text, att, Limits{MaxEventBytes: exact - 1}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	p := Packet{Type: Event, Data: json.RawMessage(`["x",{"_placeholder":true,"num":0}]`), Attachments: att}
	if _, _, err := Encode(p, Limits{MaxEventBytes: exact}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Encode(p, Limits{MaxEventBytes: exact - 1}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err := Decode([]byte(`52-["x"]`), [][]byte{{}, {}}, Limits{MaxAttachments: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode([]byte(`52-["x"]`), [][]byte{{}, {}}, Limits{MaxAttachments: 1}); !errors.Is(err, ErrTooManyAttachments) {
		t.Fatal(err)
	}
}

// TestPlaceholdersThroughDecode replaces the experiment's generic-tree Reconstruct
// cases: nested and repeated placeholders are resolved by the argument codec into
// owned bytes, large integers keep their digits, and an ordinary packet may carry a
// placeholder-shaped object that names no attachment.
func TestPlaceholdersThroughDecode(t *testing.T) {
	data := json.RawMessage(`["x",{"nested":[{"_placeholder":true,"num":0}]},{"_placeholder":true,"num":0},9007199254740993]`)
	p := Packet{Type: Event, Data: data, Attachments: [][]byte{{0, 255}}}
	text, atts, err := Encode(p, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	g, err := Decode(text, atts, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	name, args, err := EventArguments(g)
	if err != nil || name != "x" || len(args.Values) != 3 {
		t.Fatalf("EventArguments = %q, %#v, %v", name, args, err)
	}
	nestedArgs, err := args.Slice(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	nested, err := JSON[struct{ Nested [][]byte }](Limits{}).Decode(nestedArgs)
	if err != nil || len(nested.Nested) != 1 || !bytes.Equal(nested.Nested[0], []byte{0, 255}) {
		t.Fatalf("nested = %#v, %v", nested, err)
	}
	nested.Nested[0][0] = 8
	if g.Attachments[0][0] != 0 || nestedArgs.Attachments[0][0] != 0 {
		t.Fatal("decoded value aliased the arguments")
	}
	direct, err := args.Slice(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	b, err := JSON[[]byte](Limits{}).Decode(direct)
	if err != nil || !bytes.Equal(b, []byte{0, 255}) {
		t.Fatalf("direct = %v, %v", b, err)
	}
	b[0] = 7
	if nested.Nested[0][0] != 8 || g.Attachments[0][0] != 0 {
		t.Fatal("repeated references share a buffer; each decode must own its bytes")
	}
	big, err := JSON[json.Number](Limits{}).Decode(Arguments{Values: args.Values[2:3]})
	if err != nil || big != "9007199254740993" {
		t.Fatalf("number = %q, %v", big, err)
	}
	ordinary := []byte(`2["x",{"_placeholder":true,"num":99}]`)
	if _, err = Decode(ordinary, nil, Limits{}); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeInvalid(t *testing.T) {
	// The first eight cases are the experiment's, translated to the base-type model:
	// its BinaryAck without a count is an Ack that announces attachments with none.
	for _, p := range []Packet{
		{Type: 7},
		{Namespace: "bad"},
		{Namespace: "/a,b"},
		{ID: id(MaxID + 1)},
		{Attachments: [][]byte{{}}},
		{Type: Ack, Attachments: [][]byte{{}}},
		{Type: Event, Data: json.RawMessage(`[]`)},
		{Type: Connect, Data: json.RawMessage(`{`)},
		{Type: Disconnect, Attachments: [][]byte{{}}, Data: json.RawMessage(`[]`)},
		{Type: ConnectError, Attachments: [][]byte{{}}, Data: json.RawMessage(`"x"`)},
		{Type: Event, Data: json.RawMessage(`["x",{"_placeholder":true,"num":1}]`), Attachments: [][]byte{{}}},
		{Type: Event, Namespace: "/a\nb", Data: json.RawMessage(`["x"]`)},
	} {
		if _, _, err := Encode(p, Limits{}); err == nil {
			t.Fatalf("accepted %#v", p)
		}
	}
	if _, _, err := Encode(Packet{Type: Connect, Data: json.RawMessage(`{}`)}, Limits{MaxEventBytes: 2}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	if _, _, err := Encode(Packet{Namespace: "/" + strings.Repeat("a", 10)}, Limits{MaxEventBytes: 2}); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	many := make([][]byte, 3)
	p := Packet{Type: Event, Data: json.RawMessage(`["x"]`), Attachments: many}
	if _, _, err := Encode(p, Limits{MaxAttachments: 2}); !errors.Is(err, ErrTooManyAttachments) {
		t.Fatal(err)
	}
}

// TestEncodeDoesNotMutate pins that Encode leaves its packet untouched and that its
// output does not alias it.
func TestEncodeDoesNotMutate(t *testing.T) {
	data := json.RawMessage(` ["x" , {"_placeholder":true,"num":0}] `)
	att := [][]byte{{1, 2, 3}}
	p := Packet{Type: Event, Namespace: "/n", ID: id(5), Data: data, Attachments: att}
	want := Packet{Type: Event, Namespace: "/n", ID: id(5), Data: append(json.RawMessage{}, data...), Attachments: [][]byte{{1, 2, 3}}}
	text, out, err := Encode(p, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("Encode modified its argument: %#v", p)
	}
	text[len(text)-2] = 'X'
	out[0][0] = 99
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("output aliases the packet: %#v", p)
	}
}

func TestType(t *testing.T) {
	want := map[Type]string{Connect: "CONNECT", Disconnect: "DISCONNECT", Event: "EVENT", Ack: "ACK", ConnectError: "CONNECT_ERROR", 9: "Type(9)"}
	for typ, s := range want {
		if typ.String() != s {
			t.Errorf("%d.String() = %q, want %q", typ, typ.String(), s)
		}
	}
}

func FuzzEnvelope(f *testing.F) {
	for _, b := range []string{"0", `2["x"]`, `51-["x",{"_placeholder":true,"num":0}]`, `3[]`, ""} {
		f.Add([]byte(b))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		l := Limits{MaxEventBytes: 4096, MaxAttachments: 8, MaxDepth: 16}
		nl, err := l.normalized()
		if err != nil {
			t.Fatal(err)
		}
		p, n, err := parseEnvelope(b, nl)
		if err != nil {
			return
		}
		out, err := encodeEnvelope(p, n, nl)
		if err != nil {
			t.Fatal(err)
		}
		again, m, err := parseEnvelope(out, nl)
		if err != nil || m != n || !reflect.DeepEqual(p, again) {
			t.Fatalf("roundtrip: %v", err)
		}
	})
}

func FuzzGroup(f *testing.F) {
	f.Add([]byte(`51-["x",{"_placeholder":true,"num":0}]`), []byte{0, 255})
	f.Fuzz(func(t *testing.T, b, attachment []byte) {
		l := Limits{MaxEventBytes: 4096, MaxAttachments: 8, MaxDepth: 16}
		g, err := Decode(b, [][]byte{attachment}, l)
		if err != nil {
			return
		}
		text, atts, err := Encode(g, l)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Decode(text, atts, l)
		if err != nil || !reflect.DeepEqual(g, again) {
			t.Fatalf("roundtrip: %v", err)
		}
		if g.Type == Event || g.Type == Ack {
			if _, err := arrayValues(g); err != nil {
				t.Fatal(err)
			}
		}
	})
}

// arrayValues splits the arguments of an EVENT or ACK packet and validates them with
// the attachments, as a runtime does before handing them to a codec.
func arrayValues(p Packet) (Arguments, error) {
	var args Arguments
	var err error
	if p.Type == Event {
		_, args, err = EventArguments(p)
	} else {
		args, err = AckArguments(p)
	}
	if err != nil {
		return Arguments{}, err
	}
	return args, args.Validate(Limits{MaxEventBytes: 4096, MaxAttachments: 8, MaxDepth: 16})
}

// TestDepthBeyondStdlib checks that nesting past the 10000 levels encoding/json
// accepts is ErrDepth, not ErrInvalid, in every entry point that validates JSON.
func TestDepthBeyondStdlib(t *testing.T) {
	nest := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	for _, n := range []int{100, 10000, 10001, 20000} {
		text := `2["x",` + nest(n) + `]`
		if _, err := Decode([]byte(text), nil, Limits{}); !errors.Is(err, ErrDepth) {
			t.Errorf("Decode at depth %d: %v, want ErrDepth", n, err)
		}
		// An unterminated document that is too deep is still a limit breach.
		if _, err := Decode([]byte(`2["x",`+strings.Repeat("[", n)), nil, Limits{}); n > 10000 && !errors.Is(err, ErrDepth) {
			t.Errorf("Decode of unterminated depth %d: %v, want ErrDepth", n, err)
		}
		args := Arguments{Values: []json.RawMessage{json.RawMessage(nest(n))}}
		if err := args.Validate(Limits{}); !errors.Is(err, ErrDepth) {
			t.Errorf("Validate at depth %d: %v, want ErrDepth", n, err)
		}
		if _, err := EventPacket("/", nil, "x", args); n > 10000 && !errors.Is(err, ErrDepth) {
			t.Errorf("EventPacket at depth %d: %v, want ErrDepth", n, err)
		}
	}
	// Brackets inside strings do not count, and malformed shallow text stays ErrInvalid.
	deepString := `2["` + strings.Repeat("[", 20000) + `"]`
	if _, err := Decode([]byte(deepString), nil, Limits{}); err != nil {
		t.Errorf("brackets in a string: %v", err)
	}
	if _, err := Decode([]byte(`2["x",[1,]`), nil, Limits{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("malformed shallow text: %v, want ErrInvalid", err)
	}
}

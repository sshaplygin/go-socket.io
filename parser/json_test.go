package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// bin mirrors socketio.Binary without importing the root package.
type bin []byte

func (b bin) SocketIOBinary() []byte { return []byte(b) }

// ptrBin has a pointer-receiver marker, found only through an addressable value.
type ptrBin struct{ b []byte }

func (p *ptrBin) SocketIOBinary() []byte { return p.b }

type upload struct {
	Name   string            `json:"name"`
	Data   bin               `json:"data"`
	Parts  []bin             `json:"parts,omitempty"`
	ByKey  map[string]bin    `json:"byKey,omitempty"`
	Extra  *bin              `json:"extra,omitempty"`
	Any    any               `json:"any,omitempty"`
	Skip   string            `json:"-"`
	Plain  map[string]string `json:"plain,omitempty"`
	hidden int
}

type embedded struct{ Tag string }

type withEmbedded struct {
	embedded
	Blob bin `json:"blob"`
}

func TestJSONEncodeBinary(t *testing.T) {
	extra := bin{7}
	v := upload{
		Name:   "file",
		Data:   bin{0, 255},
		Parts:  []bin{{1}, {}},
		ByKey:  map[string]bin{"b": {3}, "a": {2}},
		Extra:  &extra,
		Any:    []any{bin{4}, "s", 5},
		Skip:   "x",
		Plain:  map[string]string{"k": "v"},
		hidden: 3,
	}
	c := JSON[upload](Limits{})
	a, err := c.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"file","data":{"_placeholder":true,"num":0},"parts":[{"_placeholder":true,"num":1},{"_placeholder":true,"num":2}],` +
		`"byKey":{"a":{"_placeholder":true,"num":3},"b":{"_placeholder":true,"num":4}},"extra":{"_placeholder":true,"num":5},` +
		`"any":[{"_placeholder":true,"num":6},"s",5],"plain":{"k":"v"}}`
	if len(a.Values) != 1 || string(a.Values[0]) != want {
		t.Fatalf("got  %s\nwant %s", a.Values[0], want)
	}
	if !reflect.DeepEqual(a.Attachments, [][]byte{{0, 255}, {1}, {}, {2}, {3}, {7}, {4}}) {
		t.Fatalf("attachments %v", a.Attachments)
	}
	if a.Attachments[2] == nil {
		t.Fatal("an empty attachment must stay a non-nil empty buffer")
	}
	// Decoding the arguments restores the struct, with owned bytes.
	got, err := c.Decode(a)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "file" || !bytes.Equal(got.Data, []byte{0, 255}) || len(got.Parts) != 2 || !bytes.Equal(got.Parts[0], []byte{1}) ||
		!bytes.Equal(got.ByKey["b"], []byte{3}) || got.Extra == nil || !bytes.Equal(*got.Extra, []byte{7}) {
		t.Fatalf("%#v", got)
	}
	got.Data[0] = 42
	if a.Attachments[0][0] != 0 {
		t.Fatal("Decode result aliases the arguments")
	}
	// The input was not touched and the attachments are copies.
	a.Attachments[0][0] = 1
	if v.Data[0] != 0 {
		t.Fatal("Encode aliased the value's bytes")
	}
}

func TestJSONEncodeShapes(t *testing.T) {
	var nilBin bin
	cases := []struct {
		name string
		got  func() (Arguments, error)
		want string
		atts int
	}{
		{"top-level binary", func() (Arguments, error) { return JSON[bin](Limits{}).Encode(bin{1}) }, `{"_placeholder":true,"num":0}`, 1},
		{"nil binary is an empty attachment", func() (Arguments, error) { return JSON[bin](Limits{}).Encode(nilBin) }, `{"_placeholder":true,"num":0}`, 1},
		{"plain struct", func() (Arguments, error) {
			return JSON[struct{ A int }](Limits{}).Encode(struct{ A int }{3})
		}, `{"A":3}`, 0},
		{"nil pointer", func() (Arguments, error) { return JSON[*upload](Limits{}).Encode(nil) }, `null`, 0},
		{"nil slice of binary", func() (Arguments, error) { return JSON[[]bin](Limits{}).Encode(nil) }, `null`, 0},
		{"array of binary", func() (Arguments, error) { return JSON[[2]bin](Limits{}).Encode([2]bin{{1}, {2}}) }, `[{"_placeholder":true,"num":0},{"_placeholder":true,"num":1}]`, 2},
		{"embedded struct is flattened", func() (Arguments, error) {
			return JSON[withEmbedded](Limits{}).Encode(withEmbedded{embedded{"t"}, bin{1}})
		}, `{"Tag":"t","blob":{"_placeholder":true,"num":0}}`, 1},
		{"interface holding a plain value", func() (Arguments, error) { return JSON[any](Limits{}).Encode(map[string]int{"a": 1}) }, `{"a":1}`, 0},
		{"interface holding a map with binary", func() (Arguments, error) {
			return JSON[any](Limits{}).Encode(map[string]any{"z": bin{1}, "a": nil})
		}, `{"a":null,"z":{"_placeholder":true,"num":0}}`, 1},
		{"plain bytes stay base64", func() (Arguments, error) { return JSON[[]byte](Limits{}).Encode([]byte{1, 2}) }, `"AQI="`, 0},
		{"custom marshaler wins", func() (Arguments, error) { return JSON[marshaled](Limits{}).Encode(marshaled{bin{1}}) }, `"custom"`, 0},
		{"omitempty drops the field", func() (Arguments, error) { return JSON[upload](Limits{}).Encode(upload{Name: "n"}) }, `{"name":"n","data":{"_placeholder":true,"num":0}}`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := tc.got()
			if err != nil || len(a.Values) != 1 || string(a.Values[0]) != tc.want || len(a.Attachments) != tc.atts {
				t.Fatalf("%v %#v", err, a)
			}
			if err := a.Validate(Limits{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type marshaled struct{ Hidden bin }

func (marshaled) MarshalJSON() ([]byte, error) { return []byte(`"custom"`), nil }

func TestJSONPointerReceiver(t *testing.T) {
	type holder struct{ P ptrBin }
	a, err := JSON[holder](Limits{}).Encode(holder{ptrBin{[]byte{5}}})
	if err != nil || string(a.Values[0]) != `{"P":{"_placeholder":true,"num":0}}` || !bytes.Equal(a.Attachments[0], []byte{5}) {
		t.Fatalf("%v %#v", err, a)
	}
	b, err := JSON[*ptrBin](Limits{}).Encode(&ptrBin{[]byte{6}})
	if err != nil || string(b.Values[0]) != `{"_placeholder":true,"num":0}` {
		t.Fatalf("%v %#v", err, b)
	}
}

func TestJSONEncodeUnsupported(t *testing.T) {
	type tagged struct {
		N int `json:"n,string"`
		B bin
	}
	type dup struct {
		embedded
		B bin `json:"Tag"`
	}
	if _, err := JSON[tagged](Limits{}).Encode(tagged{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("string option: %v", err)
	}
	if _, err := JSON[dup](Limits{}).Encode(dup{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("duplicate name: %v", err)
	}
	if _, err := JSON[map[int]bin](Limits{}).Encode(map[int]bin{1: {1}}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("int map key: %v", err)
	}
	if _, err := JSON[struct{ F func() }](Limits{}).Encode(struct{ F func() }{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("func field: %v", err)
	}
	if _, err := JSON[any](Limits{}).Encode(map[string]any{"b": bin{1}, "c": make(chan int)}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("chan: %v", err)
	}
}

func TestJSONEncodeLimits(t *testing.T) {
	if _, err := JSON[[]bin](Limits{MaxAttachments: 2}).Encode([]bin{{}, {}, {}}); !errors.Is(err, ErrTooManyAttachments) {
		t.Errorf("count: %v", err)
	}
	if _, err := JSON[[]bin](Limits{MaxAttachments: 3}).Encode([]bin{{}, {}, {}}); err != nil {
		t.Errorf("exact count: %v", err)
	}
	if _, err := JSON[bin](Limits{MaxEventBytes: 30}).Encode(make(bin, 2)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("bytes: %v", err) // 29 bytes of placeholder plus 2 attachment bytes
	}
	if _, err := JSON[bin](Limits{MaxEventBytes: 31}).Encode(make(bin, 2)); err != nil {
		t.Errorf("exact bytes: %v", err)
	}
	if _, err := JSON[bin](Limits{MaxEventBytes: 10}).Encode(make(bin, 11)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("attachment alone: %v", err)
	}
	if _, err := JSON[string](Limits{MaxEventBytes: 10}).Encode(strings.Repeat("a", 20)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("plain value: %v", err)
	}
	var deep any = []bin{{1}}
	for i := 0; i < 5; i++ {
		deep = []any{deep}
	}
	if _, err := JSON[any](Limits{MaxDepth: 3}).Encode(deep); !errors.Is(err, ErrDepth) {
		t.Errorf("depth: %v", err)
	}
	type cyc struct {
		Next *cyc
		B    bin
	}
	c := &cyc{}
	c.Next = c
	if _, err := JSON[*cyc](Limits{}).Encode(c); !errors.Is(err, ErrDepth) {
		t.Errorf("cycle: %v", err)
	}
	if _, err := JSON[int](Limits{MaxDepth: -1}).Encode(1); !errors.Is(err, ErrLimit) {
		t.Errorf("limits: %v", err)
	}
	if _, err := JSON[int](Limits{MaxDepth: -1}).Decode(Arguments{Values: raws(`1`)}); !errors.Is(err, ErrLimit) {
		t.Errorf("limits: %v", err)
	}
}

func TestJSONDecode(t *testing.T) {
	type msg struct {
		N    int    `json:"n"`
		Data []byte `json:"data"`
	}
	c := JSON[msg](Limits{})
	good := Arguments{Values: raws(`{"n": 5, "data": ` + string(Placeholder(1)) + `}`), Attachments: [][]byte{{9}, {1, 2}}}
	m, err := c.Decode(good)
	if err != nil || m.N != 5 || !bytes.Equal(m.Data, []byte{1, 2}) {
		t.Fatalf("%#v %v", m, err)
	}
	for _, tc := range []struct {
		name string
		a    Arguments
		want error
	}{
		{"no arguments", Arguments{}, ErrArity},
		{"two arguments", Arguments{Values: raws(`{}`, `{}`)}, ErrArity},
		{"dangling placeholder", Arguments{Values: raws(string(Placeholder(0)))}, ErrAttachments},
		{"wrong type", Arguments{Values: raws(`"s"`)}, ErrInvalid},
		{"wrong field type", Arguments{Values: raws(`{"n":"x"}`)}, ErrInvalid},
		{"not json", Arguments{Values: raws(`{`)}, ErrInvalid},
	} {
		if _, err := c.Decode(tc.a); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	// Decode does not modify the arguments it is given.
	if string(good.Values[0]) != `{"n": 5, "data": `+string(Placeholder(1))+`}` || good.Attachments[1][0] != 1 {
		t.Fatal("Decode modified its argument")
	}
	// A placeholder meeting a string field decodes to base64 text.
	s, err := JSON[string](Limits{}).Decode(Arguments{Values: raws(string(Placeholder(0))), Attachments: [][]byte{{1, 2}}})
	if err != nil || s != "AQI=" {
		t.Fatalf("%q %v", s, err)
	}
	// An argument absent in JSON terms but spelled null is one argument.
	if p, err := JSON[*msg](Limits{}).Decode(Arguments{Values: raws(`null`)}); err != nil || p != nil {
		t.Fatalf("%v %v", p, err)
	}
	if _, err := JSON[json.Number](Limits{MaxEventBytes: 3}).Decode(Arguments{Values: raws(`12345`)}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("%v", err)
	}
}

func TestJSONRoundTripThroughPackets(t *testing.T) {
	c := JSON[upload](Limits{})
	a, err := c.Encode(upload{Name: "x", Data: bin{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := EventPacket("/up", id(1), "upload", a)
	if err != nil {
		t.Fatal(err)
	}
	text, atts, err := Encode(p, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	d, err := Decode(text, atts, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	_, args, err := EventArguments(d)
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Decode(args)
	if err != nil || u.Name != "x" || !bytes.Equal(u.Data, []byte{1, 2, 3}) {
		t.Fatalf("%#v %v", u, err)
	}
}

func TestArgs2StyleExpansion(t *testing.T) {
	ca, cb := JSON[bin](Limits{}), JSON[struct{ B bin }](Limits{})
	a1, err := ca.Encode(bin{1})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := cb.Encode(struct{ B bin }{bin{2}})
	if err != nil {
		t.Fatal(err)
	}
	both, err := Concat(a1, a2)
	if err != nil || len(both.Values) != 2 || len(both.Attachments) != 2 {
		t.Fatalf("%#v %v", both, err)
	}
	first, err := both.Slice(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := both.Slice(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	x, err := ca.Decode(first)
	if err != nil || !bytes.Equal(x, []byte{1}) {
		t.Fatalf("%v %v", x, err)
	}
	y, err := cb.Decode(second)
	if err != nil || !bytes.Equal(y.B, []byte{2}) {
		t.Fatalf("%v %v", y, err)
	}
}

// TestJSONOmitEmpty pins that the walker drops omitempty fields exactly as
// encoding/json does, for every kind encoding/json treats as empty.
func TestJSONOmitEmpty(t *testing.T) {
	type s struct {
		B  bool           `json:"b,omitempty"`
		I  int            `json:"i,omitempty"`
		U  uint8          `json:"u,omitempty"`
		F  float64        `json:"f,omitempty"`
		S  string         `json:"s,omitempty"`
		P  *int           `json:"p,omitempty"`
		M  map[string]int `json:"m,omitempty"`
		L  []int          `json:"l,omitempty"`
		A  [0]int         `json:"a,omitempty"`
		E  any            `json:"e,omitempty"`
		St struct{}       `json:"st,omitempty"`
		D  bin            `json:"d"`
	}
	for _, v := range []s{{}, {B: true, I: -1, U: 1, F: 0.5, S: "x", P: new(int), M: map[string]int{"k": 1}, L: []int{0}, E: 0, D: bin{1}}} {
		a, err := JSON[s](Limits{}).Encode(v)
		if err != nil {
			t.Fatal(err)
		}
		// encoding/json spells the same value with the placeholder in place of d.
		w := v
		w.D = nil
		plain, err := json.Marshal(w)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(string(plain), `"d":null`, `"d":`+string(Placeholder(0)), 1)
		if string(a.Values[0]) != want {
			t.Errorf("got  %s\nwant %s", a.Values[0], want)
		}
	}
}

package eio4

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

type fixture struct {
	Name    string          `json:"name"`
	Wire    string          `json:"wire"`
	Packets []fixturePacket `json:"packets"`
}

type fixturePacket struct {
	Type   packet.Type `json:"type"`
	Binary bool        `json:"binary"`
	Text   string      `json:"text"`
	Base64 string      `json:"base64"`
}

func (f fixture) packetValues(t testing.TB) []Packet {
	t.Helper()
	packets := make([]Packet, len(f.Packets))
	for i, p := range f.Packets {
		packets[i] = Packet{Frame: frame.String, Type: p.Type, Data: []byte(p.Text)}
		if p.Binary {
			data, err := base64.StdEncoding.DecodeString(p.Base64)
			if err != nil {
				t.Fatal(err)
			}
			packets[i].Frame, packets[i].Data = frame.Binary, data
		}
	}
	return packets
}

func loadFixtures(t testing.TB) []fixture {
	t.Helper()
	data, err := os.ReadFile("testdata/payloads.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []fixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func TestProtocolFixtures(t *testing.T) {
	for _, f := range loadFixtures(t) {
		t.Run(f.Name, func(t *testing.T) {
			want := f.packetValues(t)
			encoded, err := Encode(want, len(f.Wire))
			if err != nil || string(encoded) != f.Wire {
				t.Fatalf("Encode = %q, %v; want %q", encoded, err, f.Wire)
			}
			decoded, err := Decode([]byte(f.Wire), len(f.Wire))
			if err != nil {
				t.Fatal(err)
			}
			assertPackets(t, decoded, want)
			if _, err := Encode(want, len(f.Wire)-1); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("Encode below wire size = %v", err)
			}
			if _, err := Decode([]byte(f.Wire), len(f.Wire)-1); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("Decode below wire size = %v", err)
			}
		})
	}
}

func assertPackets(t testing.TB, got, want []Packet) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d packets, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Frame != want[i].Frame || got[i].Type != want[i].Type || !bytes.Equal(got[i].Data, want[i].Data) {
			t.Fatalf("packet %d = %+v; want %+v", i, got[i], want[i])
		}
	}
}

func TestMalformedPayload(t *testing.T) {
	for _, wire := range []string{
		"", "\x1e", "\x1e4ok", "4ok\x1e", "4ok\x1e\x1e2", "7", "9bad", "/", ":", "B", "４hello",
		"4\xff", "4\xc0\x80", "4\xed\xa0\x80", "4\xf4\x90\x80\x80",
		"b!", "bA", "bAA", "bAAA", "b====", "bAA=A", "bAA==x", "bAB==", "bAAB=",
		"bAA==\n", "bAA\r==", "b AA==", "b__8=", "4ok\x1eb!", "4ok\x1e9",
	} {
		t.Run(wire, func(t *testing.T) {
			got, err := Decode([]byte(wire), 1024)
			if got != nil || !errors.Is(err, ErrInvalidPayload) {
				t.Fatalf("Decode(%q) = %+v, %v", wire, got, err)
			}
		})
	}
}

func TestInvalidPackets(t *testing.T) {
	for _, p := range []Packet{
		{Type: -1}, {Type: 7}, {Type: 256}, {Frame: 2, Type: packet.MESSAGE},
		{Frame: frame.Binary, Type: packet.PING},
		{Type: packet.MESSAGE, Data: []byte("hello\x1eworld")},
		{Type: packet.MESSAGE, Data: []byte{0xff}},
	} {
		got, err := Encode([]Packet{{Type: packet.NOOP}, p}, 1024)
		if got != nil || !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("Encode(%+v) = %q, %v", p, got, err)
		}
	}
	if got, err := Encode(nil, 1); got != nil || !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("Encode(nil) = %q, %v", got, err)
	}
}

func TestLimits(t *testing.T) {
	for _, limit := range []int{0, -1} {
		if _, err := Encode([]Packet{{Type: packet.NOOP}}, limit); !errors.Is(err, ErrInvalidLimit) {
			t.Fatalf("Encode limit %d: %v", limit, err)
		}
		if _, err := Decode([]byte("6"), limit); !errors.Is(err, ErrInvalidLimit) {
			t.Fatalf("Decode limit %d: %v", limit, err)
		}
	}
	for _, limit := range []int{1, 2} {
		got, err := Encode([]Packet{{Type: packet.PING}, {Type: packet.PONG}}, limit)
		if got != nil || !errors.Is(err, ErrTooLarge) {
			t.Fatalf("two packets with limit %d = %q, %v", limit, got, err)
		}
	}
	for _, binary := range []bool{false, true} {
		p := Packet{Type: packet.MESSAGE, Data: bytes.Repeat([]byte("x"), 128*1024)}
		if binary {
			p.Frame = frame.Binary
		}
		body, err := Encode([]Packet{p}, 256*1024)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(body, len(body))
		if err != nil {
			t.Fatal(err)
		}
		assertPackets(t, got, []Packet{p})
	}
}

func TestBinarySizeBoundaries(t *testing.T) {
	for _, tc := range []struct{ dataBytes, wireBytes int }{
		{0, 1}, {1, 5}, {2, 5}, {3, 5}, {4, 9}, {5, 9}, {6, 9},
	} {
		t.Run(strconv.Itoa(tc.dataBytes), func(t *testing.T) {
			want := []Packet{{Frame: frame.Binary, Type: packet.MESSAGE, Data: make([]byte, tc.dataBytes)}}
			body, err := Encode(want, tc.wireBytes)
			if err != nil || len(body) != tc.wireBytes {
				t.Fatalf("Encode = %q, %v; want %d bytes", body, err, tc.wireBytes)
			}
			got, err := Decode(body, tc.wireBytes)
			if err != nil {
				t.Fatal(err)
			}
			assertPackets(t, got, want)
			if tc.wireBytes > 1 {
				if _, err := Encode(want, tc.wireBytes-1); !errors.Is(err, ErrTooLarge) {
					t.Fatalf("Encode below exact limit = %v", err)
				}
				if _, err := Decode(body, tc.wireBytes-1); !errors.Is(err, ErrTooLarge) {
					t.Fatalf("Decode below exact limit = %v", err)
				}
			}
			if _, err := Encode(want, int(^uint(0)>>1)); err != nil {
				t.Fatalf("Encode with MaxInt limit = %v", err)
			}
		})
	}
}

func TestOversizedTextRejectedBeforeValidation(t *testing.T) {
	// Size rejection wins over content validation when raw bytes alone cannot fit.
	for _, data := range [][]byte{[]byte("too long"), {0xff, 0xff}, {'\x1e', '\x1e'}} {
		got, err := Encode([]Packet{{Type: packet.MESSAGE, Data: data}}, 1)
		if got != nil || !errors.Is(err, ErrTooLarge) {
			t.Fatalf("Encode oversized text = %q, %v", got, err)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"mixed", []byte("4hello\x1e2\x1ebAQIDBA==")},
		{"large-text", append([]byte("4"), bytes.Repeat([]byte("x"), 128*1024)...)},
		{"dense-records", bytes.Repeat([]byte("4\x1e"), 500000)[:999999]},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.body)))
			for i := 0; i < b.N; i++ {
				if _, err := Decode(tc.body, len(tc.body)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestOwnedData(t *testing.T) {
	body := []byte("4hello\x1ebAQIDBA==")
	got, err := Decode(body, len(body))
	if err != nil {
		t.Fatal(err)
	}
	wantBody := bytes.Clone(body)
	got[0].Data[0], got[1].Data[0] = 'H', 9
	if !bytes.Equal(body, wantBody) {
		t.Fatal("Decode data aliases input")
	}
	encoded, err := Encode(got, len(body))
	if err != nil {
		t.Fatal(err)
	}
	got[0].Data[0], got[1].Data[0] = 'X', 0
	if string(encoded) != "4Hello\x1ebCQIDBA==" {
		t.Fatalf("Encode data aliases input: %q", encoded)
	}
}

func FuzzDecode(f *testing.F) {
	for _, fixture := range loadFixtures(f) {
		f.Add([]byte(fixture.Wire))
	}
	for _, wire := range []string{"", "4\x1e", "bAB==", "4\xff", "7"} {
		f.Add([]byte(wire))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		got, err := Decode(body, 1<<20)
		if err != nil {
			if got != nil {
				t.Fatal("partial result on error")
			}
			return
		}
		encoded, err := Encode(got, len(body))
		if err != nil || !bytes.Equal(encoded, body) {
			t.Fatalf("noncanonical round trip: %q -> %q (%v)", body, encoded, err)
		}
	})
}

func FuzzBinaryRoundTrip(f *testing.F) {
	f.Add([]byte{0, 255, 0x1e})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		want := []Packet{{Frame: frame.Binary, Type: packet.MESSAGE, Data: data}}
		limit := 1 + base64.StdEncoding.EncodedLen(len(data))
		body, err := Encode(want, limit)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(body, limit)
		if err != nil {
			t.Fatal(err)
		}
		assertPackets(t, got, want)
	})
}

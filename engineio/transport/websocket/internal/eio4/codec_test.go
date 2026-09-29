package eio4

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/googollee/go-socket.io/engineio/frame"
	"github.com/googollee/go-socket.io/engineio/packet"
)

func assertPacket(t testing.TB, got, want Packet) {
	t.Helper()
	if got.Frame != want.Frame || got.Type != want.Type || !bytes.Equal(got.Data, want.Data) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestReferenceFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/packets.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name   string      `json:"name"`
		Type   packet.Type `json:"type"`
		Text   string      `json:"text"`
		Binary bool        `json:"binary"`
		Base64 string      `json:"base64"`
		Wire   string      `json:"wire"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			want := Packet{Frame: frame.String, Type: f.Type, Data: []byte(f.Text)}
			if f.Binary {
				want.Frame = frame.Binary
				want.Data, err = base64.StdEncoding.DecodeString(f.Base64)
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, supportsBinary := range []bool{false, true} {
				wantType, wantBody := frame.String, []byte(f.Wire)
				if supportsBinary && f.Binary {
					wantType, wantBody = frame.Binary, want.Data
				}
				limit := max(1, len(wantBody))
				ft, body, err := Encode(want, supportsBinary, limit)
				if err != nil || ft != wantType || !bytes.Equal(body, wantBody) {
					t.Fatalf("Encode(binary=%v) = %v %q %v; want %v %q", supportsBinary, ft, body, err, wantType, wantBody)
				}
				got, err := Decode(wantType, wantBody, limit)
				if err != nil {
					t.Fatal(err)
				}
				assertPacket(t, got, want)
				if len(wantBody) > 1 {
					if _, body, err := Encode(want, supportsBinary, len(wantBody)-1); body != nil || !errors.Is(err, ErrTooLarge) {
						t.Fatalf("undersized Encode = %q %v", body, err)
					}
					if _, err := Decode(wantType, wantBody, len(wantBody)-1); !errors.Is(err, ErrTooLarge) {
						t.Fatalf("undersized Decode = %v", err)
					}
				}
			}
		})
	}
}

func TestInvalidPackets(t *testing.T) {
	for _, p := range []Packet{
		{Type: -1}, {Type: 7}, {Frame: 2},
		{Frame: frame.Binary, Type: packet.PING},
		{Type: packet.MESSAGE, Data: []byte{0xff}},
	} {
		if _, body, err := Encode(p, true, 100); body != nil || !errors.Is(err, ErrInvalidPacket) {
			t.Fatalf("Encode(%+v) = %q %v", p, body, err)
		}
	}
	for _, body := range []string{"", "7", "bA", "bAA", "bAB==", "bAAB=", "bAA==\r", "bAA==\n", "b AA==", "b__8=", "4\xff"} {
		got, err := Decode(frame.String, []byte(body), 100)
		if !errors.Is(err, ErrInvalidPacket) {
			t.Fatalf("Decode(%q) = %+v %v", body, got, err)
		}
		assertPacket(t, got, Packet{})
	}
	if _, err := Decode(2, []byte("4"), 100); !errors.Is(err, ErrInvalidPacket) {
		t.Fatal(err)
	}
}

func TestLimitsAndOwnership(t *testing.T) {
	for _, limit := range []int{0, -1} {
		if _, _, err := Encode(Packet{}, true, limit); !errors.Is(err, ErrInvalidLimit) {
			t.Fatal(err)
		}
		if _, err := Decode(frame.Binary, nil, limit); !errors.Is(err, ErrInvalidLimit) {
			t.Fatal(err)
		}
	}
	for _, ft := range []frame.Type{frame.String, frame.Binary} {
		for _, supportBinary := range []bool{false, true} {
			p := Packet{Frame: ft, Type: packet.MESSAGE, Data: []byte("hello")}
			messageType, body, err := Encode(p, supportBinary, int(^uint(0)>>1))
			if err != nil {
				t.Fatal(err)
			}
			p.Data[0] = 'X'
			got, err := Decode(messageType, body, len(body))
			if err != nil || string(got.Data) != "hello" {
				t.Fatalf("Encode aliases input: %+v %v", got, err)
			}
			original := bytes.Clone(body)
			got.Data[0] = 'Y'
			if !bytes.Equal(body, original) {
				t.Fatal("Decode aliases input")
			}
		}
	}
}

func FuzzDecode(f *testing.F) {
	for _, body := range []string{"4hello", "4a\x1eb", "bAQIDBA==", "b", "", "7", "4\xff", "bAB=="} {
		f.Add([]byte(body), false)
	}
	f.Add([]byte{0, 4, 255, 0x1e}, true)
	f.Fuzz(func(t *testing.T, body []byte, binary bool) {
		ft := frame.String
		if binary {
			ft = frame.Binary
		}
		p, err := Decode(ft, body, 1<<20)
		if err != nil {
			assertPacket(t, p, Packet{})
			return
		}
		encodedType, encoded, err := Encode(p, binary, max(1, len(body)))
		if err != nil || encodedType != ft || !bytes.Equal(encoded, body) {
			t.Fatalf("noncanonical round trip: %q -> %q, %v", body, encoded, err)
		}
	})
}

func FuzzBinaryRoundTrip(f *testing.F) {
	f.Add([]byte{0, 255, 0x1e}, true)
	f.Add([]byte{}, false)
	f.Fuzz(func(t *testing.T, data []byte, supportsBinary bool) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		p := Packet{Frame: frame.Binary, Type: packet.MESSAGE, Data: data}
		limit := 1 + base64.StdEncoding.EncodedLen(len(data))
		ft, body, err := Encode(p, supportsBinary, limit)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(ft, body, limit)
		if err != nil {
			t.Fatal(err)
		}
		assertPacket(t, got, p)
	})
}

package payload

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

func TestBatchFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/batches.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		fixture
		Limit    int  `json:"limit"`
		Count    int  `json:"count"`
		TooLarge bool `json:"tooLarge"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			packets := f.packetValues(t)
			body, count, err := EncodeBatch(packets, f.Limit)
			if f.TooLarge {
				if body != nil || count != 0 || !errors.Is(err, ErrTooLarge) {
					t.Fatalf("EncodeBatch = %q, %d, %v", body, count, err)
				}
				return
			}
			if err != nil || count != f.Count || string(body) != f.Wire || len(body) > f.Limit {
				t.Fatalf("EncodeBatch = %q, %d, %v; want %q, %d", body, count, err, f.Wire, f.Count)
			}
			if count > 0 {
				decoded, err := Decode(body, f.Limit)
				if err != nil {
					t.Fatal(err)
				}
				assertPackets(t, decoded, packets[:count])
			}
		})
	}
}

func TestBatchOrderAndOwnership(t *testing.T) {
	want := []Packet{
		{Type: packet.MESSAGE, Data: []byte("first")},
		{Frame: frame.Binary, Type: packet.MESSAGE, Data: []byte{0, 255}},
		{Type: packet.NOOP},
		{Type: packet.MESSAGE, Data: []byte("last")},
	}
	queue := append([]Packet(nil), want...)
	for i := range queue {
		queue[i].Data = bytes.Clone(want[i].Data)
	}
	var received []Packet
	for len(queue) > 0 {
		body, count, err := EncodeBatch(queue, 7)
		if err != nil || count <= 0 {
			t.Fatalf("EncodeBatch = %q, %d, %v", body, count, err)
		}
		// Selecting a batch does not remove its packets or alter their data.
		assertPackets(t, queue, want[len(want)-len(queue):])
		decoded, err := Decode(body, 7)
		if err != nil {
			t.Fatal(err)
		}
		received = append(received, decoded...)
		queue = queue[count:]
	}
	assertPackets(t, received, want)
	body, _, err := EncodeBatch(want, 100)
	if err != nil {
		t.Fatal(err)
	}
	want[0].Data[0] = 'X'
	if !bytes.HasPrefix(body, []byte("4first")) {
		t.Fatal("batch body aliases packet data")
	}
}

func TestBatchErrorsAndCutoff(t *testing.T) {
	for _, limit := range []int{-1, 0} {
		if body, count, err := EncodeBatch(nil, limit); body != nil || count != 0 || !errors.Is(err, ErrInvalidLimit) {
			t.Fatalf("invalid limit = %q, %d, %v", body, count, err)
		}
	}
	if body, count, err := EncodeBatch(nil, 1); body != nil || count != 0 || err != nil {
		t.Fatalf("empty queue = %q, %d, %v", body, count, err)
	}
	valid := Packet{Type: packet.MESSAGE, Data: []byte("ok")}
	invalid := Packet{Type: packet.MESSAGE, Data: []byte{0xff}}
	if body, count, err := EncodeBatch([]Packet{valid, invalid}, 100); body != nil || count != 0 || !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("invalid scanned packet = %q, %d, %v", body, count, err)
	}
	// The cutoff packet prevents examining an invalid later packet. Nothing is
	// skipped to fill unused space; the large packet will fail as the next head.
	large := Packet{Type: packet.MESSAGE, Data: []byte("too large")}
	queue := []Packet{valid, large, invalid}
	if body, count, err := EncodeBatch(queue, 5); err != nil || count != 1 || string(body) != "4ok" {
		t.Fatalf("size cutoff = %q, %d, %v", body, count, err)
	}
	if body, count, err := EncodeBatch(queue[1:], 5); body != nil || count != 0 || !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized next head = %q, %d, %v", body, count, err)
	}
	if body, count, err := EncodeBatch([]Packet{valid}, int(^uint(0)>>1)); err != nil || count != 1 || string(body) != "4ok" {
		t.Fatalf("MaxInt limit = %q, %d, %v", body, count, err)
	}
}

func FuzzEncodeBatch(f *testing.F) {
	f.Add([]byte{1, 0, 2, 3, 0, 4}, uint16(7))
	f.Add([]byte{}, uint16(1))
	f.Add([]byte{255}, uint16(0))
	f.Fuzz(func(t *testing.T, data []byte, limit uint16) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		parts := bytes.SplitN(data, []byte{0}, 16)
		queue := make([]Packet, len(parts))
		for i, part := range parts {
			queue[i] = Packet{Frame: frame.Binary, Type: packet.MESSAGE, Data: part}
			if i%2 != 0 {
				queue[i].Frame = frame.String
				queue[i].Data = []byte(hex.EncodeToString(part))
			}
		}
		for len(queue) > 0 {
			body, count, err := EncodeBatch(queue, int(limit))
			if err != nil {
				if body != nil || count != 0 {
					t.Fatal("partial batch on error")
				}
				if limit == 0 {
					if !errors.Is(err, ErrInvalidLimit) {
						t.Fatal(err)
					}
					return
				}
				first, firstErr := Encode(queue[:1], int(^uint(0)>>1))
				if !errors.Is(err, ErrTooLarge) || firstErr != nil || len(first) <= int(limit) {
					t.Fatalf("unexpected batch error %v, first size %d, limit %d", err, len(first), limit)
				}
				return
			}
			if count <= 0 || count > len(queue) || len(body) > int(limit) {
				t.Fatalf("bad batch: bytes=%d count=%d limit=%d", len(body), count, limit)
			}
			got, err := Decode(body, int(limit))
			if err != nil {
				t.Fatal(err)
			}
			assertPackets(t, got, queue[:count])
			if count < len(queue) {
				next, err := Encode(queue[:count+1], int(^uint(0)>>1))
				if err != nil || len(next) <= int(limit) {
					t.Fatalf("non-maximal batch: next size=%d limit=%d err=%v", len(next), limit, err)
				}
			}
			queue = queue[count:]
		}
	})
}

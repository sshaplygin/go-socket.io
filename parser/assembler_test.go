package parser

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

func mustAssembler(t testing.TB, l Limits) *Assembler {
	t.Helper()
	a, err := NewAssembler(l)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAssemblerTextOnly(t *testing.T) {
	a := mustAssembler(t, Limits{})
	p, done, err := a.Text([]byte(`2/n,3["e",1]`))
	if err != nil || !done || p.Type != Event || p.Namespace != "/n" || *p.ID != 3 || a.Pending() {
		t.Fatalf("%#v %v %v", p, done, err)
	}
	if _, ok := a.Deadline(); ok {
		t.Fatal("deadline without a pending message")
	}
}

func TestAssemblerBinary(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := mustAssembler(t, Limits{AttachmentTimeout: 5 * time.Second})
	a.now = func() time.Time { return now }
	text := []byte(`62-4["x",{"_placeholder":true,"num":1}]`)
	p, done, err := a.Text(text)
	if err != nil || done || !a.Pending() {
		t.Fatalf("%#v %v %v", p, done, err)
	}
	text[0] = '0' // the Assembler does not retain the frame
	if d, ok := a.Deadline(); !ok || !d.Equal(now.Add(5*time.Second)) {
		t.Fatalf("deadline %v %v", d, ok)
	}
	frame := []byte{1, 2}
	now = now.Add(5 * time.Second) // exactly at the deadline is still in time
	if p, done, err = a.Binary(frame); err != nil || done {
		t.Fatalf("%#v %v %v", p, done, err)
	}
	frame[0] = 9 // frames are copied
	if p, done, err = a.Binary(nil); err != nil || !done || a.Pending() {
		t.Fatalf("%#v %v %v", p, done, err)
	}
	want := Packet{Type: Ack, Namespace: "/", ID: id(4), Data: []byte(`["x",{"_placeholder":true,"num":1}]`), Attachments: [][]byte{{1, 2}, {}}}
	if !reflect.DeepEqual(p, want) || p.Attachments[1] == nil {
		t.Fatalf("%#v", p)
	}
	// The same message through Decode is identical.
	d, err := Decode([]byte(`62-4["x",{"_placeholder":true,"num":1}]`), [][]byte{{1, 2}, {}}, Limits{})
	if err != nil || !reflect.DeepEqual(d, p) {
		t.Fatalf("%#v %v", d, err)
	}
	// The Assembler is reusable after a completed message.
	if _, done, err = a.Text([]byte("0")); err != nil || !done {
		t.Fatalf("%v %v", done, err)
	}
}

func TestAssemblerErrors(t *testing.T) {
	bin1 := []byte(`51-["x",{"_placeholder":true,"num":0}]`)
	t.Run("binary without envelope", func(t *testing.T) {
		a := mustAssembler(t, Limits{})
		if _, _, err := a.Binary([]byte{1}); !errors.Is(err, ErrUnexpectedFrame) {
			t.Fatal(err)
		}
	})
	t.Run("text while pending", func(t *testing.T) {
		a := mustAssembler(t, Limits{})
		if _, _, err := a.Text(bin1); err != nil {
			t.Fatal(err)
		}
		if _, _, err := a.Text([]byte("0")); !errors.Is(err, ErrUnexpectedFrame) {
			t.Fatal(err)
		}
		if a.Pending() {
			t.Fatal("an error must leave the Assembler empty")
		}
		if _, done, err := a.Text([]byte("0")); err != nil || !done {
			t.Fatal(err)
		}
	})
	t.Run("invalid envelope is rejected before attachments are awaited", func(t *testing.T) {
		a := mustAssembler(t, Limits{})
		if _, _, err := a.Text([]byte(`51-["x",{"_placeholder":true,"num":1}]`)); !errors.Is(err, ErrAttachments) || a.Pending() {
			t.Fatal(err)
		}
		if _, _, err := a.Text([]byte(`565-["x"]`)); !errors.Is(err, ErrTooManyAttachments) || a.Pending() {
			t.Fatal(err)
		}
		if _, _, err := a.Text([]byte(`7`)); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		now := time.Unix(0, 0)
		a := mustAssembler(t, Limits{})
		a.now = func() time.Time { return now }
		if _, _, err := a.Text(bin1); err != nil {
			t.Fatal(err)
		}
		now = now.Add(DefaultAttachmentTimeout + time.Nanosecond)
		if _, _, err := a.Binary([]byte{1}); !errors.Is(err, ErrAttachmentTimeout) || a.Pending() {
			t.Fatal(err)
		}
	})
	t.Run("byte budget counts the envelope and every frame", func(t *testing.T) {
		a := mustAssembler(t, Limits{MaxEventBytes: len(bin1) + 3})
		if _, _, err := a.Text(bin1); err != nil {
			t.Fatal(err)
		}
		if _, _, err := a.Binary([]byte{1, 2, 3, 4}); !errors.Is(err, ErrTooLarge) || a.Pending() {
			t.Fatal(err)
		}
		if _, _, err := a.Text(bin1); err != nil {
			t.Fatal(err)
		}
		if _, done, err := a.Binary([]byte{1, 2, 3}); err != nil || !done {
			t.Fatalf("exact budget: %v %v", done, err)
		}
		if _, _, err := a.Text(append(bin1, bytes.Repeat([]byte(" "), 10)...)); !errors.Is(err, ErrTooLarge) {
			t.Fatal(err)
		}
	})
	t.Run("second frame over the budget", func(t *testing.T) {
		a := mustAssembler(t, Limits{MaxEventBytes: len(`52-["x"]`) + 4})
		if _, _, err := a.Text([]byte(`52-["x"]`)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := a.Binary([]byte{1, 2, 3}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := a.Binary([]byte{1, 2}); !errors.Is(err, ErrTooLarge) {
			t.Fatal(err)
		}
	})
	t.Run("reset", func(t *testing.T) {
		a := mustAssembler(t, Limits{})
		if _, _, err := a.Text(bin1); err != nil {
			t.Fatal(err)
		}
		a.Reset()
		if a.Pending() {
			t.Fatal("pending after Reset")
		}
		if _, _, err := a.Binary([]byte{1}); !errors.Is(err, ErrUnexpectedFrame) {
			t.Fatal(err)
		}
	})
	t.Run("limits", func(t *testing.T) {
		if _, err := NewAssembler(Limits{MaxAttachments: -1}); !errors.Is(err, ErrLimit) {
			t.Fatal(err)
		}
	})
}

func FuzzAssembler(f *testing.F) {
	f.Add([]byte(`51-["x",{"_placeholder":true,"num":0}]`), []byte{0, 255}, []byte("0"))
	f.Add([]byte(`2["x"]`), []byte{}, []byte(`62-1[{"_placeholder":true,"num":1}]`))
	f.Fuzz(func(t *testing.T, first, second, third []byte) {
		l := Limits{MaxEventBytes: 4096, MaxAttachments: 8, MaxDepth: 16}
		a := mustAssembler(t, l)
		a.now = func() time.Time { return time.Unix(0, 0) }
		// The first frame is text, the others alternate: the Assembler must never
		// panic, must reject out-of-order frames and must agree with Decode.
		p, done, err := a.Text(first)
		if err != nil {
			if a.Pending() {
				t.Fatal("pending after an error")
			}
			return
		}
		if done {
			want, err := Decode(first, nil, l)
			if err != nil || !reflect.DeepEqual(p, want) {
				t.Fatalf("Assembler and Decode disagree: %#v %#v %v", p, want, err)
			}
			return
		}
		var frames [][]byte
		for _, fr := range [][]byte{second, third, second, third, second, third, second, third} {
			frames = append(frames, fr)
			p, done, err = a.Binary(fr)
			if err != nil {
				return
			}
			if done {
				want, err := Decode(first, frames, l)
				if err != nil || !reflect.DeepEqual(p, want) {
					t.Fatalf("Assembler and Decode disagree: %#v %#v %v", p, want, err)
				}
				return
			}
		}
	})
}

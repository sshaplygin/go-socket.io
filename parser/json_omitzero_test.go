//go:build go1.24

package parser_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/sshaplygin/go-socket.io/parser"
)

type omittedBinary struct{ Files []parser.Buffer }

func (omittedBinary) IsZero() bool { return true }

type omittedAfterNumbering struct{ File parser.Buffer }

func (v omittedAfterNumbering) IsZero() bool {
	data, _ := json.Marshal(v.File)
	return bytes.Contains(data, []byte(`"_placeholder"`))
}

type countedOmission struct {
	File  parser.Buffer
	Calls *int
}

func (v countedOmission) IsZero() bool {
	*v.Calls++
	return true
}

func TestEncoderHonorsJSONOmitZero(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"zero Buffer", struct {
			File parser.Buffer `json:"file,omitzero"`
		}{}},
		{"custom IsZero", struct {
			Hidden omittedBinary `json:"hidden,omitzero"`
		}{omittedBinary{[]parser.Buffer{{Data: []byte("secret")}}}}},
		{"IsZero examines numbered representation", struct {
			Hidden omittedAfterNumbering `json:"hidden,omitzero"`
		}{omittedAfterNumbering{parser.Buffer{Data: []byte("secret")}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := new(recordedWriter)
			if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, []any{"event", tc.value}); err != nil {
				t.Fatal(err)
			}
			if len(w.frames) != 1 || w.frames[0].String() != "2[\"event\",{}]\n" {
				t.Fatalf("omitted fields must not transmit binary data: %v", w.frames)
			}
		})
	}
}

func TestEncoderNumbersOnlyTransmittedAttachments(t *testing.T) {
	value := struct {
		Hidden  omittedBinary `json:"hidden,omitzero"`
		Visible parser.Buffer `json:"visible"`
	}{omittedBinary{make([]parser.Buffer, 11)}, parser.Buffer{Data: []byte("public")}}
	w := new(recordedWriter)
	if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, []any{"event", value}); err != nil {
		t.Fatal(err)
	}
	if len(w.frames) != 2 || w.frames[1].String() != "public" {
		t.Fatalf("expected only the visible attachment: %v", w.frames)
	}
	if got, want := w.frames[0].String(), "51-[\"event\",{\"visible\":{\"_placeholder\":true,\"num\":0}}]\n"; got != want {
		t.Fatalf("text frame = %q, want %q", got, want)
	}
}

func TestEncoderEvaluatesIsZeroOnce(t *testing.T) {
	calls := 0
	value := struct {
		Hidden countedOmission `json:"hidden,omitzero"`
	}{countedOmission{parser.Buffer{Data: []byte("secret")}, &calls}}
	w := new(recordedWriter)
	if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, []any{"event", value}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("IsZero called %d times, want one JSON evaluation", calls)
	}
	if len(w.frames) != 1 {
		t.Fatalf("omitted field transmitted %d frames", len(w.frames))
	}
}

func TestEncoderDoesNotAssociateLiteralPlaceholdersWithHiddenBuffers(t *testing.T) {
	value := struct {
		Hidden omittedBinary  `json:"hidden,omitzero"`
		Public map[string]any `json:"public"`
	}{omittedBinary{[]parser.Buffer{{Data: []byte("secret")}}}, map[string]any{"_placeholder": true, "num": 0}}
	w := new(recordedWriter)
	if err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, []any{"event", value}); err != nil {
		t.Fatal(err)
	}
	if len(w.frames) != 1 || w.frames[0].String() != "2[\"event\",{\"public\":{\"_placeholder\":true,\"num\":0}}]\n" {
		t.Fatalf("literal placeholder exposed a hidden attachment: %v", w.frames)
	}
}

type cyclicOmission struct {
	File  parser.Buffer
	Next  *cyclicOmission
	Omit  bool `json:"-"`
	Calls *int `json:"-"`
}

func (v *cyclicOmission) IsZero() bool {
	*v.Calls++
	return v.Omit
}

func TestEncoderDefersOmittedCycleToJSON(t *testing.T) {
	for _, omit := range []bool{true, false} {
		name := "visible cycle"
		if omit {
			name = "omitted cycle before visible attachment"
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			cycle := &cyclicOmission{File: parser.Buffer{Data: []byte("secret")}, Omit: omit, Calls: &calls}
			cycle.Next = cycle
			value := struct {
				Hidden  *cyclicOmission `json:"hidden,omitzero"`
				Visible parser.Buffer   `json:"visible"`
			}{cycle, parser.Buffer{Data: []byte("public")}}
			w := new(recordedWriter)
			err := parser.NewEncoder(w).Encode(parser.Header{Type: parser.Event}, []any{"event", value})
			if omit {
				if err != nil {
					t.Fatalf("JSON-omitted cycle rejected: %v", err)
				}
				if len(w.frames) != 2 || w.frames[1].String() != "public" {
					t.Fatalf("only visible attachment must be transmitted: %v", w.frames)
				}
				if got, want := w.frames[0].String(), "51-[\"event\",{\"visible\":{\"_placeholder\":true,\"num\":0}}]\n"; got != want {
					t.Fatalf("text frame = %q, want %q", got, want)
				}
			} else if err == nil || len(w.frames) != 0 {
				t.Fatalf("visible cycle must fail before writing: err=%v frames=%v", err, w.frames)
			}
			if calls != 1 {
				t.Fatalf("IsZero called %d times, want one JSON decision", calls)
			}
		})
	}
}

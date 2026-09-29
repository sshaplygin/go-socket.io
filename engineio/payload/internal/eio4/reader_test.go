package eio4

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"
)

func TestDecodeReaderBodyLimits(t *testing.T) {
	data, err := os.ReadFile("testdata/body-limits.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string `json:"name"`
		Wire   string `json:"wire"`
		Limit  int    `json:"limit"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := DecodeReader(iotest.OneByteReader(strings.NewReader(tc.Wire)), tc.Limit)
			switch tc.Status {
			case 200:
				if err != nil {
					t.Fatal(err)
				}
				wire, err := Encode(got, tc.Limit)
				if err != nil || string(wire) != tc.Wire {
					t.Fatalf("decoded body re-encodes to %q, %v; want %q", wire, err, tc.Wire)
				}
			case 413:
				if got != nil || !errors.Is(err, ErrTooLarge) {
					t.Fatalf("oversized body = %+v, %v", got, err)
				}
			default:
				t.Fatalf("unsupported reference status %d", tc.Status)
			}
		})
	}
}

func TestDecodeReaderFixtures(t *testing.T) {
	for _, fixture := range loadFixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			want, err := Decode([]byte(fixture.Wire), len(fixture.Wire))
			if err != nil {
				t.Fatal(err)
			}
			for name, wrap := range map[string]func(io.Reader) io.Reader{
				"whole":         func(r io.Reader) io.Reader { return r },
				"one-byte":      iotest.OneByteReader,
				"half":          iotest.HalfReader,
				"data-with-eof": iotest.DataErrReader,
			} {
				t.Run(name, func(t *testing.T) {
					got, err := DecodeReader(wrap(strings.NewReader(fixture.Wire)), len(fixture.Wire))
					if err != nil {
						t.Fatal(err)
					}
					assertPackets(t, got, want)
					got, err = DecodeReader(wrap(strings.NewReader(fixture.Wire)), len(fixture.Wire)-1)
					if got != nil || !errors.Is(err, ErrTooLarge) {
						t.Fatalf("undersized limit = %+v, %v", got, err)
					}
				})
			}
		})
	}
}

type trackedBody struct {
	io.Reader
	bytesRead int
	reads     int
	closed    bool
}

func (r *trackedBody) Read(p []byte) (int, error) {
	r.reads++
	n, err := r.Reader.Read(p)
	r.bytesRead += n
	return n, err
}

func (r *trackedBody) Close() error {
	r.closed = true
	return nil
}

type readFunc func([]byte) (int, error)

func (f readFunc) Read(p []byte) (int, error) { return f(p) }

func TestDecodeReaderStopsAtLimit(t *testing.T) {
	// This source never ends. A decoder that buffers it before enforcing the
	// limit would continue reading; fail immediately if it requests too much.
	const limit = 8
	remaining := limit + 1
	r := &trackedBody{Reader: readFunc(func(p []byte) (int, error) {
		if len(p) > remaining || remaining == 0 {
			t.Fatalf("read of %d bytes exceeds remaining allowance %d", len(p), remaining)
		}
		for i := range p {
			p[i] = '4'
		}
		remaining -= len(p)
		return len(p), nil
	})}
	got, err := DecodeReader(r, limit)
	if got != nil || !errors.Is(err, ErrTooLarge) {
		t.Fatalf("DecodeReader = %+v, %v", got, err)
	}
	if r.bytesRead != limit+1 || r.closed {
		t.Fatalf("bytesRead = %d, closed = %v", r.bytesRead, r.closed)
	}
}

func TestDecodeReaderLimits(t *testing.T) {
	for _, limit := range []int{0, -1} {
		r := &trackedBody{Reader: strings.NewReader("6")}
		got, err := DecodeReader(r, limit)
		if got != nil || !errors.Is(err, ErrInvalidLimit) || r.reads != 0 {
			t.Fatalf("limit %d = %+v, %v; reads %d", limit, got, err, r.reads)
		}
	}
	for _, limit := range []int{1, 2, int(^uint(0) >> 1)} {
		r := &trackedBody{Reader: strings.NewReader("6")}
		got, err := DecodeReader(r, limit)
		if err != nil || len(got) != 1 || r.closed {
			t.Fatalf("limit %d = %+v, %v; closed %v", limit, got, err, r.closed)
		}
	}
	for _, wire := range []string{"", "4ok\x1e", "7", "b!"} {
		got, err := DecodeReader(strings.NewReader(wire), 100)
		if got != nil || !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("invalid body %q = %+v, %v", wire, got, err)
		}
	}
}

func TestDecodeReaderReadErrors(t *testing.T) {
	broken := errors.New("broken source")
	for _, cause := range []error{broken, io.ErrUnexpectedEOF} {
		for _, tc := range []struct {
			name  string
			wire  string
			limit int
		}{
			{"before-data", "", 10},
			{"during-body", "4ok", 10},
			{"at-boundary", "4ok", 3},
		} {
			t.Run(cause.Error()+"/"+tc.name, func(t *testing.T) {
				r := io.MultiReader(strings.NewReader(tc.wire), iotest.ErrReader(cause))
				got, err := DecodeReader(r, tc.limit)
				if got != nil || !errors.Is(err, cause) {
					t.Fatalf("DecodeReader = %+v, %v; want cause %v", got, err, cause)
				}
			})
		}
	}
	// A Reader may return both data and an error. Preserve failures even if the
	// data seen so far happens to be a valid complete packet.
	r := readFunc(func(p []byte) (int, error) { return copy(p, "4ok"), broken })
	if got, err := DecodeReader(r, 3); got != nil || !errors.Is(err, broken) {
		t.Fatalf("simultaneous data/error = %+v, %v", got, err)
	}
	// Once an extra byte proves overflow, no additional read or EOF is needed.
	r = readFunc(func(p []byte) (int, error) { return copy(p, "4"), broken })
	source := io.MultiReader(strings.NewReader("4ok"), r)
	if got, err := DecodeReader(source, 3); got != nil || !errors.Is(err, ErrTooLarge) {
		t.Fatalf("overflow with simultaneous error = %+v, %v", got, err)
	}
}

func FuzzDecodeReader(f *testing.F) {
	for _, fixture := range loadFixtures(f) {
		f.Add([]byte(fixture.Wire), uint16(len(fixture.Wire)), uint8(1))
	}
	f.Add([]byte("4ok"), uint16(2), uint8(64))
	f.Add([]byte("6"), uint16(0), uint8(0))
	f.Fuzz(func(t *testing.T, body []byte, limit uint16, chunk uint8) {
		want, wantErr := Decode(body, int(limit))
		base := bytes.NewReader(body)
		r := &trackedBody{Reader: readFunc(func(p []byte) (int, error) {
			if len(p) > int(chunk)+1 {
				p = p[:int(chunk)+1]
			}
			return base.Read(p)
		})}
		got, err := DecodeReader(r, int(limit))
		if wantErr == nil {
			if err != nil {
				t.Fatal(err)
			}
			assertPackets(t, got, want)
		} else {
			if got != nil || err == nil {
				t.Fatalf("DecodeReader = %+v, %v; want %v", got, err, wantErr)
			}
			for _, kind := range []error{ErrInvalidLimit, ErrTooLarge, ErrInvalidPayload} {
				if errors.Is(err, kind) != errors.Is(wantErr, kind) {
					t.Fatalf("DecodeReader error %v differs from Decode error %v", err, wantErr)
				}
			}
		}
		if r.bytesRead > int(limit)+1 {
			t.Fatalf("read %d bytes with limit %d", r.bytesRead, limit)
		}
	})
}

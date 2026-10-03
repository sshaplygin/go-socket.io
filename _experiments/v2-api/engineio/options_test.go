package engineio_test

import (
	"context"
	"log/slog"
	"reflect"
	"testing"

	"github.com/sshaplygin/go-socket.io/experiments/v2-api/engineio"
)

// User callbacks must remain untouched during pure configuration normalization.
type forbiddenRedactor struct{}

func (forbiddenRedactor) Enabled(context.Context) bool { panic("called redactor during Normalize") }
func (forbiddenRedactor) Redact(context.Context, engineio.SessionInfo, engineio.PacketInfo, []byte, int) []byte {
	panic("called redactor during Normalize")
}

func TestObserverOptions(t *testing.T) {
	fallback := slog.Default()
	for _, limit := range []int{0, 1, 256} {
		input := engineio.Options{Logger: fallback, Hooks: &engineio.Hooks{}, PayloadPreviewBytes: limit, PayloadRedactor: forbiddenRedactor{}}
		got, err := input.Normalize()
		if err != nil || !reflect.DeepEqual(got, input) || slog.Default() != fallback {
			t.Fatalf("normalization mutated observer config or logger: %+v %v", got, err)
		}
	}
	for _, limit := range []int{-1, 257} {
		if _, err := (engineio.Options{PayloadPreviewBytes: limit}).Normalize(); err == nil {
			t.Fatalf("accepted preview limit %d", limit)
		}
	}
	zero, err := (engineio.Options{}).Normalize()
	if err != nil || zero.PayloadPreviewBytes != 0 || zero.PayloadRedactor != nil {
		t.Fatalf("zero configuration enabled preview: %+v %v", zero, err)
	}
	// A configured limit without a redactor is legal, but runtime capture must
	// remain disabled; this proof does not implement or claim that capture gate.
	if _, err := (engineio.Options{PayloadPreviewBytes: 128}).Normalize(); err != nil {
		t.Fatal(err)
	}
}

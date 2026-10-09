package engineio

import (
	"log/slog"
	"reflect"
	"testing"
)

func TestOptionsNormalizeObserverFields(t *testing.T) {
	fallback := slog.Default()
	for _, limit := range []int{0, 1, 256} {
		in := Options{Logger: fallback, Hooks: &Hooks{}, PayloadPreviewBytes: limit}
		got, err := in.Normalize()
		if err != nil || !reflect.DeepEqual(got, in) || slog.Default() != fallback {
			t.Fatalf("limit %d: normalization changed the options or the default logger: %+v %v", limit, got, err)
		}
	}
	for _, limit := range []int{-1, 257} {
		if _, err := (Options{PayloadPreviewBytes: limit}).Normalize(); err == nil {
			t.Fatalf("accepted preview limit %d", limit)
		}
	}
	zero, err := (Options{}).Normalize()
	if err != nil || zero.PayloadPreviewBytes != 0 || zero.Hooks != nil {
		t.Fatalf("zero options enabled an observer field: %+v %v", zero, err)
	}
}

func TestChainAndLoggingHooksAreSkeletons(t *testing.T) {
	if ChainHooks(&Hooks{}, nil) != nil || LoggingHooks(slog.Default()) != nil {
		t.Fatal("the skeleton constructors returned an observer")
	}
}

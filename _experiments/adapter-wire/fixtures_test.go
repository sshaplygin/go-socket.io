package adapterwire_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sshaplygin/go-socket.io/experiments/adapter-wire/internal/fixture"
)

func TestNodePublications(t *testing.T) {
	f, err := fixture.Load("testdata/publications.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) != 22 {
		t.Fatalf("unexpected case count: %d", len(f.Cases))
	}
	seen := make(map[string]bool)
	for _, tc := range f.Cases {
		if tc.Name == "" || seen[tc.Name] {
			t.Fatalf("missing/duplicate case name %q", tc.Name)
		}
		seen[tc.Name] = true
		t.Run(tc.Name, func(t *testing.T) {
			if tc.Name == "broadcast-local-no-publish" {
				if len(tc.Publications) != 0 {
					t.Fatal("local broadcast must not publish")
				}
				return
			}
			if len(tc.Publications) == 0 {
				t.Fatal("missing upstream publication")
			}
			for _, p := range tc.Publications {
				value, err := p.Decode()
				if err != nil {
					t.Fatal(err)
				}
				actualJSON, err := json.Marshal(fixture.Normalize(value))
				if err != nil {
					t.Fatal(err)
				}
				var actual, expected any
				if err := json.Unmarshal(actualJSON, &actual); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(p.Value, &expected); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("Go decoded %s; want %s", actualJSON, p.Value)
				}
			}
		})
	}
}

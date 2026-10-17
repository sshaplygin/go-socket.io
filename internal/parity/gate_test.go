package parity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParityGateRejectsInvalidEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, plan, source, want string
		strict                   bool
		listed                   map[testRef]bool
	}{
		{name: "missing DONE marker", plan: "DONE V1-1", source: "package sample", want: "B1 has no Covers"},
		{name: "unknown marker", plan: "DONE V1-1", source: "package sample\n// Covers Z9\nfunc TestKnown(){}", listed: map[testRef]bool{{".", "TestKnown"}: true}, want: "unknown Covers marker Z9"},
		{name: "nonexistent test", plan: "DONE V1-1", source: "package sample\n// Covers B1\nfunc TestMissing(){}", want: "no listed test"},
		{name: "wrong package", plan: "DONE V1-1", source: "package sample\n// Covers B1\nfunc TestKnown(){}", listed: map[testRef]bool{{"other", "TestKnown"}: true}, want: "no listed test"},
		{name: "marker too far", plan: "DONE V1-1", source: "package sample\n// Covers B1\n\n\n\n\nfunc TestKnown(){}", listed: map[testRef]bool{{".", "TestKnown"}: true}, want: "four lines"},
		{name: "line directive cannot move marker", plan: "DONE V1-1", source: "package sample\n// Covers B1\n\n\n\n//line sample_test.go:4\nfunc TestKnown(){}", listed: map[testRef]bool{{".", "TestKnown"}: true}, want: "four lines"},
		{name: "strict open row", plan: "ADD V1-10; REDIS", source: "package sample", strict: true, want: "B1 has no Covers"},
		{name: "open row", plan: "ADD V1-10; REDIS", source: "package sample"},
		{name: "valid tagged marker", plan: "DONE V1-1; REDIS", source: "//go:build conformance\n\npackage sample\n// Covers B1\nfunc TestKnown(){}", listed: map[testRef]bool{{".", "TestKnown"}: true}},
		{name: "free form marker", plan: "DEV", source: "package sample\n// Covers 1B-T1 (S,C).\nfunc TestKnown(){}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "docs/PARITY.md", "| B1 | "+tc.plan+" | D1 | fixture |\n")
			put(t, root, "sample_test.go", tc.source)
			for _, dir := range []string{"v2", "_examples", "child", "node_modules"} {
				if dir == "child" {
					put(t, root, dir+"/go.mod", "module child\n")
				}
				put(t, root, dir+"/ignored_test.go", "this must not be parsed")
			}
			err := checkParity(root, tc.strict, tc.listed)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
		})
	}
}

func put(t *testing.T, root, path, contents string) {
	t.Helper()
	path = filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

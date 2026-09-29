package socketio_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCompileContracts uses the active go toolchain, including GOTOOLCHAIN=go1.22.12.
// Each rejected fixture must fail at its deliberate invalid.go expression with
// all expected type-diagnostic fragments, not an import, toolchain or linker error.
func TestCompileContracts(t *testing.T) {
	run := func(t *testing.T, path string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		output, err := exec.CommandContext(ctx, "go", "test", "-run=^$", path).CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("compiler timed out: %v\n%s", ctx.Err(), output)
		}
		return string(output), err
	}
	t.Run("positive", func(t *testing.T) {
		if output, err := run(t, "./fixtures/positive"); err != nil {
			t.Fatalf("positive fixture: %v\n%s", err, output)
		}
	})
	entries, err := os.ReadDir("testdata/negative")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("negative fixtures missing")
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			t.Fatalf("unexpected fixture entry %s", entry.Name())
		}
		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join("testdata", "negative", entry.Name())
			want, err := os.ReadFile(filepath.Join(path, "diagnostic.txt"))
			if err != nil {
				t.Fatal(err)
			}
			output, err := run(t, "./"+filepath.ToSlash(path))
			if err == nil {
				t.Fatalf("invalid program compiled: %s", output)
			}
			if !strings.Contains(output, "invalid.go:") {
				t.Fatalf("failure not in fixture: %s", output)
			}
			for _, fragment := range strings.Split(strings.TrimSpace(string(want)), "\n") {
				if !strings.Contains(output, fragment) {
					t.Errorf("missing type diagnostic %q in:\n%s", fragment, output)
				}
			}
		})
	}
}

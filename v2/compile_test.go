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

// TestCompileContracts compiles the positive fixture and every negative one with the
// active go toolchain, which CI runs on Go 1.22 with GOTOOLCHAIN=local (job min-go).
// A negative fixture must fail inside its invalid.go with every fragment of its
// diagnostic.txt, so an import or toolchain failure alone cannot pass it.
func TestCompileContracts(t *testing.T) {
	build := func(t *testing.T, pkg string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		out, err := exec.CommandContext(ctx, "go", "build", pkg).CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("compiler timed out: %v\n%s", ctx.Err(), out)
		}
		return string(out), err
	}

	// The positive fixture runs first and alone: it fills the build cache with the root
	// package and its dependencies, so the parallel negative builds below do not all
	// compile them at once (a minute each on a cold Windows runner).
	t.Run("positive", func(t *testing.T) {
		for _, pkg := range []string{"./internal/fixtures/positive", "./internal/fixtures/externaladapter", "./internal/fixtures/clientstub"} {
			if out, err := build(t, pkg); err != nil {
				t.Fatalf("%s: %v\n%s", pkg, err, out)
			}
		}
	})

	entries, err := os.ReadDir(filepath.Join("testdata", "negative"))
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
		t.Run("negative/"+entry.Name(), func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join("testdata", "negative", entry.Name())
			want, err := os.ReadFile(filepath.Join(dir, "diagnostic.txt"))
			if err != nil {
				t.Fatal(err)
			}
			out, err := build(t, "./"+filepath.ToSlash(dir))
			if err == nil {
				t.Fatalf("invalid program compiled: %s", out)
			}
			if !strings.Contains(out, "invalid.go:") {
				t.Fatalf("failure is not in the fixture: %s", out)
			}
			// Checkouts with CRLF line endings leave a \r on every fragment.
			for _, fragment := range strings.Split(strings.TrimSpace(string(want)), "\n") {
				fragment = strings.TrimSpace(fragment)
				if !strings.Contains(out, fragment) {
					t.Errorf("missing diagnostic fragment %q in:\n%s", fragment, out)
				}
			}
		})
	}
}

package parity

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTypedConsumerFixtures(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	sums, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, failure string }{
		{"positive", ""}, {"wrong_payload", "cannot use"}, {"wrong_handler", "cannot use"},
		{"wrong_client_handler", "cannot use"}, {"wrong_client_role", "cannot use"},
		{"wrong_result", "cannot use"}, {"wrong_result_assignment", "cannot use"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			mod := strings.Replace(string(module), "module github.com/sshaplygin/go-socket.io", "module example.com/typedconsumer", 1)
			mod += "\nrequire github.com/sshaplygin/go-socket.io v0.0.0\nreplace github.com/sshaplygin/go-socket.io => " + strconv.Quote(root) + "\n"
			source, err := os.ReadFile(filepath.Join(root, "internal", "parity", "testdata", "typed", test.name+".go"))
			if err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string][]byte{"go.mod": []byte(mod), "go.sum": sums, "consumer.go": source} {
				if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "test", "-mod=readonly", ".")
			cmd.Dir = directory
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
			out, err := cmd.CombinedOutput()
			if test.failure == "" {
				if err != nil {
					t.Fatalf("typed consumer does not compile: %v\n%s", err, out)
				}
				return
			}
			if err == nil || !strings.Contains(string(out), test.failure) {
				t.Fatalf("expected payload/handler/result type rejection %q, got %v\n%s", test.failure, err, out)
			}
		})
	}
}

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

// The README quick start is the Go block between these markers. It must equal
// testdata/quickstart/main.go, which is compiled as an external consumer.
const (
	quickStartBegin = "<!-- quickstart:begin -->\n```go\n"
	quickStartEnd   = "```\n<!-- quickstart:end -->"
)

func TestREADMEQuickStart(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join("internal", "parity", "testdata", "quickstart", "main.go")
	source, err := os.ReadFile(filepath.Join(root, fixture))
	if err != nil {
		t.Fatal(err)
	}

	// A Windows checkout may convert both files to CRLF; compare them with LF line endings.
	text, code := strings.ReplaceAll(string(readme), "\r\n", "\n"), strings.ReplaceAll(string(source), "\r\n", "\n")
	if strings.Count(text, quickStartBegin) != 1 || strings.Count(text, quickStartEnd) != 1 {
		t.Fatalf("README.md must contain exactly one %q ... %q block", quickStartBegin, quickStartEnd)
	}
	_, block, _ := strings.Cut(text, quickStartBegin)
	block, _, _ = strings.Cut(block, quickStartEnd)
	if block != code {
		t.Fatalf("README.md quick start differs from %s; make them identical:\n%s", fixture, firstDifference(block, code))
	}

	directory := t.TempDir()
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	module := strings.Replace(string(mod), "module github.com/sshaplygin/go-socket.io", "module example.com/quickstart", 1)
	module += "\nrequire github.com/sshaplygin/go-socket.io v0.0.0\nreplace github.com/sshaplygin/go-socket.io => " + strconv.Quote(root) + "\n"
	sums, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"go.mod": []byte(module), "go.sum": sums, "main.go": source} {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "vet", "-mod=readonly", ".")
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("quick start does not compile or vet as a consumer: %v\n%s", err, out)
	}
}

// firstDifference reports the first line where the README block and the file differ.
func firstDifference(readme, file string) string {
	a, b := strings.SplitAfter(readme, "\n"), strings.SplitAfter(file, "\n")
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return "line " + strconv.Itoa(i+1) + ":\n  README.md: " + strconv.Quote(x) + "\n  file:      " + strconv.Quote(y)
		}
	}
	return "no line differs"
}

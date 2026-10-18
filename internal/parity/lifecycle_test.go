package parity

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This gate deliberately reads example modules and underscore directories too.
func TestNoProcessTermination(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	failures, err := processTermination(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("process-terminating references:\n%s", strings.Join(failures, "\n"))
	}
}

func processTermination(root string) ([]string, error) {
	var failures []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "v2" || entry.Name() == "node_modules" || entry.Name() == "vendor" || entry.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		references, err := terminatingReferences(path, source)
		failures = append(failures, references...)
		return err
	})
	if err != nil {
		return nil, err
	}
	readmePath := filepath.Join(root, "README.md")
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		return nil, err
	}
	blocks := strings.Split(strings.ReplaceAll(string(readme), "\r\n", "\n"), "```go\n")
	if len(blocks) == 1 {
		return nil, fmt.Errorf("README contains no Go quick start")
	}
	for i, block := range blocks[1:] {
		source, _, found := strings.Cut(block, "```")
		if !found {
			return nil, fmt.Errorf("unterminated README Go block %d", i+1)
		}
		references, err := terminatingReferences(fmt.Sprintf("%s:block%d", readmePath, i+1), []byte(source))
		if err != nil {
			return nil, err
		}
		failures = append(failures, references...)
	}
	return failures, nil
}

func terminatingReferences(name string, source []byte) ([]string, error) {
	positions := token.NewFileSet()
	file, err := parser.ParseFile(positions, name, source, 0)
	if err != nil {
		return nil, err
	}
	for _, imp := range file.Imports {
		// A dot import can hide any logger's terminating methods from selector checks.
		if imp.Name != nil && imp.Name.Name == "." {
			return nil, fmt.Errorf("%s: dot imports are not allowed in library/example source: %s", name, imp.Path.Value)
		}
	}
	var failures []string
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "Fatal", "Fatalf", "Fatalln", "Exit":
			// Check references too: assigning log.Fatal to a variable is also forbidden.
			failures = append(failures, fmt.Sprintf("%s: %s", positions.Position(selector.Pos()), selector.Sel.Name))
		}
		return true
	})
	return failures, nil
}

func TestProcessTerminationGate(t *testing.T) {
	for _, tc := range []struct {
		name, path, source string
		fail               bool
	}{
		{"library alias", "main.go", `package sample; import l "log"; func f(){l.Fatalf("failed")}`, true},
		{"example module", "_examples/sample/main.go", `package main; import "log"; func main(){log.Fatalln("failed")}`, true},
		{"engine underscore", "engineio/_examples/main.go", `package main; import "os"; func main(){os.Exit(1)}`, true},
		{"framework logger", "_examples/echo/main.go", `package main; func main(){app.Logger.Fatal(err)}`, true},
		{"saved function", "main.go", `package sample; import "log"; var die=log.Fatal`, true},
		{"test excluded", "sample_test.go", `package sample; func TestSample(){t.Fatal("failed")}`, false},
		{"tooling excluded", ".github/cli/main.go", `package main; import "os"; func main(){os.Exit(1)}`, false},
		{"v2 excluded", "v2/main.go", `package sample; import "log"; func f(){log.Fatal("failed")}`, false},
		{"comments and strings", "main.go", "package sample; // log.Fatal(err)\nvar text = `os.Exit(1)`", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "README.md", "```go\npackage main\nfunc main(){}\n```\n")
			put(t, root, tc.path, tc.source)
			failures, err := processTermination(root)
			if err != nil {
				t.Fatal(err)
			}
			if (len(failures) > 0) != tc.fail {
				t.Fatalf("failures=%v; want forbidden=%v", failures, tc.fail)
			}
		})
	}
	t.Run("README", func(t *testing.T) {
		root := t.TempDir()
		put(t, root, "README.md", "```go\npackage main\nimport \"log\"\nfunc main(){log.Fatal(\"failed\")}\n```\n")
		failures, err := processTermination(root)
		if err != nil || len(failures) != 1 {
			t.Fatalf("README gate: %v, %v", failures, err)
		}
	})
	t.Run("CRLF README", func(t *testing.T) {
		root := t.TempDir()
		put(t, root, "README.md", "```go\r\npackage main\r\nfunc main(){}\r\n```\r\n")
		failures, err := processTermination(root)
		if err != nil || len(failures) != 0 {
			t.Fatalf("valid CRLF README rejected: %v, %v", failures, err)
		}
	})
	t.Run("mixed README line endings", func(t *testing.T) {
		root := t.TempDir()
		put(t, root, "README.md", "```go\npackage main\nfunc main(){}\n```\n"+
			"```go\r\npackage main\r\nimport \"os\"\r\nfunc main(){os.Exit(1)}\r\n```\r\n")
		failures, err := processTermination(root)
		if err != nil || len(failures) != 1 || !strings.Contains(failures[0], "Exit") {
			t.Fatalf("CRLF block bypassed gate: %v, %v", failures, err)
		}
	})
	t.Run("malformed source", func(t *testing.T) {
		if _, err := terminatingReferences("bad.go", []byte("not Go")); err == nil {
			t.Fatal("malformed source passed")
		}
	})
	t.Run("dot import", func(t *testing.T) {
		for _, source := range []string{
			`package sample; import . "os"; func f(){Exit(1)}`,
			`package sample; import . "github.com/labstack/gommon/log"; func f(){Fatal("failed")}`,
		} {
			if _, err := terminatingReferences("dot.go", []byte(source)); err == nil {
				t.Fatalf("dot-imported termination API passed: %s", source)
			}
		}
	})
}

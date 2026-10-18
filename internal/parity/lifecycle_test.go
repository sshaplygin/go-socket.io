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

	"github.com/yuin/goldmark"
	mdast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
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
	blocks, err := readmeGoBlocks(string(readme))
	if err != nil {
		return nil, err
	}
	for i, block := range blocks {
		references, err := terminatingReferences(fmt.Sprintf("%s:block%d", readmePath, i+1), []byte(block))
		if err != nil {
			return nil, err
		}
		failures = append(failures, references...)
	}
	return failures, nil
}

// readmeGoBlocks follows Markdown block structure, including nested fences and
// invalid opener lines that must not hide a later Go block.
func readmeGoBlocks(readme string) ([]string, error) {
	source := []byte(strings.ReplaceAll(readme, "\r\n", "\n"))
	document := goldmark.New().Parser().Parse(text.NewReader(source))
	var blocks []string
	err := mdast.Walk(document, func(node mdast.Node, entering bool) (mdast.WalkStatus, error) {
		block, ok := node.(*mdast.FencedCodeBlock)
		if !entering || !ok || block.Info == nil {
			return mdast.WalkContinue, nil
		}
		fields := strings.Fields(string(block.Info.Text(source)))
		if len(fields) == 0 || (!strings.EqualFold(fields[0], "go") && !strings.EqualFold(fields[0], "golang")) {
			return mdast.WalkContinue, nil
		}
		var body strings.Builder
		for i := 0; i < block.Lines().Len(); i++ {
			line := block.Lines().At(i)
			body.Write(line.Value(source))
		}
		blocks = append(blocks, body.String())
		return mdast.WalkContinue, nil
	})
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("README contains no Go quick start")
	}
	return blocks, nil
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
	t.Run("Markdown Go fences", func(t *testing.T) {
		for _, fence := range []struct{ name, open, close string }{
			{"trailing spaces", "```go  ", "```"},
			{"tildes", "~~~go", "~~~"},
			{"long backticks", "````go", "`````"},
			{"long tildes", "~~~~go", "~~~~~"},
			{"indented", "   ```go", "   ```"},
			{"language spacing", "``` go\t", "```"},
			{"extra info", "```go linenos", "```"},
			{"golang", "```golang", "```"},
			{"case", "~~~Go", "~~~"},
		} {
			t.Run(fence.name, func(t *testing.T) {
				for _, forbidden := range []bool{false, true} {
					root := t.TempDir()
					body := "package main\nfunc main(){}\n"
					if forbidden {
						body = "package main\nimport \"os\"\nfunc main(){os.Exit(1)}\n"
					}
					put(t, root, "README.md", "```go\npackage main\nfunc main(){}\n```\n"+fence.open+"\n"+body+fence.close+"\n")
					failures, err := processTermination(root)
					if err != nil || (len(failures) != 0) != forbidden {
						t.Fatalf("forbidden=%v; failures=%v, err=%v", forbidden, failures, err)
					}
				}
			})
		}
	})
	t.Run("Markdown fence boundaries", func(t *testing.T) {
		for _, tc := range []struct {
			name, source string
			invalid      bool
		}{
			{"quote", "> ```go\n> package main\n> ```\n", false},
			{"list", "- ~~~go\n  package main\n  ~~~\n", false},
			{"unterminated", "~~~go\npackage main\n", false},
			{"wrong close", "~~~go\npackage main\n```\n", true},
			{"short close", "````go\npackage main\n```\n", true},
			{"non-Go block", "```text\n~~~go\nos.Exit(1)\n~~~\n```\n", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				root := t.TempDir()
				put(t, root, "README.md", "```go\npackage main\nfunc main(){}\n```\n"+tc.source)
				failures, err := processTermination(root)
				if (err != nil) != tc.invalid || len(failures) != 0 {
					t.Fatalf("invalid=%v; failures=%v, err=%v", tc.invalid, failures, err)
				}
			})
		}
	})
	t.Run("invalid openers cannot hide Go", func(t *testing.T) {
		for _, opener := range []string{"    ```text", "```text `"} {
			root := t.TempDir()
			put(t, root, "README.md", "```go\npackage main\nfunc main(){}\n```\n\n"+
				opener+"\n\n~~~go\npackage main\nimport \"os\"\nfunc main(){os.Exit(1)}\n~~~\n")
			failures, err := processTermination(root)
			if err != nil || len(failures) != 1 || !strings.Contains(failures[0], "Exit") {
				t.Fatalf("opener=%q; failures=%v, err=%v", opener, failures, err)
			}
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

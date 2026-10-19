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

func TestLibraryDoesNotTerminateProcess(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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
		failures, err := terminatingReferences(path, source)
		if err != nil {
			return err
		}
		for _, failure := range failures {
			t.Error(failure)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func terminatingReferences(name string, source []byte) ([]string, error) {
	positions := token.NewFileSet()
	file, err := parser.ParseFile(positions, name, source, 0)
	if err != nil {
		return nil, err
	}
	if file.Name.Name == "main" {
		return nil, nil
	}
	for _, imp := range file.Imports {
		if imp.Name != nil && imp.Name.Name == "." {
			return nil, fmt.Errorf("%s: dot import hides process-termination calls", name)
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
			failures = append(failures, fmt.Sprintf("%s: library references %s", positions.Position(selector.Pos()), selector.Sel.Name))
		}
		return true
	})
	return failures, nil
}

func TestProcessTerminationScope(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		fail         bool
	}{
		{"library", `package sample; import l "log"; func f(){l.Fatalf("failed")}`, true},
		{"saved function", `package sample; import "os"; var die=os.Exit`, true},
		{"executable", `package main; import "os"; func main(){os.Exit(1)}`, false},
		{"comments", "package sample; // log.Fatal(err)\nvar text = `os.Exit(1)`", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failures, err := terminatingReferences("fixture.go", []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			if (len(failures) != 0) != tc.fail {
				t.Fatalf("failures=%v; want forbidden=%v", failures, tc.fail)
			}
		})
	}
}

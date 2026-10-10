package main

import (
	"bytes"
	"fmt"
	"go/build/constraint"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
)

type lexeme struct {
	kind token.Token
	text string
}

func tokens(src []byte) ([]lexeme, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "source.go", src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	cgo := false
	for _, imp := range file.Imports {
		name, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		cgo = cgo || name == "C"
	}
	var scan scanner.Scanner
	var scanErr error
	scan.Init(fset.AddFile("tokens.go", -1, len(src)), src, func(pos token.Position, msg string) {
		scanErr = fmt.Errorf("%s: %s", pos, msg)
	}, scanner.ScanComments)
	var result []lexeme
	for {
		_, kind, text := scan.Scan()
		if kind == token.EOF {
			return result, scanErr
		}
		if kind == token.COMMENT && !cgo && !directive(text) {
			continue
		}
		if kind == token.SEMICOLON {
			text = ";"
		}
		result = append(result, lexeme{kind, text})
	}
}

func directive(text string) bool {
	return strings.HasPrefix(text, "//go:") ||
		constraint.IsPlusBuild(text) ||
		strings.HasPrefix(text, "//line ") ||
		strings.HasPrefix(text, "/*line ")
}

func relevant(name string) bool {
	if name == ".github/workflows/benchmarks.yml" || strings.HasPrefix(name, ".github/benchmarks/") {
		return true
	}
	for _, part := range strings.Split(path.Dir(name), "/") {
		if part != "." && (strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_")) {
			return false
		}
	}
	// Module files by base name: the v1 module at the root, the v2 module in v2/.
	switch path.Base(name) {
	case "go.mod", "go.sum", "go.work", "go.work.sum":
		return true
	}
	if strings.Contains("/"+name, "/testdata/") {
		return true
	}
	switch path.Ext(name) {
	case ".go", ".s", ".S", ".c", ".h", ".cc", ".cpp", ".cxx", ".m", ".mm", ".f", ".F", ".for", ".f90", ".syso", ".swig", ".swigcxx":
		return true
	}
	return false
}

func git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %v: %w: %s", args, err, stderr.String())
	}
	return out, nil
}

func codeChanged(dir, base, head string) (bool, error) {
	diff, err := git(dir, "diff", "--name-status", "--no-renames", "-z", base, head, "--")
	if err != nil {
		return false, err
	}
	fields := strings.Split(strings.TrimSuffix(string(diff), "\x00"), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		status, name := fields[i], fields[i+1]
		if !relevant(name) {
			continue
		}
		if status != "M" || path.Ext(name) != ".go" {
			return true, nil
		}
		before, err := git(dir, "show", base+":"+name)
		if err != nil {
			return false, err
		}
		after, err := git(dir, "show", head+":"+name)
		if err != nil {
			return false, err
		}
		oldTokens, oldErr := tokens(before)
		newTokens, newErr := tokens(after)
		// Invalid source must reach the build instead of being silently skipped.
		if oldErr != nil || newErr != nil || !slices.Equal(oldTokens, newTokens) {
			return true, nil
		}
	}
	return false, nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./.github/benchmarks BASE HEAD")
		os.Exit(1)
	}
	changed, err := codeChanged(".", os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("changed=%t\n", changed)
}

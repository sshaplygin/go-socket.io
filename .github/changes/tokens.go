package main

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

type lexeme struct {
	kind token.Token
	text string
}

// outputMarker is the start of the comment group that go test compares with the output of an
// Example function (the pattern of go/doc).
var outputMarker = regexp.MustCompile(`(?i)^[[:space:]]*(unordered )?output:`)

// tokens returns the token stream of a Go file without the comments that neither the
// compiler nor go test reads. Kept are //go: directives (//go:build included), // +build
// lines, //line directives, every comment of a file that imports "C" (the cgo preamble) and,
// in test files, every comment of a comment group inside the body of an Example function
// that starts with "Output:" or "Unordered output:": go test compares the output with the
// whole group. Comments that only linters read, such as //nolint, Deprecated: paragraphs or
// generated-file markers, are dropped; the classifier runs the lint job of the module for
// every change of comments or formatting.
func tokens(src []byte, test bool) ([]lexeme, error) {
	fset := token.NewFileSet()
	mode := parser.ImportsOnly
	if test {
		mode = parser.ParseComments | parser.SkipObjectResolution
	}
	file, err := parser.ParseFile(fset, "source.go", src, mode)
	if err != nil {
		return nil, err
	}
	examples := exampleOutputs(fset, file)
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
	scanned := fset.AddFile("tokens.go", -1, len(src))
	scan.Init(scanned, src, func(pos token.Position, msg string) {
		scanErr = fmt.Errorf("%s: %s", pos, msg)
	}, scanner.ScanComments)
	var result []lexeme
	for {
		pos, kind, text := scan.Scan()
		if kind == token.EOF {
			return result, scanErr
		}
		if kind == token.COMMENT && !cgo && !directive(text) && !examples.contains(scanned.Offset(pos)) {
			continue
		}
		if kind == token.SEMICOLON {
			text = ";"
		}
		result = append(result, lexeme{kind, text})
	}
}

// spans are byte ranges of a file, each [start, end).
type spans [][2]int

func (s spans) contains(offset int) bool {
	for _, r := range s {
		if offset >= r[0] && offset < r[1] {
			return true
		}
	}
	return false
}

// exampleOutputs returns the byte ranges of the comment groups that start with an output
// marker inside the bodies of the top-level functions whose name starts with Example. go
// test (go/doc) reads only the last comment group of a body, and only when it starts with
// the marker; every marker group is kept, so that moving the marker between groups also
// counts. A comment group added after the output group is therefore a change of comments
// only: it makes go vet fail ("output comment block must be the last comment block"), and
// make vet of the lint job reports it.
func exampleOutputs(fset *token.FileSet, file *ast.File) spans {
	var result spans
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Example") {
			continue
		}
		for _, group := range file.Comments {
			if group.Pos() < fn.Body.Lbrace || group.End() > fn.Body.Rbrace || !outputMarker.MatchString(group.Text()) {
				continue
			}
			result = append(result, [2]int{fset.Position(group.Pos()).Offset, fset.Position(group.End()).Offset})
		}
	}
	return result
}

// directive reports whether the compiler reads a comment outside a cgo preamble.
func directive(text string) bool {
	return strings.HasPrefix(text, "//go:") ||
		constraint.IsPlusBuild(text) ||
		strings.HasPrefix(text, "//line ") ||
		strings.HasPrefix(text, "/*line ")
}

// constraints returns the build constraint lines of a Go file: //go:build and // +build
// comments before the package clause.
func constraints(src []byte) []string {
	var lines []string
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			break
		}
		if constraint.IsGoBuild(line) || constraint.IsPlusBuild(line) {
			lines = append(lines, line)
		}
	}
	return lines
}

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

// generated is the marker that makes linters skip a file.
var generated = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

// deprecated is a line that opens a Deprecated: paragraph, which staticcheck (SA1019,
// enabled by checks: all in both .golangci.yml) reads to flag every use of the
// declaration. It matches a // comment and any line of a /* */ comment.
var deprecated = regexp.MustCompile(`(?m)^(//|/\*)?[[:space:]]*Deprecated:`)

// tokens returns the token stream of a Go file without the comments that no tool reads.
// Directives, cgo preambles, linter suppressions, generated-file markers, lines opening a
// Deprecated: paragraph and, in test files, every comment inside the body of an Example
// function are kept: go test compares the output with the whole last comment group of the
// body, and every line after "// Output:" is part of it.
func tokens(src []byte, test bool) ([]lexeme, error) {
	fset := token.NewFileSet()
	mode := parser.ImportsOnly
	if test {
		mode = parser.SkipObjectResolution
	}
	file, err := parser.ParseFile(fset, "source.go", src, mode)
	if err != nil {
		return nil, err
	}
	examples := exampleBodies(fset, file)
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
		if kind == token.COMMENT && !cgo && !kept(text) && !examples.contains(scanned.Offset(pos)) {
			continue
		}
		if kind == token.SEMICOLON {
			text = ";"
		}
		result = append(result, lexeme{kind, text})
	}
}

// kept reports whether a comment is read by a tool.
func kept(text string) bool {
	return directive(text) || deprecated.MatchString(text)
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

// exampleBodies returns the byte ranges of the bodies of the top-level functions whose
// name starts with Example; go test runs them and compares their output comments.
func exampleBodies(fset *token.FileSet, file *ast.File) spans {
	var result spans
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Example") {
			continue
		}
		result = append(result, [2]int{fset.Position(fn.Body.Lbrace).Offset, fset.Position(fn.Body.Rbrace).Offset + 1})
	}
	return result
}

func directive(text string) bool {
	return strings.HasPrefix(text, "//go:") ||
		constraint.IsPlusBuild(text) ||
		strings.HasPrefix(text, "//line ") ||
		strings.HasPrefix(text, "/*line ") ||
		strings.HasPrefix(text, "//nolint") ||
		strings.HasPrefix(text, "//lint:") ||
		generated.MatchString(text)
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

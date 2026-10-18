package main

import (
	"fmt"
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

// exampleOutput is the comment go test compares with the output of an Example function.
var exampleOutput = regexp.MustCompile(`(?i)^//[[:space:]]*(unordered )?output:`)

// generated is the marker that makes linters skip a file.
var generated = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

// tokens returns the token stream of a Go file without the comments that no tool reads.
// Directives, cgo preambles, linter suppressions, generated-file markers and, in test
// files, Example output comments are kept.
func tokens(src []byte, test bool) ([]lexeme, error) {
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
		if kind == token.COMMENT && !cgo && !kept(text, test) {
			continue
		}
		if kind == token.SEMICOLON {
			text = ";"
		}
		result = append(result, lexeme{kind, text})
	}
}

// kept reports whether a comment is read by a tool.
func kept(text string, test bool) bool {
	return directive(text) || (test && exampleOutput.MatchString(text))
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

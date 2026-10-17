// Command coverage accounts for calls to empty functions in a cover -func summary.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type block struct{ startLine, startCol, endLine, endCol int }

func emptyCalls(profile io.Reader, root, module string) (map[string]bool, error) {
	files := make(map[string][]block)
	scanner := bufio.NewScanner(profile)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 1 && strings.HasPrefix(fields[0], "mode:") {
			continue
		}
		if len(fields) == 2 && fields[0] == "mode:" {
			continue
		}
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid coverage profile line: %q", scanner.Text())
		}
		statements, err := strconv.Atoi(fields[1])
		if err != nil || statements < 0 {
			return nil, fmt.Errorf("invalid statement count: %q", fields[1])
		}
		count, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || count < 0 {
			return nil, fmt.Errorf("invalid execution count: %q", fields[2])
		}
		if statements != 0 || count == 0 {
			continue
		}
		split := strings.LastIndexByte(fields[0], ':')
		if split < 0 {
			return nil, fmt.Errorf("invalid block location: %q", fields[0])
		}
		file, location := fields[0][:split], fields[0][split+1:]
		var b block
		if n, err := fmt.Sscanf(location, "%d.%d,%d.%d", &b.startLine, &b.startCol, &b.endLine, &b.endCol); err != nil || n != 4 {
			return nil, fmt.Errorf("invalid block extent: %q", location)
		}
		files[file] = append(files[file], b)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	called := make(map[string]bool)
	for file, blocks := range files {
		rel, ok := strings.CutPrefix(file, module+"/")
		if !ok {
			return nil, fmt.Errorf("profile file outside module: %q", file)
		}
		clean := filepath.Clean(filepath.FromSlash(rel))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("invalid source path: %q", rel)
		}
		fset := token.NewFileSet()
		source, err := parser.ParseFile(fset, filepath.Join(root, clean), nil, 0)
		if err != nil {
			return nil, err
		}
		occurrences := make(map[string]int)
		for _, decl := range source.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				key := fmt.Sprintf("%s:%d: %s", file, fset.Position(fn.Pos()).Line, fn.Name.Name)
				occurrences[key]++
			}
		}
		for _, decl := range source.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || len(fn.Body.List) != 0 {
				continue
			}
			key := fmt.Sprintf("%s:%d: %s", file, fset.Position(fn.Pos()).Line, fn.Name.Name)
			if occurrences[key] != 1 {
				continue
			}
			start, end := fset.Position(fn.Body.Lbrace), fset.Position(fn.Body.Rbrace)
			for _, b := range blocks {
				startsInside := b.startLine > start.Line || b.startLine == start.Line && b.startCol > start.Column
				endsInside := b.endLine < end.Line || b.endLine == end.Line && b.endCol <= end.Column+1
				if startsInside && endsInside {
					called[key] = true
					break
				}
			}
		}
	}
	return called, nil
}

func annotate(profile, summary io.Reader, output io.Writer, root, module string) error {
	called, err := emptyCalls(profile, root, module)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(summary)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[2] == "0.0%" && called[fields[0]+" "+fields[1]] {
			line = fmt.Sprintf("%s\t%s\t100.0%%", fields[0], fields[1])
		}
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func run() error {
	profilePath := flag.String("profile", "", "coverage profile")
	summaryPath := flag.String("summary", "", "go tool cover -func summary")
	root := flag.String("root", ".", "module source directory")
	module := flag.String("module", "", "module import path")
	flag.Parse()
	if *profilePath == "" || *summaryPath == "" || *module == "" {
		return fmt.Errorf("profile, summary and module are required")
	}
	profile, err := os.Open(*profilePath)
	if err != nil {
		return err
	}
	defer func() { _ = profile.Close() }()
	summary, err := os.Open(*summaryPath)
	if err != nil {
		return err
	}
	defer func() { _ = summary.Close() }()
	return annotate(profile, summary, os.Stdout, *root, *module)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cover-check:", err)
		os.Exit(1)
	}
}

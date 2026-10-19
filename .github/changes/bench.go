package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// testDecls is what one test file declares at the top level: each key is a function,
// type, variable or constant name (a method counts as its receiver type), each value the
// identifiers its declaration refers to. roots are the declarations that run during a
// benchmark binary without being referenced: Benchmark functions, TestMain, init and
// package-level variables.
type testDecls struct {
	refs  map[string]map[string]bool
	roots []string
	bench bool
}

func receiver(expr ast.Expr) string {
	for {
		switch e := expr.(type) {
		case *ast.StarExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.IndexListExpr:
			expr = e.X
		case *ast.Ident:
			return e.Name
		default:
			return ""
		}
	}
}

// parseTestDecls parses a test file. A file that does not parse yields an error; the caller
// then treats it as touching benchmarks.
func parseTestDecls(src []byte) (testDecls, error) {
	result := testDecls{refs: map[string]map[string]bool{}}
	file, err := parser.ParseFile(token.NewFileSet(), "x_test.go", src, parser.SkipObjectResolution)
	if err != nil {
		return result, err
	}
	add := func(name string, node ast.Node) {
		set := result.refs[name]
		if set == nil {
			set = map[string]bool{}
			result.refs[name] = set
		}
		ast.Inspect(node, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				set[id.Name] = true
			}
			return true
		})
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = receiver(d.Recv.List[0].Type)
			} else if strings.HasPrefix(name, "Benchmark") || name == "TestMain" || name == "init" {
				result.roots = append(result.roots, name)
				result.bench = result.bench || strings.HasPrefix(name, "Benchmark")
			}
			add(name, d)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add(s.Name.Name, s)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						add(n.Name, s)
						if d.Tok == token.VAR {
							result.roots = append(result.roots, n.Name)
						}
					}
				}
			}
		}
	}
	return result, nil
}

// benchReach parses the test files of dir and returns whether the package has a Benchmark
// function and the names of the test declarations a benchmark binary reaches.
func benchReach(dir string) (bool, map[string]bool, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return false, nil, err
	}
	refs := map[string]map[string]bool{}
	var roots []string
	bench := false
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			return false, nil, err
		}
		decls, err := parseTestDecls(src)
		if err != nil {
			// Unparsable test source: assume every declaration is reached.
			return true, nil, nil
		}
		bench = bench || decls.bench
		roots = append(roots, decls.roots...)
		for name, set := range decls.refs {
			if refs[name] == nil {
				refs[name] = map[string]bool{}
			}
			for ref := range set {
				refs[name][ref] = true
			}
		}
	}
	if !bench {
		return false, map[string]bool{}, nil
	}
	reach := map[string]bool{}
	for len(roots) > 0 {
		name := roots[len(roots)-1]
		roots = roots[:len(roots)-1]
		if reach[name] {
			continue
		}
		reach[name] = true
		for ref := range refs[name] {
			if _, declared := refs[ref]; declared && !reach[ref] {
				roots = append(roots, ref)
			}
		}
	}
	return true, reach, nil
}

// touchesBench reports whether one version of a changed test file declares a Benchmark
// function or a declaration that the benchmarks of its package reach (reach nil: all).
func touchesBench(src []byte, reach map[string]bool) bool {
	if src == nil {
		return false
	}
	decls, err := parseTestDecls(src)
	if err != nil || decls.bench || reach == nil {
		return true
	}
	for name := range decls.refs {
		if reach[name] {
			return true
		}
	}
	return false
}

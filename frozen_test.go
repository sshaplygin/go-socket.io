package socketio_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// unresolvedMarker matches the wording that marks a declaration or a document as not
// settled. G2 requires none of it in the frozen contract (docs/ROADMAP.md, 2.0).
var unresolvedMarker = regexp.MustCompile(`(?i)\bTODO\b|\bFIXME\b|placeholder (any|handler)|\bproposed\b|\bunreviewed\b|not yet frozen|open before g2`)

// frozenGoFiles are the files that hold the declarations frozen at G2: the whole root
// package, and the Engine.IO and parser files of the contract.
func frozenGoFiles(t *testing.T) []string {
	t.Helper()
	root, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range root {
		if !strings.HasSuffix(f, "_test.go") {
			files = append(files, f)
		}
	}
	return append(files, "engineio/hooks.go", "engineio/options.go", "parser/value.go")
}

// TestFrozenContract is the G2 check that no unresolved marker remains: neither
// docs/API.md nor any comment of a frozen file carries one, and no exported function,
// method, interface method or function-typed field takes or returns a bare any (the
// placeholder handler). The only any in the contract is the variadic payload of
// Adapter.ServerSideEmit, which ROADMAP 2.2 fixes.
func TestFrozenContract(t *testing.T) {
	doc, err := os.ReadFile("docs/API.md")
	if err != nil {
		t.Fatal(err)
	}
	if m := unresolvedMarker.FindString(string(doc)); m != "" {
		t.Errorf("docs/API.md carries an unresolved marker %q", m)
	}

	fset := token.NewFileSet()
	for _, name := range frozenGoFiles(t) {
		file, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, group := range file.Comments {
			if m := unresolvedMarker.FindString(group.Text()); m != "" {
				t.Errorf("%s: comment carries an unresolved marker %q", fset.Position(group.Pos()), m)
			}
		}
		check := func(what string, ft *ast.FuncType) {
			if ft == nil || what == "ServerSideEmit" {
				return
			}
			for _, list := range []*ast.FieldList{ft.Params, ft.Results} {
				if list == nil {
					continue
				}
				for _, field := range list.List {
					ast.Inspect(field.Type, func(n ast.Node) bool {
						switch x := n.(type) {
						case *ast.Ident:
							if x.Name == "any" {
								t.Errorf("%s: %s uses any", fset.Position(x.Pos()), what)
							}
						case *ast.InterfaceType:
							if len(x.Methods.List) == 0 {
								t.Errorf("%s: %s uses interface{}", fset.Position(x.Pos()), what)
							}
						}
						return true
					})
				}
			}
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.IsExported() {
					check(d.Name.Name, d.Type)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || !ts.Name.IsExported() {
						continue
					}
					ast.Inspect(ts.Type, func(n ast.Node) bool {
						if f, ok := n.(*ast.Field); ok {
							if ft, ok := f.Type.(*ast.FuncType); ok {
								name := ts.Name.Name
								if len(f.Names) > 0 {
									name = f.Names[0].Name
								}
								check(name, ft)
							}
						}
						return true
					})
				}
			}
		}
	}
}

package socketio_test

import (
	"fmt"
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

// anyUses returns one message per place of file where an exported declaration of the
// contract uses a bare any or interface{} in a position a handler or hook signature
// would: the parameters and results of an exported function or method, of an exported
// func type (RawHandler, Middleware, AdapterFactory), of a func-typed field or interface
// method of an exported type, and the type of an exported struct field. Type
// parameter constraints (Event[T any]) are not signatures and are not examined, and
// Adapter.ServerSideEmit, whose variadic payload ROADMAP 2.2 fixes, is skipped.
func anyUses(fset *token.FileSet, file *ast.File) []string {
	var out []string
	scan := func(what string, n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if x.Name == "any" {
					out = append(out, fmt.Sprintf("%s: %s uses any", fset.Position(x.Pos()), what))
				}
			case *ast.InterfaceType:
				if len(x.Methods.List) == 0 {
					out = append(out, fmt.Sprintf("%s: %s uses interface{}", fset.Position(x.Pos()), what))
				}
			}
			return true
		})
	}
	signature := func(what string, ft *ast.FuncType) {
		if ft == nil || what == "ServerSideEmit" {
			return
		}
		for _, list := range []*ast.FieldList{ft.Params, ft.Results} {
			if list != nil {
				for _, field := range list.List {
					scan(what, field.Type)
				}
			}
		}
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name.IsExported() {
				signature(d.Name.Name, d.Type)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || !ts.Name.IsExported() {
					continue
				}
				if ft, ok := ts.Type.(*ast.FuncType); ok {
					signature(ts.Name.Name, ft)
					continue
				}
				_, isStruct := ts.Type.(*ast.StructType)
				ast.Inspect(ts.Type, func(n ast.Node) bool {
					f, ok := n.(*ast.Field)
					if !ok {
						return true
					}
					name := ts.Name.Name
					if len(f.Names) > 0 {
						name = ts.Name.Name + "." + f.Names[0].Name
					}
					if ft, ok := f.Type.(*ast.FuncType); ok {
						if len(f.Names) > 0 {
							name = f.Names[0].Name
						}
						signature(name, ft)
						return false
					}
					if isStruct && len(f.Names) > 0 && f.Names[0].IsExported() {
						scan(name, f.Type)
					}
					return true
				})
			}
		}
	}
	return out
}

// TestFrozenContract is the G2 check that no unresolved marker remains: neither
// docs/API.md nor any comment of a frozen file carries one, and no exported function,
// method, func type, func-typed field, interface method or struct field of a frozen
// file uses a bare any (the placeholder handler); anyUses lists exactly what is
// examined. The only any in the contract is the variadic payload of
// Adapter.ServerSideEmit, which ROADMAP 2.2 fixes. Unexported declarations, type
// parameter constraints and function bodies are not examined.
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
		for _, msg := range anyUses(fset, file) {
			t.Error(msg)
		}
	}
}

// TestAnyUsesDetects keeps the any check able to fail: each case is a declaration the
// gate is about, and the clean cases are the shapes that must stay allowed.
func TestAnyUsesDetects(t *testing.T) {
	const head = "package p\n\nimport \"context\"\n\nvar _ context.Context\n\n"
	cases := []struct {
		name, src string
		want      int
	}{
		{"named func type parameter", "type RawHandler func(context.Context, any) error", 1},
		{"named func type result", "type Factory func() (any, error)", 1},
		{"func type with interface{}", "type H func(interface{}) error", 1},
		{"exported struct field", "type Opts struct{ Payload any }", 1},
		{"exported struct field in a map", "type Opts struct{ Payload map[string]any }", 1},
		{"func-typed field", "type Opts struct{ On func(any) }", 1},
		{"interface method", "type A interface{ M(args any) }", 1},
		{"exported function", "func F(v any) {}", 1},
		{"exported method result", "type T struct{}\n\nfunc (T) M() any { return nil }", 1},
		{"clean func type", "type H func(context.Context, string) error", 0},
		{"unexported field", "type Opts struct{ payload any }", 0},
		{"unexported func type", "type h func(any)", 0},
		{"type parameter constraint", "type E[T any] struct{ name string }\n\nfunc N[T any](n string) E[T] { return E[T]{name: n} }", 0},
		{"ServerSideEmit payload", "type A interface{ ServerSideEmit(context.Context, string, ...any) error }", 0},
	}
	for _, c := range cases {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "case.go", head+c.src+"\n", 0)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := anyUses(fset, file); len(got) != c.want {
			t.Errorf("%s: %d findings, want %d: %v", c.name, len(got), c.want, got)
		}
	}
}

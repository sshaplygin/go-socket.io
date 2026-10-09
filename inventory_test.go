package socketio_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestInventoryListsEveryExportedSignature keeps docs/API.md complete: every exported
// function, method and type of the root package must appear in the inventory by name
// (methods as Type.Method), so a declaration added without an inventory row fails.
func TestInventoryListsEveryExportedSignature(t *testing.T) {
	raw, err := os.ReadFile("docs/API.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, ok := pkgs["socketio"]
	if !ok {
		t.Fatal("package socketio not found")
	}
	check := func(name string) {
		if !strings.Contains(doc, "`"+name) {
			t.Errorf("docs/API.md does not list %s", name)
		}
	}
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() {
					continue
				}
				if d.Recv == nil {
					check(d.Name.Name)
					continue
				}
				check(receiverName(d.Recv.List[0].Type) + "." + d.Name.Name)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							check(s.Name.Name)
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								check(n.Name)
							}
						}
					}
				}
			}
		}
	}
}

func receiverName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return receiverName(x.X)
	case *ast.IndexExpr:
		return receiverName(x.X)
	case *ast.IndexListExpr:
		return receiverName(x.X)
	case *ast.Ident:
		return x.Name
	}
	return "?"
}

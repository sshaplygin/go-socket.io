package socketio_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"regexp"
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

// inventoryRow returns the third column of the inventory row whose second column lists
// key as a code span, or "" when there is none.
func inventoryRow(doc, key string) string {
	for _, line := range strings.Split(doc, "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 5 || !strings.HasPrefix(strings.TrimSpace(cells[1]), "`") {
			continue
		}
		for _, span := range strings.Split(cells[2], ",") {
			if strings.Trim(strings.TrimSpace(span), "`") == key {
				return strings.Join(cells[3:len(cells)-1], "|")
			}
		}
	}
	return ""
}

// resultsText prints the result list the way the inventory writes it: a single
// unnamed result bare, several parenthesised, none as "".
func resultsText(ft *ast.FuncType) string {
	if ft.Results == nil || len(ft.Results.List) == 0 {
		return ""
	}
	var parts []string
	for _, f := range ft.Results.List {
		n := len(f.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			parts = append(parts, types.ExprString(f.Type))
		}
	}
	if len(parts) == 1 && len(ft.Results.List[0].Names) == 0 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// checkResults fails when the inventory text sig documents the method or function name
// with other result types than the declaration. Parameters are not compared: the
// inventory abbreviates context.Context to ctx and drops parameter names.
func checkResults(t *testing.T, sig, owner, name string, ft *ast.FuncType) {
	t.Helper()
	want := resultsText(ft)
	if want == "" {
		return
	}
	loc := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `[\[(]`).FindStringIndex(sig)
	if loc == nil {
		t.Errorf("docs/API.md row of %s does not show %s(...)", owner, name)
		return
	}
	i := loc[1] - 1
	for _, pair := range [][2]byte{{'[', ']'}, {'(', ')'}} {
		if i >= len(sig) || sig[i] != pair[0] {
			continue
		}
		depth := 0
		for ; i < len(sig); i++ {
			if sig[i] == pair[0] {
				depth++
			} else if sig[i] == pair[1] {
				depth--
				if depth == 0 {
					i++
					break
				}
			}
		}
	}
	got := strings.TrimSpace(sig[i:])
	if !strings.HasPrefix(got, want) {
		t.Errorf("docs/API.md row of %s: %s returns %s in the declaration, the inventory has %q",
			owner, name, want, strings.TrimSpace(sig[loc[0]:min(len(sig), i+len(want)+8)]))
	}
}

// TestInventoryResultTypesMatchDeclarations compares the result types of every
// documented function, method and interface method with the AST. The first column of
// checks is the part of a signature a consumer cannot guess from the name.
func TestInventoryResultTypesMatchDeclarations(t *testing.T) {
	raw, err := os.ReadFile("docs/API.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	if strings.Contains(doc, "`Args`") {
		t.Errorf("docs/API.md abbreviates parser.Arguments as Args; write the declared type")
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range pkgs["socketio"].Files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if !d.Name.IsExported() {
					continue
				}
				key := d.Name.Name
				if d.Recv != nil {
					key = receiverName(d.Recv.List[0].Type) + "." + key
				}
				sig := inventoryRow(doc, key)
				if sig == "" {
					t.Errorf("docs/API.md has no row for %s", key)
					continue
				}
				checkResults(t, sig, key, d.Name.Name, d.Type)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || !ts.Name.IsExported() {
						continue
					}
					it, ok := ts.Type.(*ast.InterfaceType)
					if !ok {
						continue
					}
					sig := inventoryRow(doc, ts.Name.Name)
					if sig == "" {
						continue
					}
					for _, m := range it.Methods.List {
						ft, ok := m.Type.(*ast.FuncType)
						if !ok || len(m.Names) == 0 || !m.Names[0].IsExported() {
							continue
						}
						checkResults(t, sig, ts.Name.Name, m.Names[0].Name, ft)
					}
				}
			}
		}
	}
}

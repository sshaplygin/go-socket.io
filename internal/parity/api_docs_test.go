package parity

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Public role contracts must be declared in their public package. Aliases to
// implementation packages compile but hide their methods from go doc.
func TestPublicRoleDocumentation(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []struct {
		path    string
		types   []string
		members []string
	}{
		{".", []string{"Conn", "Namespace", "Emitter", "Handshake", "AckFunc"},
			[]string{"Conn.Close", "Namespace.Emit", "Emitter.Emit", "Handshake.Headers"}},
		{"client", []string{"Conn", "Namespace", "Emitter", "Socket", "AckFunc"},
			[]string{"Conn.Close", "Namespace.Emit", "Emitter.Emit", "Socket.OnConnect", "Socket.Emit"}},
	} {
		t.Run(role.path, func(t *testing.T) {
			packages, err := parser.ParseDir(token.NewFileSet(), filepath.Join(root, role.path), func(info os.FileInfo) bool {
				return !strings.HasSuffix(info.Name(), "_test.go")
			}, 0)
			if err != nil {
				t.Fatal(err)
			}
			declarations := make(map[string]*ast.TypeSpec)
			methods := make(map[string][]string)
			for _, pkg := range packages {
				for _, file := range pkg.Files {
					for _, decl := range file.Decls {
						switch decl := decl.(type) {
						case *ast.GenDecl:
							for _, spec := range decl.Specs {
								if spec, ok := spec.(*ast.TypeSpec); ok {
									declarations[spec.Name.Name] = spec
								}
							}
						case *ast.FuncDecl:
							if decl.Recv == nil || !decl.Name.IsExported() {
								continue
							}
							receiver := decl.Recv.List[0].Type
							if pointer, ok := receiver.(*ast.StarExpr); ok {
								receiver = pointer.X
							}
							if name, ok := receiver.(*ast.Ident); ok {
								methods[name.Name] = append(methods[name.Name], decl.Name.Name)
							}
						}
					}
				}
			}
			// Fixed probes also catch wrappers whose members disappear from this AST.
			queries := make(map[string]bool)
			for _, member := range role.members {
				queries[member] = true
			}
			for _, name := range role.types {
				spec := declarations[name]
				if spec == nil || spec.Assign.IsValid() {
					t.Errorf("%s must be defined in its public package", name)
					continue
				}
				symbols := []string{name}
				var fields *ast.FieldList
				switch typ := spec.Type.(type) {
				case *ast.InterfaceType:
					fields = typ.Methods
				case *ast.StructType:
					fields = typ.Fields
				case *ast.FuncType:
				default:
					t.Errorf("%s must expose its declaration rather than delegate to another type", name)
					continue
				}
				if fields != nil {
					for _, field := range fields.List {
						for _, member := range field.Names {
							if member.IsExported() {
								symbols = append(symbols, name+"."+member.Name)
							}
						}
					}
				}
				for _, method := range methods[name] {
					symbols = append(symbols, name+"."+method)
				}
				for _, symbol := range symbols {
					queries[symbol] = true
				}
			}
			symbols := make([]string, 0, len(queries))
			for symbol := range queries {
				symbols = append(symbols, symbol)
			}
			sort.Strings(symbols)
			for _, symbol := range symbols {
				t.Run(symbol, func(t *testing.T) {
					cmd := exec.Command("go", "doc", ".", symbol)
					cmd.Dir = filepath.Join(root, role.path)
					cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off")
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("public documentation unavailable: %v\n%s", err, out)
					}
				})
			}
		})
	}
}

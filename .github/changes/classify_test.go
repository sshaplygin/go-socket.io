package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fixture is a two-module repository shaped like this one: the v1 module at the root with
// a server root package over engineio over parser, a leaf adapter, tag-only conformance
// tests, the parity gate and examples; the v2 module in v2/ with examples and experiments.
var fixture = map[string]string{
	"go.mod":                                   "module example.com/m\n\ngo 1.22\n",
	"server.go":                                "package m\n\nimport \"example.com/m/engineio\"\n\nfunc Serve() int { return engineio.Open() }\n",
	"server_test.go":                           "package m\n\nimport \"testing\"\n\nfunc TestServe(t *testing.T) { _ = Serve() }\n",
	"engineio/engine.go":                       "package engineio\n\nimport \"example.com/m/parser\"\n\n// Open opens.\nfunc Open() int { return parser.Parse(\"x\") }\n",
	"engineio/errors.go":                       "package engineio\n\n// ErrX is shared with v2.\nvar ErrX = 1\n",
	"parser/parser.go":                         "package parser\n\n// Parse parses.\nfunc Parse(s string) int {\n\treturn len(s)\n}\n",
	"parser/parser_test.go":                    "package parser\n\nimport \"testing\"\n\nfunc TestParse(t *testing.T) {\n\tif Parse(\"ab\") != 2 {\n\t\tt.Fatal(\"len\")\n\t}\n}\n",
	"parser/bench_test.go":                     "package parser\n\nimport \"testing\"\n\nfunc BenchmarkParse(b *testing.B) {\n\tfor i := 0; i < b.N; i++ {\n\t\tParse(input())\n\t}\n}\n",
	"parser/helper_test.go":                    "package parser\n\nfunc input() string { return \"abc\" }\n",
	"adapter/adapter.go":                       "package adapter\n\nfunc Broadcast() int { return 1 }\n",
	"adapter/adapter_test.go":                  "package adapter\n\nimport \"testing\"\n\nfunc TestBroadcast(t *testing.T) { _ = Broadcast() }\n",
	"internal/conformance/conformance_test.go": "//go:build conformance\n\npackage conformance\n\nimport (\n\t\"testing\"\n\n\tm \"example.com/m\"\n)\n\nfunc TestInterop(t *testing.T) { _ = m.Serve() }\n",
	"internal/parity/parity_test.go":           "package parity\n\nimport \"testing\"\n\n// Covers B1\nfunc TestB1(t *testing.T) {}\n",
	"_examples/app/go.mod":                     "module example.com/app\n\ngo 1.22\n",
	"_examples/app/main.go":                    "package main\n\n// main runs.\nfunc main() {}\n",
	"_examples/app/chat.go":                    "package main\n\n// chat is a shared copy.\nfunc chat() {}\n",
	"Makefile":                                 "test:\n\tgo test ./...\n",
	"README.md":                                "# m\n",
	"CHANGELOG.md":                             "# Changelog\n",
	"docs/PARITY.md":                           "| B1 | DONE | x |\n",
	"docs/API.md":                              "# API\n",
	"LICENSE":                                  "license\n",
	".github/sync-manifest":                    "engineio/errors.go v2/engineio/errors.go\n",
	".github/workflows/ci.yaml":                "name: CI\n",
	".github/workflows/benchmarks.yml":         "name: Benchmarks\n",
	".github/dependabot.yml":                   "version: 2\n",
	"v2/go.mod":                                "module example.com/m/v2\n\ngo 1.22\n",
	"v2/socket.go":                             "package socketio\n\nimport \"example.com/m/v2/engineio\"\n\n// Server is frozen.\ntype Server struct{ c engineio.Conn }\n",
	"v2/socket_test.go":                        "package socketio\n\nimport \"testing\"\n\nfunc TestServer(t *testing.T) { _ = Server{} }\n",
	"v2/frozen_test.go":                        frozenFixture,
	"v2/engineio/conn.go":                      "package engineio\n\n// Conn is a connection.\ntype Conn struct{ ID int }\n",
	"v2/engineio/options.go":                   "package engineio\n\n// Options are frozen.\ntype Options struct{ N int }\n",
	"v2/engineio/errors.go":                    "package engineio\n\n// ErrX is shared with v2.\nvar ErrX = 1\n",
	"v2/parser/parser.go":                      "package parser\n\nfunc Decode() {}\n",
	"v2/_examples/x/go.mod":                    "module example.com/x\n\ngo 1.22\n",
	"v2/_examples/x/chat.go":                   "package main\n\nfunc chat() {}\n",
	"v2/_examples/x/main.go":                   "package main\n\nfunc main() {}\n",
	"v2/_experiments/e/go.mod":                 "module example.com/e\n\ngo 1.22\n",
	"v2/_experiments/e/e.go":                   "package e\n\nfunc E() {}\n",
}

// frozenFixture has the shape of frozenGoFiles in v2/frozen_test.go.
const frozenFixture = "package socketio\n\nimport (\n\t\"path/filepath\"\n\t\"strings\"\n\t\"testing\"\n)\n\n" +
	"func frozenGoFiles(t *testing.T) []string {\n\troot, _ := filepath.Glob(\"*.go\")\n\tvar files []string\n" +
	"\tfor _, f := range root {\n\t\tif !strings.HasSuffix(f, \"_test.go\") {\n\t\t\tfiles = append(files, f)\n\t\t}\n\t}\n" +
	"\treturn append(files, \"engineio/hooks.go\", \"engineio/options.go\", \"parser/value.go\")\n}\n"

// none is the decision of a change no job reads.
var none = map[string]string{
	"v1": "false", "v1_packages": "", "gates_v1": "false", "examples_v1": "false",
	"conformance": "false", "bench_v1": "", "v2": "false", "v2_packages": "",
	"gates_v2": "false", "examples_v2": "false", "experiments_v2": "false", "bench_v2": "",
	"sync": "false", "changed": "false",
}

func with(changes map[string]string) map[string]string {
	result := map[string]string{}
	for k, v := range none {
		result[k] = v
	}
	for k, v := range changes {
		result[k] = v
	}
	return result
}

var fullV1 = map[string]string{
	"v1": "true", "v1_packages": "./...", "gates_v1": "true", "examples_v1": "true",
	"conformance": "true",
}

func merge(maps ...map[string]string) map[string]string {
	result := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return with(result)
}

const deleted = "\x00delete"

func TestClassify(t *testing.T) {
	tests := []struct {
		name  string
		edit  map[string]string // path -> new content; deleted removes the file
		moves [][2]string
		want  map[string]string
	}{
		{name: "empty diff", want: none},
		{name: "comment in a package", edit: map[string]string{
			"parser/parser.go": "package parser\n\n// Parse parses a string.\n// It counts bytes.\nfunc Parse(s string) int {\n\treturn len(s) // bytes\n}\n",
		}, want: none},
		{name: "formatting only", edit: map[string]string{
			"parser/parser.go": "package parser\n// Parse parses.\nfunc Parse(s string)int{\n\treturn len( s )\n\n}\n",
		}, want: none},
		{name: "function body of a leaf package", edit: map[string]string{
			"adapter/adapter.go": "package adapter\n\nfunc Broadcast() int { return 2 }\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": "./adapter", "gates_v1": "true", "examples_v1": "true", "conformance": "true",
		})},
		{name: "package imported by the server root", edit: map[string]string{
			"parser/parser.go": "package parser\n\n// Parse parses.\nfunc Parse(s string) int {\n\treturn len(s) + 1\n}\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": ". ./engineio ./parser", "gates_v1": "true", "examples_v1": "true",
			"conformance": "true", "bench_v1": "./parser", "changed": "true",
		})},
		{name: "deprecation comment", edit: map[string]string{
			"parser/parser.go": "package parser\n\n// Parse parses.\n//\n// Deprecated: use Decode.\nfunc Parse(s string) int {\n\treturn len(s)\n}\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": ". ./engineio ./parser", "gates_v1": "true", "examples_v1": "true",
			"conformance": "true", "bench_v1": "./parser", "changed": "true",
		})},
		{name: "server root", edit: map[string]string{
			"server.go": "package m\n\nimport \"example.com/m/engineio\"\n\nfunc Serve() int { return engineio.Open() + 1 }\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": ".", "gates_v1": "true", "examples_v1": "true", "conformance": "true",
		})},
		{name: "test that no benchmark reaches", edit: map[string]string{
			"parser/parser_test.go": "package parser\n\nimport \"testing\"\n\nfunc TestParse(t *testing.T) {\n\tif Parse(\"abc\") != 3 {\n\t\tt.Fatal(\"len\")\n\t}\n}\n",
		}, want: with(map[string]string{"v1": "true", "v1_packages": "./parser", "gates_v1": "true"})},
		{name: "helper that a benchmark reaches", edit: map[string]string{
			"parser/helper_test.go": "package parser\n\nfunc input() string { return \"abcd\" }\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": "./parser", "gates_v1": "true", "bench_v1": "./parser", "changed": "true",
		})},
		{name: "conformance test", edit: map[string]string{
			"internal/conformance/conformance_test.go": "//go:build conformance\n\npackage conformance\n\nimport (\n\t\"testing\"\n\n\tm \"example.com/m\"\n)\n\nfunc TestInterop(t *testing.T) { _ = m.Serve() + 1 }\n",
		}, want: with(map[string]string{"gates_v1": "true", "conformance": "true"})},
		{name: "Covers marker comment", edit: map[string]string{
			"internal/parity/parity_test.go": "package parity\n\nimport \"testing\"\n\n// Covers B2\nfunc TestB1(t *testing.T) {}\n",
		}, want: with(map[string]string{"gates_v1": "true"})},
		{name: "example code", edit: map[string]string{
			"_examples/app/main.go": "package main\n\n// main runs.\nfunc main() { println() }\n",
		}, want: with(map[string]string{"examples_v1": "true", "conformance": "true", "gates_v1": "true"})},
		{name: "example comment", edit: map[string]string{
			"_examples/app/main.go": "package main\n\n// main runs the example.\nfunc main() {}\n",
		}, want: none},
		{name: "comment in a byte-compared example copy", edit: map[string]string{
			"_examples/app/chat.go": "package main\n\n// chat is a shared copy, edited.\nfunc chat() {}\n",
		}, want: with(map[string]string{"examples_v1": "true", "conformance": "true"})},
		{name: "example module file", edit: map[string]string{
			"_examples/app/go.mod": "module example.com/app\n\ngo 1.23\n",
		}, want: with(map[string]string{"examples_v1": "true", "conformance": "true"})},
		{name: "Markdown only", edit: map[string]string{"CHANGELOG.md": "# Changelog\n\n- x\n"}, want: none},
		{name: "Markdown read by the parity gate", edit: map[string]string{"docs/PARITY.md": "| B1 | DONE | y |\n"},
			want: with(map[string]string{"gates_v1": "true"})},
		{name: "Markdown read by the v2 gates", edit: map[string]string{"docs/API.md": "# API\n\nmore\n"},
			want: with(map[string]string{"gates_v2": "true"})},
		{name: "module file", edit: map[string]string{"go.mod": "module example.com/m\n\ngo 1.23\n"},
			want: merge(fullV1, map[string]string{"bench_v1": "./...", "changed": "true"})},
		{name: "build constraint", edit: map[string]string{
			"adapter/adapter.go": "//go:build linux\n\npackage adapter\n\nfunc Broadcast() int { return 1 }\n",
		}, want: merge(fullV1, map[string]string{"bench_v1": "./...", "changed": "true"})},
		{name: "test fixture", edit: map[string]string{"parser/testdata/in.txt": "x\n"},
			want: merge(fullV1, map[string]string{"bench_v1": "./...", "changed": "true"})},
		{name: "unclassified file", edit: map[string]string{"LICENSE": "new license\n"},
			want: merge(fullV1, map[string]string{"bench_v1": "./...", "changed": "true"})},
		{name: "Makefile", edit: map[string]string{"Makefile": "test:\n\tgo test -count=1 ./...\n"},
			want: merge(fullV1, map[string]string{"sync": "true"})},
		{name: "deleted package", edit: map[string]string{"adapter/adapter.go": deleted, "adapter/adapter_test.go": deleted},
			want: merge(fullV1, map[string]string{"bench_v1": "./...", "changed": "true"})},
		{name: "deleted file of a package", edit: map[string]string{"engineio/errors.go": deleted, "v2/engineio/errors.go": deleted, ".github/sync-manifest": "\n"},
			want: with(map[string]string{
				"v1": "true", "v1_packages": ". ./engineio", "gates_v1": "true", "examples_v1": "true", "conformance": "true",
				"v2": "true", "v2_packages": ". ./engineio", "gates_v2": "true", "sync": "true",
			})},
		{name: "renamed file", moves: [][2]string{{"adapter/adapter.go", "adapter/broadcast.go"}},
			want: with(map[string]string{
				"v1": "true", "v1_packages": "./adapter", "gates_v1": "true", "examples_v1": "true", "conformance": "true",
			})},
		{name: "comment in a shared file", edit: map[string]string{
			"engineio/errors.go": "package engineio\n\n// ErrX is shared.\nvar ErrX = 1\n",
		}, want: with(map[string]string{"sync": "true"})},
		{name: "ci workflow", edit: map[string]string{".github/workflows/ci.yaml": "name: CI\non: push\n"},
			want: merge(fullV1, map[string]string{
				"v2": "true", "v2_packages": "./...", "gates_v2": "true", "examples_v2": "true", "experiments_v2": "true", "sync": "true",
			})},
		{name: "benchmark workflow", edit: map[string]string{".github/workflows/benchmarks.yml": "name: Benchmarks\non: push\n"},
			want: with(map[string]string{"bench_v1": "./...", "bench_v2": "./...", "changed": "true"})},
		{name: "dependabot", edit: map[string]string{".github/dependabot.yml": "version: 2\nupdates: []\n"}, want: none},
		{name: "v2 package", edit: map[string]string{
			"v2/engineio/conn.go": "package engineio\n\n// Conn is a connection.\ntype Conn struct{ ID, N int }\n",
		}, want: with(map[string]string{"v2": "true", "v2_packages": ". ./engineio", "gates_v2": "true"})},
		{name: "v2 leaf package", edit: map[string]string{"v2/parser/parser.go": "package parser\n\nfunc Decode() { println() }\n"},
			want: with(map[string]string{"v2": "true", "v2_packages": "./parser", "gates_v2": "true"})},
		{name: "v2 frozen comment", edit: map[string]string{
			"v2/socket.go": "package socketio\n\nimport \"example.com/m/v2/engineio\"\n\n// Server is frozen: TBD.\ntype Server struct{ c engineio.Conn }\n",
		}, want: with(map[string]string{"gates_v2": "true"})},
		{name: "v2 frozen comment outside the root", edit: map[string]string{
			"v2/engineio/options.go": "package engineio\n\n// Options are frozen. TODO: settle.\ntype Options struct{ N int }\n",
		}, want: with(map[string]string{"gates_v2": "true"})},
		{name: "v2 comment outside the frozen files", edit: map[string]string{
			"v2/engineio/conn.go": "package engineio\n\n// Conn is a connection. TODO: rename.\ntype Conn struct{ ID int }\n",
		}, want: none},
		{name: "v2 comment in a root test file", edit: map[string]string{
			"v2/socket_test.go": "package socketio\n\nimport \"testing\"\n\n// TestServer builds a Server.\nfunc TestServer(t *testing.T) { _ = Server{} }\n",
		}, want: none},
		{name: "v2 example copy", edit: map[string]string{"v2/_examples/x/chat.go": "package main\n\nfunc chat() { println() }\n"},
			want: with(map[string]string{"examples_v2": "true"})},
		{name: "v2 experiment", edit: map[string]string{"v2/_experiments/e/e.go": "package e\n\nfunc E() { println() }\n"},
			want: with(map[string]string{"experiments_v2": "true"})},
		{name: "v2 module file", edit: map[string]string{"v2/go.mod": "module example.com/m/v2\n\ngo 1.23\n"},
			want: with(map[string]string{
				"v2": "true", "v2_packages": "./...", "gates_v2": "true", "examples_v2": "true", "experiments_v2": "true",
				"bench_v2": "./...", "changed": "true",
			})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			run := func(args ...string) string {
				t.Helper()
				out, err := git(dir, args...)
				if err != nil {
					t.Fatal(err)
				}
				return strings.TrimSpace(string(out))
			}
			run("init", "-q")
			run("config", "user.name", "Classifier Test")
			run("config", "user.email", "test@example.invalid")
			run("config", "commit.gpgsign", "false")
			write := func(files map[string]string) {
				t.Helper()
				for name, content := range files {
					file := filepath.Join(dir, filepath.FromSlash(name))
					if content == deleted {
						if err := os.Remove(file); err != nil {
							t.Fatal(err)
						}
						continue
					}
					if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				run("add", "-A")
				run("commit", "-qm", "fixture", "--allow-empty")
			}
			write(fixture)
			base := run("rev-parse", "HEAD")
			for _, move := range tt.moves {
				run("mv", move[0], move[1])
			}
			write(tt.edit)
			got := decide(t, dir, base)
			for key, want := range tt.want {
				if got[key] != want {
					t.Errorf("%s = %q, want %q", key, got[key], want)
				}
			}
			if len(got) != len(none) {
				t.Errorf("outputs %v, want the keys of %v", got, none)
			}
		})
	}
}

func decide(t *testing.T, dir, base string) map[string]string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-C", dir, base, "HEAD"}, &stdout, &stderr); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	t.Log(stderr.String())
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		key, value, _ := strings.Cut(line, "=")
		got[key] = value
	}
	return got
}

func TestRunErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{{}, {"one"}, {"-full", "a", "b"}, {"-C", t.TempDir(), "missing", "HEAD"}} {
		if err := run(args, &stdout, &stderr); err == nil {
			t.Errorf("run(%q) succeeded", args)
		}
	}
}

func TestFull(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-full"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	want := "v1=true\nv1_packages=./...\ngates_v1=true\nexamples_v1=true\nconformance=true\nbench_v1=./...\n" +
		"v2=true\nv2_packages=./...\ngates_v2=true\nexamples_v2=true\nexperiments_v2=true\nbench_v2=./...\n" +
		"sync=true\nchanged=true\n"
	if stdout.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", stdout.String(), want)
	}
}

func TestPattern(t *testing.T) {
	for p, ok := range map[string]bool{
		".": true, "./...": true, "./engineio/transport": true, "./a-b/c_d.e": true,
		"": false, "x": false, "./$(id)": false, "./a b": false, "../x": false, "./a;b": false,
	} {
		if pattern.MatchString(p) != ok {
			t.Errorf("pattern(%q) = %t, want %t", p, !ok, ok)
		}
	}
}

// TestFrozenPatternsOfRepository reads frozenGoFiles of the real v2/frozen_test.go: it
// fails when the function changes shape so that the classifier no longer finds the frozen
// files, or names a file that does not exist.
func TestFrozenPatternsOfRepository(t *testing.T) {
	root := filepath.Join("..", "..")
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(frozenSource)))
	if err != nil {
		t.Fatal(err)
	}
	list := frozenPatterns(src)
	d := &decision{frozen: list}
	for _, rel := range []string{"server.go", "engineio/hooks.go", "engineio/options.go", "parser/value.go"} {
		if !d.frozenFile(rel) {
			t.Errorf("frozenPatterns = %q, does not cover %s", list, rel)
		}
	}
	for _, rel := range []string{"server_test.go", "engineio/conn.go", "parser/parser.go"} {
		if d.frozenFile(rel) {
			t.Errorf("frozenPatterns = %q, covers %s", list, rel)
		}
	}
	for _, p := range list {
		matches, err := filepath.Glob(filepath.Join(root, "v2", filepath.FromSlash(p)))
		if err != nil || len(matches) == 0 {
			t.Errorf("frozenGoFiles names %s, which matches no file of v2/", p)
		}
	}
}

func TestFrozenFallback(t *testing.T) {
	for _, src := range []string{"", "package x\n", "not go"} {
		if list := frozenPatterns([]byte(src)); list != nil {
			t.Errorf("frozenPatterns(%q) = %q, want nil", src, list)
		}
	}
	d := &decision{}
	if !d.frozenFile("engineio/conn.go") || d.frozenFile("x_test.go") {
		t.Error("without patterns every non-test file must count as frozen")
	}
}

// TestGateInputsCoverRepository fails when a Go file of the repository names a Markdown
// file that gateInputs does not list: a Markdown-only change to it would skip its reader.
func TestGateInputsCoverRepository(t *testing.T) {
	known := map[string]bool{}
	for name := range gateInputs {
		known[path.Base(name)] = true
	}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules":
				return filepath.SkipDir
			}
			if file == filepath.Join(root, ".github", "changes") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(file, ".go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil // fixtures that must not parse
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err == nil && strings.HasSuffix(value, ".md") && !known[path.Base(value)] {
				t.Errorf("%s reads %s, which gateInputs does not list", file, value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

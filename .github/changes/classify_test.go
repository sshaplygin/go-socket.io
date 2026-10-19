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
	"slices"
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
	"adapter/example_test.go":                  "package adapter\n\nimport \"fmt\"\n\nfunc Example() {\n\tfmt.Println(\"hello\")\n\t// Output:\n\t// hello\n}\n",
	"adapter/embed.go":                         "package adapter\n\nimport _ \"embed\"\n\n//go:embed adapter.go\nvar source string\n",
	"native/native.go":                         "package native\n\n/*\n#define ANSWER 42\n*/\nimport \"C\"\n\nfunc Answer() int { return 42 }\n",
	"internal/conformance/conformance_test.go": "//go:build conformance\n\npackage conformance\n\nimport (\n\t\"testing\"\n\n\tm \"example.com/m\"\n)\n\nfunc TestInterop(t *testing.T) { _ = m.Serve() }\n",
	"internal/parity/parity_test.go":           "package parity\n\nimport \"testing\"\n\n// Covers B1\nfunc TestB1(t *testing.T) {}\n",
	"_examples/app/go.mod":                     "module example.com/app\n\ngo 1.22\n",
	"_examples/app/main.go":                    "package main\n\n// main runs.\nfunc main() {}\n",
	"_examples/app/chat.go":                    "package main\n\n// chat is a shared copy.\nfunc chat() {}\n",
	"engineio/_examples/main.go":               "package main\n\n// main echoes.\nfunc main() {}\n",
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

// lintV1 and lintV2 are the decisions of a change of Go comments or formatting only.
var (
	lintV1 = with(map[string]string{"v1": "true"})
	lintV2 = with(map[string]string{"v2": "true"})
)

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

var fullV2 = map[string]string{
	"v2": "true", "v2_packages": "./...", "gates_v2": "true", "examples_v2": "true", "experiments_v2": "true",
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
		}, want: lintV1},
		{name: "formatting only", edit: map[string]string{
			"parser/parser.go": "package parser\n// Parse parses.\nfunc Parse(s string)int{\n\treturn len( s )\n\n}\n",
		}, want: lintV1},
		// gates_v1 stays false when packages are selected: make cover-check of lint-v1 then
		// tests every package, internal/parity included.
		{name: "function body of a leaf package", edit: map[string]string{
			"adapter/adapter.go": "package adapter\n\nfunc Broadcast() int { return 2 }\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": "./adapter", "examples_v1": "true", "conformance": "true",
		})},
		{name: "package imported by the server root", edit: map[string]string{
			"parser/parser.go": "package parser\n\n// Parse parses.\nfunc Parse(s string) int {\n\treturn len(s) + 1\n}\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": ". ./engineio ./parser", "examples_v1": "true",
			"conformance": "true", "bench_v1": "./parser", "changed": "true",
		})},
		// Comments that linters read run lint only.
		{name: "deprecation comment", edit: map[string]string{
			"parser/parser.go": "package parser\n\n// Parse parses.\n//\n// Deprecated: use Decode.\nfunc Parse(s string) int {\n\treturn len(s)\n}\n",
		}, want: lintV1},
		{name: "directive with a space (SA9009)", edit: map[string]string{
			"parser/parser.go": "package parser\n\n// Parse parses.\n//\n// go:noinline\nfunc Parse(s string) int {\n\treturn len(s)\n}\n",
		}, want: lintV1},
		{name: "linter suppression", edit: map[string]string{
			"adapter/adapter.go": "package adapter\n\nfunc Broadcast() int { return 1 } //nolint:revive\n",
		}, want: lintV1},
		{name: "linter suppression with a space", edit: map[string]string{
			"adapter/adapter.go": "package adapter\n\nfunc Broadcast() int { return 1 } // nolint:revive\n",
		}, want: lintV1},
		{name: "generated-file marker", edit: map[string]string{
			"adapter/adapter.go": "// Code generated by hand. DO NOT EDIT.\n\npackage adapter\n\nfunc Broadcast() int { return 1 }\n",
		}, want: lintV1},
		{name: "v2 linter suppression", edit: map[string]string{
			"v2/parser/parser.go": "package parser\n\nfunc Decode() {} // nolint:unused\n",
		}, want: lintV2},
		// Comments that the compiler reads are code.
		{name: "compiler directive", edit: map[string]string{
			"parser/parser.go": "package parser\n\n// Parse parses.\n//\n//go:noinline\nfunc Parse(s string) int {\n\treturn len(s)\n}\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": ". ./engineio ./parser", "examples_v1": "true",
			"conformance": "true", "bench_v1": "./parser", "changed": "true",
		})},
		{name: "line directive", edit: map[string]string{
			"adapter/adapter.go": "package adapter\n\n//line broadcast.go:10\nfunc Broadcast() int { return 1 }\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": "./adapter", "examples_v1": "true", "conformance": "true",
		})},
		{name: "block line directive", edit: map[string]string{
			"adapter/adapter.go": "package adapter\n\n/*line broadcast.go:10*/\nfunc Broadcast() int { return 1 }\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": "./adapter", "examples_v1": "true", "conformance": "true",
		})},
		{name: "embed pattern", edit: map[string]string{
			"adapter/embed.go": "package adapter\n\nimport _ \"embed\"\n\n//go:embed *.go\nvar source string\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": "./adapter", "examples_v1": "true", "conformance": "true",
		})},
		{name: "cgo preamble comment", edit: map[string]string{
			"native/native.go": "package native\n\n/*\n#define ANSWER 43\n*/\nimport \"C\"\n\nfunc Answer() int { return 42 }\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": "./native", "examples_v1": "true", "conformance": "true",
		})},
		{name: "legacy build constraint", edit: map[string]string{
			"adapter/adapter.go": "// +build linux\n\npackage adapter\n\nfunc Broadcast() int { return 1 }\n",
		}, want: merge(fullV1, map[string]string{"bench_v1": "./...", "changed": "true"})},
		{name: "server root", edit: map[string]string{
			"server.go": "package m\n\nimport \"example.com/m/engineio\"\n\nfunc Serve() int { return engineio.Open() + 1 }\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": ".", "examples_v1": "true", "conformance": "true",
		})},
		{name: "test that no benchmark reaches", edit: map[string]string{
			"parser/parser_test.go": "package parser\n\nimport \"testing\"\n\nfunc TestParse(t *testing.T) {\n\tif Parse(\"abc\") != 3 {\n\t\tt.Fatal(\"len\")\n\t}\n}\n",
		}, want: with(map[string]string{"v1": "true", "v1_packages": "./parser"})},
		{name: "helper that a benchmark reaches", edit: map[string]string{
			"parser/helper_test.go": "package parser\n\nfunc input() string { return \"abcd\" }\n",
		}, want: with(map[string]string{
			"v1": "true", "v1_packages": "./parser", "bench_v1": "./parser", "changed": "true",
		})},
		{name: "expected output of an example", edit: map[string]string{
			"adapter/example_test.go": "package adapter\n\nimport \"fmt\"\n\nfunc Example() {\n\tfmt.Println(\"hello\")\n\t// Output:\n\t// goodbye\n}\n",
		}, want: with(map[string]string{"v1": "true", "v1_packages": "./adapter"})},
		{name: "unordered output of an example", edit: map[string]string{
			"adapter/example_test.go": "package adapter\n\nimport \"fmt\"\n\nfunc Example() {\n\tfmt.Println(\"hello\")\n\t// Unordered output:\n\t// hello\n}\n",
		}, want: with(map[string]string{"v1": "true", "v1_packages": "./adapter"})},
		{name: "comment before an example", edit: map[string]string{
			"adapter/example_test.go": "package adapter\n\nimport \"fmt\"\n\n// Example prints a greeting.\nfunc Example() {\n\tfmt.Println(\"hello\")\n\t// Output:\n\t// hello\n}\n",
		}, want: lintV1},
		{name: "comment of an example body outside its output", edit: map[string]string{
			"adapter/example_test.go": "package adapter\n\nimport \"fmt\"\n\nfunc Example() {\n\t// Print a greeting.\n\tfmt.Println(\"hello\")\n\t// Output:\n\t// hello\n}\n",
		}, want: lintV1},
		// go test reads only the last comment group of the body, so this group makes the
		// example lose its expected output. It is a change of comments only on purpose: go
		// vet rejects it ("output comment block must be the last comment block"), and make
		// vet of lint-v1 reports it.
		{name: "comment group after the output of an example", edit: map[string]string{
			"adapter/example_test.go": "package adapter\n\nimport \"fmt\"\n\nfunc Example() {\n\tfmt.Println(\"hello\")\n\t// Output:\n\t// hello\n\n\t// Printed once.\n}\n",
		}, want: lintV1},
		{name: "conformance test", edit: map[string]string{
			"internal/conformance/conformance_test.go": "//go:build conformance\n\npackage conformance\n\nimport (\n\t\"testing\"\n\n\tm \"example.com/m\"\n)\n\nfunc TestInterop(t *testing.T) { _ = m.Serve() + 1 }\n",
		}, want: with(map[string]string{"gates_v1": "true", "conformance": "true"})},
		// A comment that go vet rejects (a +build word before the package clause) in a file
		// that only a tag or a GOOS compiles: lint runs, and make vet of lint-v1 vets the
		// file under that tag.
		{name: "comment in a conformance test", edit: map[string]string{
			"internal/conformance/conformance_test.go": "//go:build conformance\n\n// Run with -tags conformance, not a +build line.\n\npackage conformance\n\nimport (\n\t\"testing\"\n\n\tm \"example.com/m\"\n)\n\nfunc TestInterop(t *testing.T) { _ = m.Serve() }\n",
		}, want: lintV1},
		{name: "Covers marker comment", edit: map[string]string{
			"internal/parity/parity_test.go": "package parity\n\nimport \"testing\"\n\n// Covers B2\nfunc TestB1(t *testing.T) {}\n",
		}, want: with(map[string]string{"v1": "true", "gates_v1": "true"})},
		// Every Go change of a v1 example also runs lint-v1: make vet vets the examples, and
		// make vuln scans the example modules.
		{name: "example code", edit: map[string]string{
			"_examples/app/main.go": "package main\n\n// main runs.\nfunc main() { println() }\n",
		}, want: with(map[string]string{"v1": "true", "examples_v1": "true", "conformance": "true", "gates_v1": "true"})},
		{name: "example comment", edit: map[string]string{
			"_examples/app/main.go": "package main\n\n// main runs the example.\nfunc main() {}\n",
		}, want: lintV1},
		{name: "example header comment that go vet rejects", edit: map[string]string{
			"_examples/app/main.go": "// The example needs no +build line.\n\npackage main\n\n// main runs.\nfunc main() {}\n",
		}, want: lintV1},
		{name: "Engine.IO example comment", edit: map[string]string{
			"engineio/_examples/main.go": "package main\n\n// main echoes packets.\nfunc main() {}\n",
		}, want: lintV1},
		{name: "comment in a byte-compared example copy", edit: map[string]string{
			"_examples/app/chat.go": "package main\n\n// chat is a shared copy, edited.\nfunc chat() {}\n",
		}, want: with(map[string]string{"v1": "true", "examples_v1": "true", "conformance": "true"})},
		// make vuln of the lint job scans the example and experiment modules.
		{name: "example module file", edit: map[string]string{
			"_examples/app/go.mod": "module example.com/app\n\ngo 1.23\n",
		}, want: with(map[string]string{"v1": "true", "examples_v1": "true", "conformance": "true"})},
		{name: "example module checksums", edit: map[string]string{
			"_examples/app/go.sum": "example.com/dep v1.0.0 h1:x=\n",
		}, want: with(map[string]string{"v1": "true", "examples_v1": "true", "conformance": "true"})},
		{name: "Markdown only", edit: map[string]string{"CHANGELOG.md": "# Changelog\n\n- x\n"}, want: none},
		{name: "Markdown read by the parity gate", edit: map[string]string{"docs/PARITY.md": "| B1 | DONE | y |\n"},
			want: none},
		{name: "Markdown read by the v2 gates", edit: map[string]string{"docs/API.md": "# API\n\nmore\n"},
			want: none},
		{name: "Markdown read by a gate with a Go comment", edit: map[string]string{
			"README.md":        "# m\n\nmore\n",
			"parser/parser.go": "package parser\n\n// Parse parses a string.\nfunc Parse(s string) int {\n\treturn len(s)\n}\n",
		}, want: lintV1},
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
		{name: "deleted package", edit: map[string]string{"adapter/adapter.go": deleted, "adapter/adapter_test.go": deleted, "adapter/example_test.go": deleted, "adapter/embed.go": deleted},
			want: merge(fullV1, map[string]string{"bench_v1": "./...", "changed": "true"})},
		{name: "deleted file of a package", edit: map[string]string{"engineio/errors.go": deleted, "v2/engineio/errors.go": deleted, ".github/sync-manifest": "\n"},
			want: with(map[string]string{
				"v1": "true", "v1_packages": ". ./engineio", "examples_v1": "true", "conformance": "true",
				"v2": "true", "v2_packages": ". ./engineio", "sync": "true",
			})},
		{name: "renamed file", moves: [][2]string{{"adapter/adapter.go", "adapter/broadcast.go"}},
			want: with(map[string]string{
				"v1": "true", "v1_packages": "./adapter", "examples_v1": "true", "conformance": "true",
			})},
		{name: "comment in a shared file", edit: map[string]string{
			"engineio/errors.go": "package engineio\n\n// ErrX is shared.\nvar ErrX = 1\n",
		}, want: with(map[string]string{"v1": "true", "sync": "true"})},
		{name: "ci workflow", edit: map[string]string{".github/workflows/ci.yaml": "name: CI\non: push\n"},
			want: merge(fullV1, map[string]string{
				"v2": "true", "v2_packages": "./...", "gates_v2": "true", "examples_v2": "true", "experiments_v2": "true", "sync": "true",
			})},
		{name: "benchmark workflow", edit: map[string]string{".github/workflows/benchmarks.yml": "name: Benchmarks\non: push\n"},
			want: with(map[string]string{"bench_v1": "./...", "bench_v2": "./...", "changed": "true"})},
		{name: "classifier", edit: map[string]string{".github/changes/main.go": "package main\n\nfunc main() {}\n"},
			want: merge(fullV1, fullV2, map[string]string{"bench_v1": "./...", "bench_v2": "./...", "changed": "true", "sync": "true"})},
		{name: "other file under .github", edit: map[string]string{".github/ISSUE_TEMPLATE/bug.yml": "name: Bug\n"},
			want: merge(fullV1, fullV2, map[string]string{"bench_v1": "./...", "bench_v2": "./...", "changed": "true", "sync": "true"})},
		{name: "architecture checker", edit: map[string]string{".github/architecture/arch.go": "package main\n\nfunc main() {}\n"},
			want: merge(fullV1)},
		{name: "coverage checker", edit: map[string]string{".github/coverage/cover.go": "package main\n\nfunc main() {}\n"},
			want: merge(fullV1)},
		{name: "v1 lint config", edit: map[string]string{".golangci.yml": "version: \"2\"\n"},
			want: merge(fullV1)},
		{name: "v2 lint config", edit: map[string]string{"v2/.golangci.yml": "version: \"2\"\n"},
			want: merge(fullV2)},
		{name: "v2 Makefile", edit: map[string]string{"v2/Makefile": "test:\n\tgo test ./...\n"},
			want: merge(fullV2)},
		{name: "assembly source", edit: map[string]string{"parser/parse_amd64.s": "#include \"textflag.h\"\n"},
			want: with(map[string]string{
				"v1": "true", "v1_packages": ". ./engineio ./parser", "examples_v1": "true",
				"conformance": "true", "bench_v1": "./parser", "changed": "true",
			})},
		{name: "C source", edit: map[string]string{"adapter/native.c": "int answer(void) { return 42; }\n"},
			want: with(map[string]string{
				"v1": "true", "v1_packages": "./adapter", "examples_v1": "true", "conformance": "true",
			})},
		{name: "dependabot", edit: map[string]string{".github/dependabot.yml": "version: 2\nupdates: []\n"}, want: none},
		// gates_v2 is false when the root package is selected (test-v2 runs the gate tests)
		// and true for a leaf package that the root does not import.
		{name: "v2 package", edit: map[string]string{
			"v2/engineio/conn.go": "package engineio\n\n// Conn is a connection.\ntype Conn struct{ ID, N int }\n",
		}, want: with(map[string]string{"v2": "true", "v2_packages": ". ./engineio"})},
		{name: "v2 leaf package", edit: map[string]string{"v2/parser/parser.go": "package parser\n\nfunc Decode() { println() }\n"},
			want: with(map[string]string{"v2": "true", "v2_packages": "./parser", "gates_v2": "true"})},
		{name: "v2 frozen comment", edit: map[string]string{
			"v2/socket.go": "package socketio\n\nimport \"example.com/m/v2/engineio\"\n\n// Server is frozen: TBD.\ntype Server struct{ c engineio.Conn }\n",
		}, want: with(map[string]string{"v2": "true", "gates_v2": "true"})},
		{name: "v2 frozen comment outside the root", edit: map[string]string{
			"v2/engineio/options.go": "package engineio\n\n// Options are frozen. TODO: settle.\ntype Options struct{ N int }\n",
		}, want: with(map[string]string{"v2": "true", "gates_v2": "true"})},
		{name: "v2 comment outside the frozen files", edit: map[string]string{
			"v2/engineio/conn.go": "package engineio\n\n// Conn is a connection. TODO: rename.\ntype Conn struct{ ID int }\n",
		}, want: lintV2},
		{name: "v2 comment in a root test file", edit: map[string]string{
			"v2/socket_test.go": "package socketio\n\nimport \"testing\"\n\n// TestServer builds a Server.\nfunc TestServer(t *testing.T) { _ = Server{} }\n",
		}, want: lintV2},
		{name: "v2 example copy", edit: map[string]string{"v2/_examples/x/chat.go": "package main\n\nfunc chat() { println() }\n"},
			want: with(map[string]string{"examples_v2": "true"})},
		// A code change of an experiment also runs lint-v2: make vuln scans the experiment
		// modules, and what reaches a vulnerable symbol depends on the code.
		{name: "v2 experiment", edit: map[string]string{"v2/_experiments/e/e.go": "package e\n\nfunc E() { println() }\n"},
			want: with(map[string]string{"v2": "true", "experiments_v2": "true"})},
		{name: "v2 experiment file added", edit: map[string]string{"v2/_experiments/e/f.go": "package e\n\nfunc F() {}\n"},
			want: with(map[string]string{"v2": "true", "experiments_v2": "true"})},
		{name: "v2 experiment module file", edit: map[string]string{"v2/_experiments/e/go.mod": "module example.com/e\n\ngo 1.23\n"},
			want: with(map[string]string{"v2": "true", "experiments_v2": "true"})},
		{name: "v2 example module file", edit: map[string]string{"v2/_examples/x/go.mod": "module example.com/x\n\ngo 1.23\n"},
			want: with(map[string]string{"examples_v2": "true"})},
		{name: "v2 experiment comment", edit: map[string]string{"v2/_experiments/e/e.go": "package e\n\n// E runs. nolint:unused\nfunc E() {}\n"},
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
// file that gateInputs does not list: the job summary would not name the gate that a
// Markdown-only change to it skips. Paths are compared relative to the repository root.
// It also fails when no Go file names an entry of gateInputs any more.
func TestGateInputsCoverRepository(t *testing.T) {
	root := filepath.Join("..", "..")
	read := map[string]bool{}
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
		src, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		for _, name := range markdownNames(filepath.ToSlash(rel), src) {
			read[name] = true
			if _, ok := gateInputs[name]; !ok {
				t.Errorf("%s reads %s, which gateInputs does not list", rel, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name := range gateInputs {
		if !read[name] {
			t.Errorf("gateInputs lists %s, which no Go file names", name)
		}
	}
}

// markdownNames returns the repository-relative Markdown paths that the Go file rel names.
// A path that starts with "./" or "../", the first argument of an os function and a
// //go:embed pattern are relative to the directory of the file, as go test and the
// compiler resolve them. Any other path is relative to the repository root: the arguments
// of a path.Join or filepath.Join call joined (code that joins them to a root directory),
// and a word of another string literal (fixture writers, messages).
func markdownNames(rel string, src []byte) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil // fixtures that must not parse
	}
	dir := path.Dir(rel)
	var names []string
	add := func(name string, local bool) {
		name = strings.Trim(name, "`'\"()[],;:")
		if !strings.HasSuffix(name, ".md") {
			return
		}
		if local || strings.HasPrefix(name, "./") || strings.HasPrefix(name, "../") {
			name = path.Join(dir, name)
		}
		names = append(names, path.Clean(name))
	}
	literal := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(lit.Value)
		return value, err == nil
	}
	handled := map[ast.Expr]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch {
			case sel.Sel.Name == "Join" && (pkg.Name == "path" || pkg.Name == "filepath"):
				var parts []string
				for _, arg := range n.Args {
					if value, ok := literal(arg); ok {
						parts = append(parts, value)
						handled[arg] = true
					}
				}
				add(path.Join(parts...), false)
			case pkg.Name == "os" && len(n.Args) > 0:
				if value, ok := literal(n.Args[0]); ok {
					add(value, true)
					handled[n.Args[0]] = true
				}
			}
		case *ast.BasicLit:
			if value, ok := literal(n); ok && !handled[n] {
				for _, word := range strings.Fields(value) {
					add(word, false)
				}
			}
		}
		return true
	})
	for _, group := range file.Comments {
		for _, c := range group.List {
			if pattern, ok := strings.CutPrefix(c.Text, "//go:embed "); ok {
				for _, word := range strings.Fields(pattern) {
					add(word, true)
				}
			}
		}
	}
	return names
}

func TestMarkdownNames(t *testing.T) {
	src := "package p\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n)\n\n" +
		"//go:embed notes.md\nvar notes string\n\n" +
		"func f(root string) {\n" +
		"\t_, _ = os.ReadFile(\"README.md\")\n" +
		"\t_, _ = os.ReadFile(\"../docs/API.md\")\n" +
		"\t_ = filepath.Join(root, \"docs\", \"PARITY.md\")\n" +
		"\tput(root, \"docs/ROADMAP.md\")\n" +
		"\tpanic(\"no rows in docs/PARITY.md\")\n" +
		"}\n"
	got := markdownNames("v2/p/p.go", []byte(src))
	want := []string{"v2/p/README.md", "v2/docs/API.md", "docs/PARITY.md", "docs/ROADMAP.md", "docs/PARITY.md", "v2/p/notes.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("markdownNames = %q, want %q", got, want)
	}
}

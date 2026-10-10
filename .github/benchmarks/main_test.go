package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTokens(t *testing.T) {
	tests := []struct {
		name, before, after string
		equal               bool
	}{
		{"line comment", "package p\n// old\nvar x = 1\n", "package p\n// new\nvar x = 1 // inline\n", true},
		{"block comment", "package p\n/* old */ var x = 1\n", "package p\n/* new\ncomment */ var x = 1\n", true},
		{"formatting", "package p\nvar x=1\n", "package p\n\nvar x = 1;\n", true},
		{"code", "package p\nvar x = 1\n", "package p\nvar x = 2\n", false},
		{"comment in string", "package p\nvar x = `// old`\n", "package p\nvar x = `// new`\n", false},
		{"block comment in string", "package p\nvar x = `/* old */`\n", "package p\nvar x = `/* new */`\n", false},
		{"build directive", "//go:build linux\n\npackage p\n", "//go:build darwin\n\npackage p\n", false},
		{"legacy build directive", "// +build linux\n\npackage p\n", "// +build darwin\n\npackage p\n", false},
		{"legacy build tab", "// +build\tlinux\n\npackage p\n", "// +build\tdarwin\n\npackage p\n", false},
		{"compiler directive", "package p\nfunc f() {}\n", "package p\n//go:noinline\nfunc f() {}\n", false},
		{"embed directive", "package p\n//go:embed a.txt\nvar s string\n", "package p\n//go:embed b.txt\nvar s string\n", false},
		{"line directive", "package p\n//line a.go:1\nvar x = 1\n", "package p\n//line b.go:1\nvar x = 1\n", false},
		{"block line directive", "package p\n/*line a.go:1*/var x = 1\n", "package p\n/*line b.go:1*/var x = 1\n", false},
		{"cgo preamble", "package p\n/* #define X 1 */\nimport \"C\"\n", "package p\n/* #define X 2 */\nimport \"C\"\n", false},
		{"cgo raw import", "package p\n/* #define X 1 */\nimport `C`\n", "package p\n/* #define X 2 */\nimport `C`\n", false},
		{"semicolon insertion", "package p\nfunc f() { return /* same line */ 1 }\n", "package p\nfunc f() { return /* new\nline */ 1 }\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, err := tokens([]byte(tt.before))
			if err != nil {
				t.Fatal(err)
			}
			after, err := tokens([]byte(tt.after))
			if err != nil {
				t.Fatal(err)
			}
			if equal := slices.Equal(before, after); equal != tt.equal {
				t.Fatalf("equal = %t, want %t", equal, tt.equal)
			}
		})
	}
}

func TestCodeChanged(t *testing.T) {
	tests := []struct {
		name, path, before, after string
		changed                   bool
	}{
		{"comments", "file.go", "package p\n// old\n", "package p\n// new\n", false},
		{"code", "file.go", "package p\nvar x = 1\n", "package p\nvar x = 2\n", true},
		{"add", "new.go", "", "package p\n", true},
		{"delete", "old.go", "package p\n", "", true},
		{"dependencies", "go.mod", "module old\n", "module new\n", true},
		{"checksum", "go.sum", "old\n", "new\n", true},
		{"documentation", "README.md", "old\n", "new\n", false},
		{"example", "_examples/main.go", "package old\n", "package new\n", false},
		{"experiment source", "_experiments/x/x.go", "package old\n", "package new\n", false},
		{"experiment module", "_experiments/x/go.mod", "module old\n", "module new\n", false},
		{"v2 code", "v2/engineio/x.go", "package p\nvar x = 1\n", "package p\nvar x = 2\n", true},
		{"v2 dependencies", "v2/go.mod", "module old\n", "module new\n", true},
		{"v2 checksum", "v2/go.sum", "old\n", "new\n", true},
		{"v2 example", "v2/_examples/x/main.go", "package old\n", "package new\n", false},
		{"unrelated workflow", ".github/workflows/ci.yaml", "old\n", "new\n", false},
		{"benchmark workflow", ".github/workflows/benchmarks.yml", "old\n", "new\n", true},
		{"helper comments", ".github/benchmarks/main.go", "package main\n// old\n", "package main\n// new\n", false},
		{"fixture", "engineio/testdata/input.json", "old\n", "new\n", true},
		{"assembly", "code.s", "old\n", "new\n", true},
		{"space in filename", "a b.go", "package p\nvar x = 1\n", "package p\nvar x = 2\n", true},
		{"invalid source", "file.go", "package p\n", "package p\nvar x = `\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			run("config", "user.name", "Benchmark Test")
			run("config", "user.email", "test@example.invalid")
			run("config", "commit.gpgsign", "false")
			write := func(content string) {
				t.Helper()
				name := filepath.Join(dir, tt.path)
				if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
					t.Fatal(err)
				}
				if content == "" {
					if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(name, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
				run("add", "-A")
				run("commit", "-qm", "fixture", "--allow-empty")
			}
			write(tt.before)
			base := run("rev-parse", "HEAD")
			write(tt.after)
			changed, err := codeChanged(dir, base, "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			if changed != tt.changed {
				t.Fatalf("changed = %t, want %t", changed, tt.changed)
			}
			changed, err = codeChanged(dir, "HEAD", "HEAD")
			if err != nil || changed {
				t.Fatalf("identical revisions: changed = %t, err = %v", changed, err)
			}
			if _, err := codeChanged(dir, "missing-revision", "HEAD"); err == nil {
				t.Fatal("missing revision must fail")
			}
			if tt.name == "code" {
				previous := run("rev-parse", "HEAD")
				write(tt.after + "// Documentation added after the code change.\n")
				changed, err := codeChanged(dir, previous, "HEAD")
				if err != nil || changed {
					t.Fatalf("comment-only follow-up: changed = %t, err = %v", changed, err)
				}
			}
			if tt.name == "comments" {
				previous := run("rev-parse", "HEAD")
				run("mv", tt.path, "renamed_linux.go")
				run("commit", "-qm", "rename")
				changed, err := codeChanged(dir, previous, "HEAD")
				if err != nil || !changed {
					t.Fatalf("renamed source: changed = %t, err = %v", changed, err)
				}
			}
		})
	}
}

package parity

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// Use the documented extraction command itself, so moving helper boundaries or
// changing a function's shell syntax cannot silently break the example.
func tddHistoryExtraction(t *testing.T) (string, string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "ROADMAP.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, `eval "$(awk `) {
			return root, line
		}
	}
	t.Fatal("ROADMAP has no helper extraction command")
	return "", ""
}

func tddHistoryBash(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		var roots []string
		if git, err := exec.LookPath("git"); err == nil {
			dir := filepath.Dir(git)
			for i := 0; i < 3; i++ {
				roots = append(roots, dir)
				dir = filepath.Dir(dir)
			}
		}
		for _, key := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			if dir := os.Getenv(key); dir != "" {
				roots = append(roots, filepath.Join(dir, "Git"), filepath.Join(dir, "Programs", "Git"))
			}
		}
		for _, root := range roots {
			for _, rel := range []string{"bin/bash.exe", "usr/bin/bash.exe"} {
				candidate := filepath.Join(root, filepath.FromSlash(rel))
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					return candidate
				}
			}
		}
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal("the documented TDD helper requires bash; Git for Windows provides it")
	}
	return bash
}

func TestTDDHistoryExtractionIncludesAllHelpers(t *testing.T) {
	root, extract := tddHistoryExtraction(t)
	cmd := exec.Command(tddHistoryBash(t), "-c", "set -e\n"+extract+"\ndeclare -F tdd_red tdd_marker tdd")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("documented extraction omitted a helper: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "tdd_red\ntdd_marker\ntdd" {
		t.Fatalf("helpers=%q", out)
	}
}

type tddHistoryRepo struct {
	t                             *testing.T
	dir, root, extract, base, red string
}

func newTDDHistoryRepo(t *testing.T) *tddHistoryRepo {
	t.Helper()
	root, extract := tddHistoryExtraction(t)
	r := &tddHistoryRepo{t: t, dir: t.TempDir(), root: root, extract: extract}
	r.git("init", "-q")
	r.git("config", "user.name", "Repository Test")
	r.git("config", "user.email", "test@example.invalid")
	r.git("config", "commit.gpgsign", "false")
	hooks := filepath.Join(r.dir, "empty-hooks")
	if err := os.MkdirAll(hooks, 0755); err != nil {
		t.Fatal(err)
	}
	r.git("config", "core.hooksPath", hooks)
	r.write("feature_test.go", tddExistingTest)
	r.write("docs/PARITY.md", "| G1 | BUG V1-1 |\n")
	r.base = r.commit("baseline feature")
	r.write("defect_test.go", tddTargetFile)
	r.red = r.commit("introduce regression test")
	return r
}

const tddExistingTest = "package fixture\nimport \"testing\"\nfunc TestExisting(t *testing.T) {}\n"
const tddTargetDecl = "func TestMoved(t *testing.T) { t.Fatal(\"regression\") }\n"
const tddTargetFile = "package fixture\nimport \"testing\"\n// Covers G1\n" + tddTargetDecl

func (r *tddHistoryRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func (r *tddHistoryRepo) write(path, data string) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(r.dir, path)), 0755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, path), []byte(data), 0600); err != nil {
		r.t.Fatal(err)
	}
}
func (r *tddHistoryRepo) commit(message string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-qm", message)
	return r.git("rev-parse", "HEAD")
}
func (r *tddHistoryRepo) moveIntoFeature() {
	r.t.Helper()
	r.write("feature_test.go", tddExistingTest+tddTargetDecl)
	r.git("rm", "-q", "defect_test.go")
	r.commit("group tests in existing feature file")
}
func (r *tddHistoryRepo) expect(test, path, want string) {
	r.t.Helper()
	script := "set -e\n" + r.extract + "\ncd \"$1\"\nshift\ntdd_red \"$@\""
	cmd := exec.Command(tddHistoryBash(r.t), "-c", script, "history", filepath.ToSlash(r.dir), r.base, r.git("rev-parse", "HEAD"), test, path)
	cmd.Dir = r.root
	out, err := cmd.CombinedOutput()
	if want == "" {
		if err == nil || len(strings.TrimSpace(string(out))) != 0 {
			r.t.Fatalf("ambiguous/preexisting history accepted: err=%v output=%q", err, out)
		}
	} else if err != nil || strings.TrimSpace(string(out)) != want {
		r.t.Fatalf("introduced test commit=%q err=%v; want %s", out, err, want)
	}
}

func TestTDDHistoryGlobalProvenance(t *testing.T) {
	for _, tc := range []struct {
		name       string
		change     func(*tddHistoryRepo)
		test, path string
		reject     bool
	}{
		{name: "move into existing feature file", change: func(r *tddHistoryRepo) { r.moveIntoFeature() }, test: "TestMoved", path: "feature_test.go"},
		{name: "rename", change: func(r *tddHistoryRepo) {
			r.git("mv", "defect_test.go", "renamed_test.go")
			r.commit("rename test file")
		}, test: "TestMoved", path: "renamed_test.go"},
		{name: "move and rename", change: func(r *tddHistoryRepo) {
			r.moveIntoFeature()
			r.git("mv", "feature_test.go", "renamed_test.go")
			r.commit("rename grouped file")
		}, test: "TestMoved", path: "renamed_test.go"},
		{name: "duplicates across files", change: func(r *tddHistoryRepo) { r.write("duplicate_test.go", tddTargetFile); r.commit("duplicate test") }, test: "TestMoved", path: "defect_test.go", reject: true},
		{name: "duplicates in one file", change: func(r *tddHistoryRepo) {
			r.write("defect_test.go", tddTargetFile+tddTargetDecl)
			r.commit("duplicate declaration")
		}, test: "TestMoved", path: "defect_test.go", reject: true},
		{name: "historical duplicates resolved", change: func(r *tddHistoryRepo) {
			r.write("duplicate_test.go", tddTargetFile)
			r.commit("duplicate test")
			r.git("rm", "-q", "duplicate_test.go")
			r.commit("remove duplicate")
		}, test: "TestMoved", path: "defect_test.go", reject: true},
		{name: "existed at base", change: func(r *tddHistoryRepo) {
			r.git("mv", "feature_test.go", "renamed_test.go")
			r.commit("rename existing test")
		}, test: "TestExisting", path: "renamed_test.go", reject: true},
		{name: "reintroduced name", change: func(r *tddHistoryRepo) {
			r.git("rm", "-q", "defect_test.go")
			r.commit("remove test")
			r.write("reintroduced_test.go", tddTargetFile)
			r.commit("reintroduce same name")
		}, test: "TestMoved", path: "reintroduced_test.go", reject: true},
		{name: "wrong current path", change: func(*tddHistoryRepo) {}, test: "TestMoved", path: "feature_test.go", reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTDDHistoryRepo(t)
			tc.change(r)
			want := r.red
			if tc.reject {
				want = ""
			}
			r.expect(tc.test, tc.path, want)
		})
	}
}

// Keep genuine-red classification separate from provenance. A controlled go
// command reports the selected failure at red and a passing target at head.
// No Go child build is needed.
func TestTDDHistoryRejectsNonRegressionFailures(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		status       int
		want         string
	}{
		{"build failure", "[build failed]\n--- FAIL: TestMoved (0.00s)", 1, "does not build or run"},
		{"setup failure", "[setup failed]\n--- FAIL: TestMoved (0.00s)", 1, "does not build or run"},
		{"unrelated failure", "tool unavailable", 1, "not red at"},
		{"passing panic text", "panic: logged only\n--- PASS: TestMoved (0.00s)", 0, "not red at"},
		{"genuine assertion", "--- FAIL: TestMoved (0.00s)", 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTDDHistoryRepo(t)
			r.write("note.txt", "head\n")
			head := r.commit("record fixed head")
			script := "set -e\n" + r.extract + `
cd "$1"
expected_head=$3
red_output=$4
red_status=$5
go() {
 if test "$(git rev-parse HEAD)" = "$expected_head"; then
  printf '%s\n' '--- PASS: TestMoved (0.00s)'
  return 0
 fi
 printf '%s\n' "$red_output"
 return "$red_status"
}
tdd "$2" "$3" TestMoved`
			cmd := exec.Command(tddHistoryBash(t), "-c", script, "classification", filepath.ToSlash(r.dir), r.base, head, tc.output, strconv.Itoa(tc.status))
			cmd.Dir = r.root
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("helper exited before diagnosing failure: %v\n%s", err, out)
			}
			if tc.want == "" {
				if len(strings.TrimSpace(string(out))) != 0 {
					t.Fatalf("genuine red rejected: %s", out)
				}
			} else if !strings.Contains(string(out), tc.want) {
				t.Fatalf("missing %q diagnostic: %s", tc.want, out)
			}
		})
	}
}

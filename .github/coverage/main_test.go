package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmptyFunctionsRequireExecutionEvidence(t *testing.T) {
	root := t.TempDir()
	source := `package fixture
func Called() {}
func Uncalled() {}
func Nonempty() { println("body") }
func Branch() { if true {} }
type receiver struct{}
func (receiver) Multiline(
) {
 // no executable statements
}
func One() {}; func Two() {}
`
	if err := os.WriteFile(filepath.Join(root, "fixture.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	profile := `mode: set
example/fixture.go:2.16,2.17 0 1
example/fixture.go:3.18,3.19 0 0
example/fixture.go:4.17,4.34 1 0
example/fixture.go:5.25,5.26 0 1
example/fixture.go:8.4,10.1 0 1
example/fixture.go:11.13,11.14 0 1
example/fixture.go:11.28,11.29 0 0
`
	summary := `example/fixture.go:2: Called 0.0%
example/fixture.go:3: Uncalled 0.0%
example/fixture.go:4: Nonempty 0.0%
example/fixture.go:5: Branch 0.0%
example/fixture.go:7: Multiline 0.0%
example/fixture.go:11: One 0.0%
example/fixture.go:11: Two 0.0%
total: (statements) 72.3%
`
	var output bytes.Buffer
	if err := annotate(strings.NewReader(profile), strings.NewReader(summary), &output, root, "example"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		want := "0.0%"
		if i == 0 || i == 4 || i == 5 {
			want = "100.0%"
		}
		if i == 7 {
			want = "72.3%"
		}
		if fields[2] != want {
			t.Errorf("line %d: got %q, want %s", i, line, want)
		}
	}
}

func TestMalformedProfileFails(t *testing.T) {
	for _, profile := range []string{"broken", "mode: set\nfile.go:1.1,1.2 x 1\n", "mode: set\nfile.go:1.1,1.2 0 x\n", "mode: set\nfile.go:invalid 0 1\n", "mode: set\nother/file.go:1.1,1.2 0 1\n", "mode: set\nexample/../outside.go:1.1,1.2 0 1\n"} {
		if err := annotate(strings.NewReader(profile), strings.NewReader(""), &bytes.Buffer{}, t.TempDir(), "example"); err == nil {
			t.Errorf("accepted invalid profile %q", profile)
		}
	}
}

func TestLineDirectivesDoNotCreditUncalledFunctions(t *testing.T) {
	for name, fixture := range map[string]struct{ source, body, uncalled string }{
		"aliased functions": {`package fixture
//line fixture.go:10:1
func Called() {}
//line fixture.go:10:1
func Unused() {}
//line fixture.go:20:1
func Live() int { return 1 }
`, "Called(); _ = Live()", "Unused"},
		"reversed branch": {`package fixture
func Uncalled() {}
func called() {
 if true {
//line fixture.go:2:17
 }
}
`, "called()", "Uncalled"},
		"block directives": {`package fixture
/*line fixture.go:10:1*/func Called() {}
/*line fixture.go:10:1*/func Unused() {}
/*line fixture.go:20:1*/func Live() int { return 1 }
`, "Called(); _ = Live()", "Unused"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for file, content := range map[string]string{
				"go.mod":          "module example\n\ngo 1.22\n",
				"fixture.go":      fixture.source,
				"fixture_test.go": "package fixture\nimport \"testing\"\nfunc TestCalls(t *testing.T) { " + fixture.body + " }\n",
			} {
				if err := os.WriteFile(filepath.Join(root, file), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			run := func(args ...string) []byte {
				t.Helper()
				cmd := exec.Command("go", args...)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("go %v: %v\n%s", args, err, out)
				}
				return out
			}
			run("test", "-count=1", "-coverprofile=profile.out", ".")
			summary := run("tool", "cover", "-func=profile.out")
			profile, err := os.ReadFile(filepath.Join(root, "profile.out"))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, line := range strings.Split(string(summary), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 3 && fields[1] == fixture.uncalled {
					found = true
					if fields[2] != "0.0%" {
						t.Fatalf("fixture unexpectedly covered %s: %s", fixture.uncalled, line)
					}
				}
			}
			if !found {
				t.Fatalf("missing uncalled function in summary: %s", summary)
			}
			var output bytes.Buffer
			if err := annotate(bytes.NewReader(profile), bytes.NewReader(summary), &output, root, "example"); err != nil {
				t.Fatal(err)
			}
			if output.String() != string(summary) {
				t.Fatalf("ambiguous line directives must not add execution credit:\n%s", output.String())
			}
		})
	}
}

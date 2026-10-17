package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrossFileDirectivesDoNotCreditUncalledFunctions(t *testing.T) {
	for name, directive := range map[string]string{
		"line comment":  "//line victim.go:2:1\n",
		"block comment": "/*line victim.go:2:1*/",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{
				"go.mod":          "module example\n\ngo 1.22\n",
				"victim.go":       "package fixture\nfunc Unused() {}\nfunc Live() int { return 1 }\n",
				"alias.go":        "package fixture\n" + directive + "func called() {}\n",
				"fixture_test.go": "package fixture\nimport \"testing\"\nfunc TestCalls(t *testing.T) { called(); _ = Live() }\n",
			}
			for file, content := range files {
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
			var output bytes.Buffer
			if err := annotate(bytes.NewReader(profile), bytes.NewReader(summary), &output, root, "example"); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, line := range strings.Split(output.String(), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 3 && fields[1] == "Unused" {
					found = true
					if fields[2] != "0.0%" {
						t.Fatalf("another physical file supplied false call evidence: %s\nraw profile:\n%s", line, profile)
					}
				}
			}
			if !found {
				t.Fatalf("missing Unused summary: %s", output.String())
			}
		})
	}
}

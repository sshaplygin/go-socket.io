package parity

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

var rowID = regexp.MustCompile(`^[A-Z][0-9]+$`)
var markerID = regexp.MustCompile(`^// Covers ([A-Z][0-9]+)( |$)`)

type testRef struct{ dir, name string }

func TestParityCovers(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	listed, err := listedTests(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkParity(root, os.Getenv("PARITY_STRICT") == "1", listed); err != nil {
		t.Fatal(err)
	}
}

// The conformance tag discovers its tests without running Node. Package identity
// matters: a same-named test in another package cannot validate a stale marker.
func listedTests(root string) (map[testRef]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-tags", "conformance", "-list", ".", "-json", "./...")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			output = append(output, failure.Stderr...)
		}
		return nil, fmt.Errorf("list tagged tests: %w\n%s", err, output)
	}
	const module = "github.com/sshaplygin/go-socket.io"
	listed := make(map[testRef]bool)
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for {
		var event struct{ Action, Package, Output string }
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode test list: %w", err)
		}
		name := strings.TrimSpace(event.Output)
		if event.Action != "output" || !strings.HasPrefix(name, "Test") || strings.ContainsAny(name, " \t\r\n") {
			continue
		}
		dir := "."
		if event.Package != module {
			var ok bool
			dir, ok = strings.CutPrefix(event.Package, module+"/")
			if !ok {
				continue
			}
		}
		listed[testRef{filepath.FromSlash(dir), name}] = true
	}
	return listed, nil
}

func checkParity(root string, strict bool, listed map[testRef]bool) error {
	data, err := os.ReadFile(filepath.Join(root, "docs", "PARITY.md"))
	if err != nil {
		return err
	}
	rows := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		columns := strings.Split(line, "|")
		if len(columns) < 4 {
			continue
		}
		id := strings.TrimSpace(columns[1])
		if !rowID.MatchString(id) {
			continue
		}
		if _, exists := rows[id]; exists {
			return fmt.Errorf("duplicate parity row %s", id)
		}
		required := false
		for _, entry := range strings.Split(columns[2], ";") {
			fields := strings.Fields(entry)
			if len(fields) == 0 {
				continue
			}
			switch fields[0] {
			case "DONE":
				required = true
			case "ADD", "CHG", "BUG", "TEST":
				required = required || strict
			}
		}
		rows[id] = required
	}
	if len(rows) == 0 {
		return fmt.Errorf("no parity rows in docs/PARITY.md")
	}
	found := make(map[string]bool)
	var problems []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != root {
				switch d.Name() {
				case ".git", "node_modules", "_examples", "v2":
					return filepath.SkipDir
				}
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		dir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				position := fset.PositionFor(comment.Pos(), false)
				if position.Column != 1 {
					continue
				}
				match := markerID.FindStringSubmatch(comment.Text)
				if match == nil {
					continue
				}
				id := match[1]
				if _, exists := rows[id]; !exists {
					problems = append(problems, fmt.Sprintf("unknown Covers marker %s in %s:%d", id, path, position.Line))
					continue
				}
				valid := false
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") {
						continue
					}
					distance := fset.PositionFor(fn.Pos(), false).Line - position.Line
					if distance > 0 && distance <= 4 && listed[testRef{dir, fn.Name.Name}] {
						valid = true
						break
					}
				}
				if !valid {
					problems = append(problems, fmt.Sprintf("Covers %s in %s:%d has no listed test within four lines", id, path, position.Line))
				}
				found[id] = found[id] || valid
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for id, required := range rows {
		if required && !found[id] {
			problems = append(problems, "parity row "+id+" has no Covers test")
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return fmt.Errorf("parity coverage:\n%s", strings.Join(problems, "\n"))
	}
	return nil
}

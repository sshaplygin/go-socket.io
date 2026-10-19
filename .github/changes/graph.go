package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// pkg is one package of a module as go list reports it, with its test variants folded in.
type pkg struct {
	path string // import path
	dir  string // slash path relative to the module root, "." for the root
	// deps holds every import path the package, its tests and its external tests depend on,
	// transitively.
	deps map[string]bool
	// listed is true when `go list ./...` with the default build tags matches the package,
	// which is what `go test ./...` tests.
	listed bool
	// tags names the extra build tags under which the package exists ("" for the default).
	tags map[string]bool
}

type graph struct {
	module string
	byDir  map[string]*pkg
	byPath map[string]*pkg
}

// listing is one go list run: the build tags and GOOS it uses.
type listing struct {
	tags, goos string
}

// listings are the build configurations of CI: the test matrix runs linux, darwin and
// windows with the default tags, the conformance job runs linux with -tags conformance.
var listings = []listing{
	{"", "linux"}, {"", "darwin"}, {"", "windows"}, {"conformance", "linux"},
}

func modulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", fmt.Errorf("%s/go.mod has no module line", root)
}

// owner maps a go list entry of `go list -test` to the package it belongs to: "p",
// "p [p.test]", "p_test [p.test]" and "p.test" all belong to p.
func owner(entry string) string {
	if i := strings.Index(entry, " ["); i >= 0 {
		return strings.TrimSuffix(strings.TrimSuffix(entry[i+2:], "]"), ".test")
	}
	return strings.TrimSuffix(entry, ".test")
}

func strip(entry string) string {
	if i := strings.Index(entry, " ["); i >= 0 {
		return entry[:i]
	}
	return entry
}

// loadGraph lists the packages of the module in root under every CI build configuration.
func loadGraph(root string) (*graph, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	abs, err := realPath(root)
	if err != nil {
		return nil, err
	}
	g := &graph{module: module, byDir: map[string]*pkg{}, byPath: map[string]*pkg{}}
	for _, l := range listings {
		args := []string{"list", "-e", "-test", "-f", "{{.ImportPath}}\t{{.Dir}}\t{{join .Deps \" \"}}"}
		if l.tags != "" {
			args = append(args, "-tags", l.tags)
		}
		cmd := exec.Command("go", append(args, "./...")...)
		cmd.Dir = root
		cmd.Env = append(cmd.Environ(), "GOOS="+l.goos, "GOFLAGS=-mod=readonly", "GOWORK=off")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("go list in %s (GOOS=%s tags=%q): %w: %s", root, l.goos, l.tags, err, stderr.String())
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) != 3 {
				continue
			}
			path := owner(fields[0])
			if path != module && !strings.HasPrefix(path, module+"/") {
				continue
			}
			p := g.byPath[path]
			if p == nil {
				p = &pkg{path: path, deps: map[string]bool{}, tags: map[string]bool{}}
				g.byPath[path] = p
			}
			if p.dir == "" && fields[1] != "" {
				real, err := realPath(fields[1])
				if err != nil {
					return nil, err
				}
				rel, err := filepath.Rel(abs, real)
				if err != nil {
					return nil, err
				}
				p.dir = filepath.ToSlash(rel)
				g.byDir[p.dir] = p
			}
			p.tags[l.tags] = true
			p.listed = p.listed || l.tags == ""
			for _, dep := range strings.Fields(fields[2]) {
				p.deps[strip(dep)] = true
			}
		}
	}
	return g, nil
}

func realPath(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// dependents returns the packages that are changed or depend on a changed package,
// through their own imports or through their tests.
func (g *graph) dependents(changed map[string]bool) map[string]*pkg {
	result := map[string]*pkg{}
	for path, p := range g.byPath {
		if changed[path] {
			result[path] = p
			continue
		}
		for dep := range p.deps {
			if changed[dep] {
				result[path] = p
				break
			}
		}
	}
	return result
}

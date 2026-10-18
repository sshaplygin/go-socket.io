package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/build"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

type listedPackage struct {
	ImportPath string
	GoFiles    []string
	CgoFiles   []string
	Imports    []string
	Deps       []string
	Incomplete bool
	Error      *struct{ Err string }
	DepsErrors []struct{ Err string }
}

type packageGraph map[string][]string

func inspect(ctx context.Context, root string) (int, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("go list failed: %w: %s", err, strings.TrimSpace(diagnostic.String()))
	}
	graph, err := readGraph(bytes.NewReader(out))
	if err != nil {
		return 0, err
	}
	for _, retired := range retiredPaths {
		_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(retired)))
		if err == nil {
			return 0, fmt.Errorf("retired path remains: %s", retired)
		}
		if !os.IsNotExist(err) {
			return 0, fmt.Errorf("check retired path %s: %w", retired, err)
		}
	}
	if err := validateGraph(graph); err != nil {
		return 0, err
	}
	return len(graph), nil
}

func readGraph(r io.Reader) (packageGraph, error) {
	decoder := json.NewDecoder(r)
	graph := make(packageGraph)
	seen := make(map[string]bool)
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("invalid go list JSON: %w", err)
		}
		if pkg.ImportPath == "" {
			return nil, fmt.Errorf("go list package has no import path")
		}
		if seen[pkg.ImportPath] {
			return nil, fmt.Errorf("duplicate go list package: %s", pkg.ImportPath)
		}
		seen[pkg.ImportPath] = true
		if pkg.Error != nil || len(pkg.DepsErrors) != 0 || pkg.Incomplete {
			return nil, fmt.Errorf("go list package error: %s: error=%v dependencies=%v incomplete=%t", pkg.ImportPath, pkg.Error, pkg.DepsErrors, pkg.Incomplete)
		}
		// Test-only packages have no runtime node. GoFiles contains filenames;
		// runtime edges come exclusively from Imports, never TestImports/XTestImports.
		if len(pkg.GoFiles)+len(pkg.CgoFiles) != 0 {
			// Native production Deps also crosses external modules whose package
			// objects are absent from go list ./.... Test dependencies stay out.
			if err := checkDependencyClosure(pkg.ImportPath, pkg.Deps); err != nil {
				return nil, err
			}
			graph[pkg.ImportPath] = pkg.Imports
		}
	}
	if len(graph) == 0 {
		return nil, fmt.Errorf("go list produced no runtime packages")
	}
	return graph, nil
}

func validateGraph(graph packageGraph) error {
	paths := sortedPaths(graph)
	for _, path := range paths {
		relative, local := relativePath(path)
		allowed, known := allowedImports[relative]
		if !local || !known {
			return fmt.Errorf("unexpected runtime package: %s", path)
		}
		imports := append([]string(nil), graph[path]...)
		sort.Strings(imports)
		for _, imported := range imports {
			dependency, local := relativePath(imported)
			if local && !slices.Contains(allowed, dependency) {
				return fmt.Errorf("forbidden production import: %s -> %s", path, imported)
			}
			if relative == "adapter" {
				dependency, err := build.Default.Import(imported, "", build.FindOnly)
				if err != nil || !dependency.Goroot {
					return fmt.Errorf("adapter requires standard-library dependencies only: %s", imported)
				}
			}
		}
	}
	for _, path := range sortedPaths(allowedImports) {
		if _, ok := graph[fullPath(path)]; !ok {
			return fmt.Errorf("missing runtime package: %s", fullPath(path))
		}
	}
	if err := checkCycles(graph); err != nil {
		return err
	}
	return checkClosures(graph)
}

func checkClosures(graph packageGraph) error {
	for _, path := range sortedPaths(graph) {
		if err := checkDependencyClosure(path, closure(graph, path)); err != nil {
			return err
		}
	}
	return nil
}

func checkDependencyClosure(path string, dependencies []string) error {
	relative, _ := relativePath(path)
	client := relative == "client" || strings.HasPrefix(relative, "client/") || relative == "engineio/client"
	server := relative == "" || relative == "internal/connection" || relative == "internal/namespace" || relative == "engineio" || relative == "engineio/internal/session"
	for _, dependency := range dependencies {
		local, repository := relativePath(dependency)
		if client && (repository && (local == "" || local == "engineio" || local == "internal/connection" || local == "internal/namespace" || local == "engineio/internal/session" || local == "adapter" || strings.HasPrefix(local, "adapter/")) || strings.Contains(dependency, "redigo")) {
			return fmt.Errorf("client reaches server/adapter: %s -> %s", path, dependency)
		}
		if server && (repository && (local == "client" || strings.HasPrefix(local, "client/") || local == "engineio/client" || local == "adapter/redis") || strings.Contains(dependency, "redigo")) {
			return fmt.Errorf("server reaches client/Redis: %s -> %s", path, dependency)
		}
		if repository && relative == "parser" && local != "logger" {
			return fmt.Errorf("Socket.IO parser reaches another component: %s", dependency)
		}
		if repository && strings.HasPrefix(relative, "engineio/parser") && local != "logger" && local != "engineio/parser/internal/packet" && local != "engineio/parser/internal/payload" {
			return fmt.Errorf("Engine.IO parser reaches another component: %s -> %s", path, dependency)
		}
	}
	return nil
}

func checkCycles(graph packageGraph) error {
	state := make(map[string]byte)
	var visit func(string) error
	visit = func(path string) error {
		if state[path] == 1 {
			return fmt.Errorf("production import cycle at %s", path)
		}
		if state[path] == 2 {
			return nil
		}
		state[path] = 1
		for _, dependency := range graph[path] {
			if _, local := graph[dependency]; local {
				if err := visit(dependency); err != nil {
					return err
				}
			}
		}
		state[path] = 2
		return nil
	}
	for _, path := range sortedPaths(graph) {
		if err := visit(path); err != nil {
			return err
		}
	}
	return nil
}

func closure(graph packageGraph, root string) []string {
	seen := make(map[string]bool)
	pending := []string{root}
	for len(pending) != 0 {
		path := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, dependency := range graph[path] {
			if !seen[dependency] {
				seen[dependency] = true
				pending = append(pending, dependency)
			}
		}
	}
	return sortedPaths(seen)
}

func sortedPaths[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func fullPath(relative string) string {
	if relative == "" {
		return modulePath
	}
	return modulePath + "/" + relative
}

func relativePath(path string) (string, bool) {
	if path == modulePath {
		return "", true
	}
	return strings.TrimPrefix(path, modulePath+"/"), strings.HasPrefix(path, modulePath+"/")
}

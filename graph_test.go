package socketio_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

const modulePath = "github.com/sshaplygin/go-socket.io"

// packageGraph returns the import edges between packages of this module, without
// test files, keyed by import path relative to the module ("." is the root).
func packageGraph(t *testing.T) map[string][]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-json=ImportPath,Imports", "./...")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	rel := func(p string) string {
		if p == modulePath {
			return "."
		}
		return strings.TrimPrefix(p, modulePath+"/")
	}
	graph := map[string][]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var pkg struct {
			ImportPath string
			Imports    []string
		}
		if err := dec.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		edges := []string{}
		for _, imp := range pkg.Imports {
			if imp == modulePath || strings.HasPrefix(imp, modulePath+"/") {
				edges = append(edges, rel(imp))
			}
		}
		sort.Strings(edges)
		graph[rel(pkg.ImportPath)] = edges
	}
	if len(graph) == 0 {
		t.Fatal("go list returned no packages")
	}
	return graph
}

func under(p, prefix string) bool { return p == prefix || strings.HasPrefix(p, prefix+"/") }

// forbiddenEdge reports why package p (relative to the module, "." is the root) must
// not import package q, or returns "" when the edge is allowed. The rules pin only
// what the roadmap relies on. ROADMAP 2.2 "Readiness" (the line "External adapters
// import the root; the root never imports them"): the root never imports adapter/...,
// adaptertest/..., client/, contrib/... or internal/fixtures/..., which all import it.
// ROADMAP 2.2 (Readiness, Snapshots and flags): adapter/codec depends on parser and wire
// types, never on the root, so the one handshake redaction helper, socketio.RedactHandshake,
// is called by the root itself (local snapshots) and by each adapter package (decoded
// peer snapshots), both without an edge the rules forbid.
// ROADMAP wave 2D (logger.Wrap and the Socket.IO > Engine > logger.Log precedence):
// the root may import engineio/..., parser and logger. The lower layers
// (engineio/..., parser, logger) never import the root.
func forbiddenEdge(p, q string) string {
	switch {
	case p == "." && (under(q, "adapter") || under(q, "adaptertest") || under(q, "client") || under(q, "contrib") || under(q, "internal/fixtures")):
		return fmt.Sprintf("root imports %s; adapters, the client, contrib and fixtures import the root, never the reverse", q)
	case p == "adapter/codec" && q == ".":
		return "adapter/codec imports the root package; it depends on parser and wire types only"
	case q == "." && (under(p, "engineio") || p == "parser" || p == "logger"):
		return fmt.Sprintf("%s imports the root package; engineio/..., parser and logger never do", p)
	case under(p, "engineio") && q == "parser":
		return fmt.Sprintf("%s imports parser; Engine.IO does not import the Socket.IO parser", p)
	case p == "parser" && q != "engineio/frame" && q != "logger":
		return fmt.Sprintf("parser imports %s; it may import only engineio/frame and logger", q)
	case p == "logger":
		return fmt.Sprintf("logger imports %s; it is a leaf", q)
	}
	return ""
}

// TestForbiddenEdge covers the layering rules with edges that do not exist yet, so a
// rule that is too narrow or too wide fails here rather than when 2.3C lands.
func TestForbiddenEdge(t *testing.T) {
	allowed := [][2]string{
		{"client", "."},
		{"client", "parser"},
		{"adapter", "."},
		{"adapter/redis", "."},
		{"adapter/redis", "adapter/codec"},
		{"adapter/codec", "parser"},
		{"contrib/otel", "."},
		{"internal/fixtures/positive", "."},
		{".", "engineio"},
		{".", "parser"},
		{".", "logger"},
		{".", "engineio/frame"},
		{".", "engineio/transport/polling"},
		{"parser", "engineio/frame"},
		{"parser", "logger"},
		{"engineio", "logger"},
		{"engineio", "engineio/frame"},
	}
	for _, e := range allowed {
		if msg := forbiddenEdge(e[0], e[1]); msg != "" {
			t.Errorf("%s -> %s should be allowed: %s", e[0], e[1], msg)
		}
	}
	forbidden := [][2]string{
		{"engineio", "."},
		{"engineio/session", "."},
		{"parser", "."},
		{"logger", "."},
		{"engineio", "parser"},
		{"engineio/transport/polling", "parser"},
		{".", "client"},
		{".", "internal/fixtures/positive"},
		{".", "adapter"},
		{".", "adapter/codec"},
		{"adapter/codec", "."},
		{".", "adaptertest"},
		{".", "contrib/otel"},
		{"parser", "engineio"},
		{"logger", "engineio"},
	}
	for _, e := range forbidden {
		if forbiddenEdge(e[0], e[1]) == "" {
			t.Errorf("%s -> %s should be forbidden", e[0], e[1])
		}
	}
}

// TestPackageGraph is the acyclic-graph check of docs/API.md (make graph). The Go
// compiler already rejects an import cycle; this test additionally pins the
// layering the roadmap relies on, so a dependency in the wrong direction fails here
// with its edge named.
func TestPackageGraph(t *testing.T) {
	graph := packageGraph(t)

	// Cycle detection by depth-first search with colours.
	const (
		white = iota
		grey
		black
	)
	colour := map[string]int{}
	var visit func(p string, path []string)
	visit = func(p string, path []string) {
		switch colour[p] {
		case grey:
			t.Errorf("import cycle: %s -> %s", strings.Join(path, " -> "), p)
			return
		case black:
			return
		}
		colour[p] = grey
		for _, q := range graph[p] {
			visit(q, append(path, p))
		}
		colour[p] = black
	}
	for p := range graph {
		visit(p, nil)
	}

	has := func(p string) bool { _, ok := graph[p]; return ok }
	for p, edges := range graph {
		for _, q := range edges {
			if msg := forbiddenEdge(p, q); msg != "" {
				t.Error(msg)
			}
		}
	}
	for _, p := range []string{".", "engineio", "parser", "logger"} {
		if !has(p) {
			t.Errorf("package %s missing from the graph", p)
		}
	}
	if t.Failed() {
		t.Logf("graph: %v", graph)
	}
	if os.Getenv("SOCKETIO_PRINT_GRAPH") != "" {
		keys := make([]string, 0, len(graph))
		for p := range graph {
			keys = append(keys, p)
		}
		sort.Strings(keys)
		for _, p := range keys {
			t.Logf("%s -> %s", p, strings.Join(graph[p], " "))
		}
	}
}

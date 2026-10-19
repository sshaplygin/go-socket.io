package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func allowedGraph() packageGraph {
	graph := make(packageGraph)
	for path, imports := range allowedImports {
		for _, dependency := range imports {
			graph[fullPath(path)] = append(graph[fullPath(path)], fullPath(dependency))
		}
		graph[fullPath(path)] = append(graph[fullPath(path)], "fmt")
	}
	return graph
}

func TestAllowedRuntimeGraph(t *testing.T) {
	graph := allowedGraph()
	var stream bytes.Buffer
	for _, path := range sortedPaths(graph) {
		if err := json.NewEncoder(&stream).Encode(listedPackage{ImportPath: path, GoFiles: []string{"component.go"}, Imports: graph[path]}); err != nil {
			t.Fatal(err)
		}
	}
	parsed, err := readGraph(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(graph, parsed) {
		t.Fatal("production Imports changed during JSON decoding")
	}
	if err := validateGraph(parsed); err != nil {
		t.Fatal(err)
	}
}

func TestForbiddenProductionImports(t *testing.T) {
	for _, tc := range []struct{ from, to string }{
		{"", "client"}, {"", "adapter/redis"},
		{"client", ""}, {"client", "engineio"},
		{"client/internal/connection", "internal/connection"},
		{"client/internal/namespace", "internal/namespace"},
		{"internal/event", ""}, {"internal/event", "client"},
		{"internal/event", "internal/connection"}, {"internal/event", "adapter"},
		{"engineio/client", "engineio/internal/session"},
		{"parser", "engineio/parser"},
		{"engineio/parser/internal/packet", "engineio/internal/session"},
		{"engineio/parser/internal/payload", "engineio/transport/polling"},
		{"adapter", "client"}, {"adapter/redis", ""},
		{"engineio/parser", "engineio/session"},
		{"client", "v2/parser"},
	} {
		t.Run(tc.from+"->"+tc.to, func(t *testing.T) {
			graph := allowedGraph()
			graph[fullPath(tc.from)] = append(graph[fullPath(tc.from)], fullPath(tc.to))
			if err := validateGraph(graph); err == nil || !strings.Contains(err.Error(), "forbidden production import") || !strings.Contains(err.Error(), fullPath(tc.to)) {
				t.Fatalf("forbidden edge accepted or wrong diagnostic: %v", err)
			}
		})
	}
	for _, from := range []string{"", "client/internal/connection", "engineio/client"} {
		graph := allowedGraph()
		graph[fullPath(from)] = append(graph[fullPath(from)], "github.com/gomodule/redigo/redis")
		if err := validateGraph(graph); err == nil || !strings.Contains(err.Error(), "redigo") {
			t.Fatalf("role reached Redis dependency: %v", err)
		}
	}
	for _, imported := range []string{"example.org/rooms", "localrooms"} {
		graph := allowedGraph()
		graph[fullPath("adapter")] = append(graph[fullPath("adapter")], imported)
		if err := validateGraph(graph); err == nil || !strings.Contains(err.Error(), "standard-library") {
			t.Fatalf("memory adapter accepted an external runtime dependency %s: %v", imported, err)
		}
	}
}

func TestTransitiveRoleAndCodecExclusions(t *testing.T) {
	t.Run("external package names stay distinct", func(t *testing.T) {
		for _, role := range []string{"", "client", "engineio/client", "parser", "engineio/parser/internal/packet"} {
			if err := checkDependencyClosure(fullPath(role), []string{"client", "client/namespace", "engineio", "internal/connection", "adapter", "adapter/redis"}); err != nil {
				t.Fatalf("external import name misclassified as a repository role: %v", err)
			}
		}
	})
	// Exercise closure rules independently of direct-edge rules, so a future
	// allowed-edge amendment cannot silently couple roles through a private owner.
	for _, chain := range [][]string{
		{"", "internal/connection", "adapter/redis"},
		{"client", "client/internal/namespace", "engineio"},
		{"engineio/client", "engineio/parser/internal/packet", ""},
		{"parser", "logger", "engineio/internal/session"},
		{"engineio/parser", "engineio/parser/internal/payload", "engineio/transport"},
		{"engineio/parser/internal/packet", "logger", "engineio/internal/session"},
	} {
		graph := make(packageGraph)
		for i := 0; i+1 < len(chain); i++ {
			graph[fullPath(chain[i])] = []string{fullPath(chain[i+1])}
		}
		if err := checkClosures(graph); err == nil {
			t.Fatalf("accepted forbidden transitive chain: %v", chain)
		}
	}
}

func TestRuntimeInventoryAndCycles(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		graph := allowedGraph()
		delete(graph, fullPath("client/internal/namespace"))
		if err := validateGraph(graph); err == nil || !strings.Contains(err.Error(), "missing runtime package") {
			t.Fatalf("missing package accepted: %v", err)
		}
	})
	for _, path := range []string{fullPath("helpers"), fullPath("v2"), "example.org/another/module"} {
		t.Run(path, func(t *testing.T) {
			graph := allowedGraph()
			graph[path] = nil
			if err := validateGraph(graph); err == nil || !strings.Contains(err.Error(), "unexpected runtime package") {
				t.Fatalf("unexpected package accepted: %v", err)
			}
		})
	}
	if err := checkCycles(packageGraph{"a": {"b"}, "b": {"a"}}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle accepted: %v", err)
	}
}

func TestInvalidListOutput(t *testing.T) {
	for _, text := range []string{
		"", " ", "[]", "{}", "null", "not JSON",
		`{"ImportPath":"x","GoFiles":["x.go"]`,
		`{"ImportPath":"x","GoFiles":["x.go"]} trailing`,
		`{"ImportPath":"x","GoFiles":42}`,
		`{"ImportPath":"x","GoFiles":["x.go"],"Error":{"Err":"bad package"}}`,
		`{"ImportPath":"x","GoFiles":["x.go"],"DepsErrors":[{"Err":"missing dependency"}]}`,
		`{"ImportPath":"x","GoFiles":["x.go"],"Incomplete":true}`,
		`{"ImportPath":"test-only","TestGoFiles":["x_test.go"]}`,
		`{"ImportPath":"x","GoFiles":["x.go"]}{"ImportPath":"x","GoFiles":["x.go"]}`,
	} {
		t.Run(text, func(t *testing.T) {
			if _, err := readGraph(strings.NewReader(text)); err == nil {
				t.Fatal("invalid, empty or incomplete listing accepted")
			}
		})
	}
}

func TestTestImportsAndFilenamesAreNotRuntimeEdges(t *testing.T) {
	graph := allowedGraph()
	var stream bytes.Buffer
	for _, path := range sortedPaths(graph) {
		pkg := map[string]interface{}{
			"ImportPath": path, "GoFiles": []string{"client.go", "adapter.go"},
			"Imports": graph[path], "TestImports": []string{fullPath("client"), fullPath("")},
			"XTestImports": []string{fullPath("adapter/redis")},
		}
		if path == fullPath("logger") {
			pkg["GoFiles"], pkg["CgoFiles"] = nil, []string{"logger.go"}
		}
		if err := json.NewEncoder(&stream).Encode(pkg); err != nil {
			t.Fatal(err)
		}
	}
	if err := json.NewEncoder(&stream).Encode(map[string]interface{}{
		"ImportPath": fullPath("test-only"), "TestGoFiles": []string{"private_test.go"},
		"TestImports": []string{fullPath("client")}, "Imports": []string{fullPath("adapter/redis")},
	}); err != nil {
		t.Fatal(err)
	}
	parsed, err := readGraph(&stream)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGraph(parsed); err != nil {
		t.Fatal(err)
	}
}

func writeFixture(t *testing.T, root, name, source string) {
	t.Helper()
	name = filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRealGoListRejectsForbiddenProductionImport(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.22\n")
	writeFixture(t, root, "server.go", "package socketio\nimport _ \""+fullPath("client")+"\"\n")
	writeFixture(t, root, "client/client.go", "package client\n")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := inspect(ctx, root); err == nil || !strings.Contains(err.Error(), "server reaches client/Redis: "+modulePath+" -> "+fullPath("client")) {
		t.Fatalf("real go list did not reject the production edge: %v", err)
	}
}

func TestFailedListingAndRetiredPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := inspect(ctx, t.TempDir()); err == nil || !strings.Contains(err.Error(), "go list failed") {
		t.Fatalf("failed listing accepted: %v", err)
	}
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.22\n")
	for path := range allowedImports {
		writeFixture(t, root, filepath.Join(path, "component.go"), "package component\n")
	}
	if _, err := inspect(ctx, root); err != nil {
		t.Fatalf("allowed real fixture failed: %v", err)
	}
	for _, retired := range retiredPaths {
		t.Run(retired, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(retired))
			if strings.HasSuffix(path, ".go") {
				writeFixture(t, root, retired, "package component\n")
			} else if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
			}()
			if _, err := inspect(ctx, root); err == nil || !strings.Contains(err.Error(), "retired path remains: "+retired) {
				t.Fatalf("retired path accepted or wrong diagnostic: %v", err)
			}
		})
	}
	t.Run("dangling retired link", func(t *testing.T) {
		path := filepath.Join(root, "engineio", "frame")
		if err := os.Symlink("missing-retired-target", path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		defer func() {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}()
		if _, err := inspect(ctx, root); err == nil || !strings.Contains(err.Error(), "retired path remains: engineio/frame") {
			t.Fatalf("dangling retired path accepted: %v", err)
		}
	})

}

func TestPolicyMatchesRoadmap(t *testing.T) {
	text, err := os.ReadFile("../../docs/ROADMAP.md")
	if err != nil {
		t.Fatal(err)
	}
	documented, err := parsePolicyTable(string(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(documented) != len(allowedImports) {
		t.Fatalf("roadmap/policy component count differs: %d/%d", len(documented), len(allowedImports))
	}
	for path, imports := range allowedImports {
		expected, found := documented[path]
		actual := append([]string{}, imports...)
		slices.Sort(actual)
		if !found || !reflect.DeepEqual(expected, actual) {
			t.Errorf("roadmap/policy imports differ for %s: %v/%v", path, expected, actual)
		}
	}
}

// Native Deps is necessary when the intermediate package belongs to an external
// module, which go list ./... does not emit as a repository runtime node.
func TestNativeProductionDependencies(t *testing.T) {
	for _, tc := range []struct{ from, to string }{
		{"", "client"}, {"client", "engineio"},
		{"client/internal/connection", "internal/connection"},
		{"engineio/parser/internal/payload", "engineio/internal/session"},
		{"parser", "engineio/transport"},
		{"", "engineio/session"}, {"logger", "helpers"},
		{"client/internal/connection", "v2/parser"}, {"engineio/parser", "engineio/packet"},
	} {
		var stream bytes.Buffer
		if err := json.NewEncoder(&stream).Encode(listedPackage{
			ImportPath: fullPath(tc.from), GoFiles: []string{"component.go"},
			Imports: []string{"example.org/helper"}, Deps: []string{"example.org/helper", fullPath(tc.to)},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := readGraph(&stream); err == nil || !strings.Contains(err.Error(), fullPath(tc.to)) {
			t.Fatalf("native forbidden dependency accepted: %v", err)
		}
	}
}

func TestRealGoListRejectsTransitiveExternalImport(t *testing.T) {
	root, helper := t.TempDir(), t.TempDir()
	writeFixture(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.22\nrequire example.org/helper v0.0.0\nreplace example.org/helper => "+strconv.Quote(helper)+"\n")
	for path := range allowedImports {
		writeFixture(t, root, filepath.Join(path, "component.go"), "package component\n")
	}
	writeFixture(t, root, "component.go", "package component\nimport _ \"example.org/helper\"\n")
	writeFixture(t, helper, "go.mod", "module example.org/helper\n\ngo 1.22\nrequire "+modulePath+" v0.0.0\n")
	writeFixture(t, helper, "helper.go", "package helper\nimport _ \""+fullPath("client")+"\"\n")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := inspect(ctx, root); err == nil || !strings.Contains(err.Error(), "server reaches client/Redis: "+modulePath+" -> "+fullPath("client")) {
		t.Fatalf("external module concealed transitive client dependency: %v", err)
	}
}

func parsePolicyTable(text string) (map[string][]string, error) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	_, table, found := strings.Cut(text, "| Package | Responsibility | May import |\n")
	if !found {
		return nil, fmt.Errorf("owning Component boundaries table missing")
	}
	quoted := regexp.MustCompile("`([^`]+)`")
	documented := make(map[string][]string)
	for _, line := range strings.Split(table, "\n")[1:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(line, "|")
		if len(cells) != 5 {
			return nil, fmt.Errorf("invalid component row: %s", line)
		}
		path := quoted.FindStringSubmatch(cells[1])
		if len(path) != 2 {
			return nil, fmt.Errorf("component package missing: %s", line)
		}
		key := path[1]
		if strings.HasPrefix(strings.TrimSpace(cells[1]), "root ") {
			key = ""
		}
		dependencies := []string{}
		for _, match := range quoted.FindAllStringSubmatch(cells[3], -1) {
			dependencies = append(dependencies, match[1])
		}
		if strings.Contains(cells[3], "its polling/websocket implementations") {
			dependencies = append(dependencies, "engineio/transport/polling", "engineio/transport/websocket")
		}
		slices.Sort(dependencies)
		documented[key] = dependencies
	}

	return documented, nil
}

func TestPolicyTableLineEndings(t *testing.T) {
	raw, err := os.ReadFile("../../docs/ROADMAP.md")
	if err != nil {
		t.Fatal(err)
	}
	canonical := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
	want, err := parsePolicyTable(canonical)
	if err != nil {
		t.Fatal(err)
	}
	var mixed strings.Builder
	lines := strings.Split(canonical, "\n")
	for i, line := range lines {
		mixed.WriteString(line)
		if i+1 < len(lines) {
			mixed.WriteString([]string{"\n", "\r\n", "\r"}[i%3])
		}
	}
	for name, text := range map[string]string{
		"LF": canonical, "CRLF": strings.ReplaceAll(canonical, "\n", "\r\n"),
		"CR": strings.ReplaceAll(canonical, "\n", "\r"), "mixed": mixed.String(),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parsePolicyTable(text)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("line endings changed component policy: %v/%v", want, got)
			}
		})
	}
}

func TestRealGoListRejectsUnlistedExternalImports(t *testing.T) {
	for _, path := range []string{"engineio/session", "engineio/packet", "engineio/transport/utils", "helpers"} {
		t.Run(path, func(t *testing.T) {
			root, helper, unlisted := t.TempDir(), t.TempDir(), t.TempDir()
			dependency := fullPath(path)
			writeFixture(t, root, "go.mod", "module "+modulePath+"\n\ngo 1.22\nrequire (\nexample.org/helper v0.0.0\n"+dependency+" v0.0.0\n)\nreplace example.org/helper => "+strconv.Quote(helper)+"\nreplace "+dependency+" => "+strconv.Quote(unlisted)+"\n")
			for path := range allowedImports {
				writeFixture(t, root, filepath.Join(path, "component.go"), "package component\n")
			}
			writeFixture(t, root, "component.go", "package component\nimport _ \"example.org/helper\"\n")
			writeFixture(t, helper, "go.mod", "module example.org/helper\n\ngo 1.22\nrequire "+dependency+" v0.0.0\n")
			writeFixture(t, helper, "helper.go", "package helper\nimport _ "+strconv.Quote(dependency)+"\n")
			writeFixture(t, unlisted, "go.mod", "module "+dependency+"\n\ngo 1.22\n")
			writeFixture(t, unlisted, "component.go", "package component\n")
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if _, err := inspect(ctx, root); err == nil || !strings.Contains(err.Error(), "unlisted repository dependency") || !strings.Contains(err.Error(), dependency) {
				t.Fatalf("external module concealed retired/unlisted repository dependency: %v", err)
			}
		})
	}
}

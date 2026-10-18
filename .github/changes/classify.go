package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The two modules: v1 at the repository root, v2 in v2/.
const (
	v1 = "v1"
	v2 = "v2"
)

// gateInputs are the Markdown files that a gate reads, with the module whose gate job
// reads them: internal/parity reads README.md and docs/PARITY.md, .github/architecture
// reads docs/ROADMAP.md, the v2 root tests read docs/API.md. Markdown starts no job on a
// pull request, these files included (the owner accepted that the push to master checks
// them); the classifier only names them in its reasons so the job summary shows the skip.
var gateInputs = map[string]string{
	"README.md":       v1,
	"docs/PARITY.md":  v1,
	"docs/ROADMAP.md": v1,
	"docs/API.md":     v2,
}

// copies are example files that make examples compares byte for byte with one master copy,
// so a comment in them matters.
var copies = map[string]bool{"chat.go": true, "lifecycle.go": true}

// sources are non-Go files the go command compiles into a package.
var sources = map[string]bool{
	".s": true, ".S": true, ".c": true, ".h": true, ".cc": true, ".cpp": true, ".cxx": true,
	".m": true, ".mm": true, ".f": true, ".F": true, ".for": true, ".f90": true,
	".syso": true, ".swig": true, ".swigcxx": true,
}

// change is a Go or native source change that still has to be attributed to a package.
type change struct {
	dir, name     string
	test          bool
	before, after []byte // nil when the file does not exist on that side
}

// impact is what one module has to run.
type impact struct {
	full                                      bool // every CI job of the module, `go test ./...`
	benchFull                                 bool // every benchmark of the module
	lint                                      bool // the lint job of the module, without packages to test
	examples, experiments, gates, conformance bool
	changes                                   []change
	graph                                     *graph
}

// frozenSource is the v2 test file whose function frozenGoFiles lists the files that
// TestFrozenContract (make freeze, make g2) reads, comments included.
const frozenSource = "v2/frozen_test.go"

// frozenPatterns returns the string literals ending in .go and not starting with an
// underscore inside frozenGoFiles of src:
// path.Match patterns relative to v2/ (the root glob "*.go" and the named files). It
// returns nil when src does not parse or has no such function.
func frozenPatterns(src []byte) []string {
	file, err := parser.ParseFile(token.NewFileSet(), frozenSource, src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var list []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "frozenGoFiles" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			// "_test.go" is the suffix the function filters on, not a file.
			if value, err := strconv.Unquote(lit.Value); err == nil && strings.HasSuffix(value, ".go") && !strings.HasPrefix(value, "_") {
				list = append(list, value)
			}
			return true
		})
	}
	return list
}

// decision is the result of the classifier for one diff.
type decision struct {
	mods    map[string]*impact
	sync    bool
	reasons []string
	// frozen are the patterns of frozenGoFiles at base and head; when there are none, every
	// non-test v2 Go file counts as frozen.
	frozen []string
}

// frozenFile reports whether TestFrozenContract reads the comments of the v2 file rel.
func (d *decision) frozenFile(rel string) bool {
	if strings.HasSuffix(rel, "_test.go") {
		return false
	}
	if len(d.frozen) == 0 {
		return true
	}
	for _, p := range d.frozen {
		if ok, err := path.Match(p, rel); ok || err != nil {
			return true
		}
	}
	return false
}

func newDecision() *decision {
	return &decision{mods: map[string]*impact{v1: {}, v2: {}}}
}

func (d *decision) note(format string, args ...any) {
	d.reasons = append(d.reasons, fmt.Sprintf(format, args...))
}

func (d *decision) fullRun(mod string, bench bool, why string) {
	d.mods[mod].full = true
	d.mods[mod].benchFull = d.mods[mod].benchFull || bench
	d.note("%s: full run (%s)", mod, why)
}

func git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %v: %w: %s", args, err, stderr.String())
	}
	return out, nil
}

// manifest returns the paths named by .github/sync-manifest at rev (none if it is absent).
func manifest(dir, rev string) map[string]bool {
	result := map[string]bool{}
	data, err := git(dir, "show", rev+":.github/sync-manifest")
	if err != nil {
		return result
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		for _, f := range fields {
			result[f] = true
		}
	}
	return result
}

// classify reads the diff between base and head in the repository dir. The working tree of
// dir must be the head revision: the package graph is listed from it.
func classify(dir, base, head string) (*decision, error) {
	diff, err := git(dir, "diff", "--name-status", "--no-renames", "-z", base, head, "--")
	if err != nil {
		return nil, err
	}
	d := newDecision()
	shared := manifest(dir, base)
	for name := range manifest(dir, head) {
		shared[name] = true
	}
	for _, rev := range []string{base, head} {
		if src, err := git(dir, "show", rev+":"+frozenSource); err == nil {
			d.frozen = append(d.frozen, frozenPatterns(src)...)
		}
	}
	if len(d.frozen) == 0 {
		d.note("v2: no frozenGoFiles in %s, every non-test v2 Go comment runs the gates", frozenSource)
	}
	fields := strings.Split(strings.TrimSuffix(string(diff), "\x00"), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		status, name := fields[i], fields[i+1]
		if shared[name] {
			d.sync = true
			d.note("sync-check: %s is in .github/sync-manifest", name)
		}
		show := func(rev string) ([]byte, error) {
			out, err := git(dir, "show", rev+":"+name)
			if err != nil {
				return nil, err
			}
			if out == nil {
				out = []byte{}
			}
			return out, nil
		}
		var before, after []byte
		if status != "A" {
			if before, err = show(base); err != nil {
				return nil, err
			}
		}
		if status != "D" {
			if after, err = show(head); err != nil {
				return nil, err
			}
		}
		d.path(status, name, before, after)
	}
	for _, mod := range []string{v1, v2} {
		root := dir
		if mod == v2 {
			root = filepath.Join(dir, "v2")
		}
		if err := d.attribute(mod, root); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// path classifies one changed path. before and after are nil on the side where the path
// does not exist.
func (d *decision) path(status, name string, before, after []byte) {
	if strings.HasSuffix(name, ".md") {
		if mod, ok := gateInputs[name]; ok {
			d.note("%s: gates-%s reads %s; skipped on a pull request, the push to master runs it", mod, mod, name)
		}
		return
	}
	switch {
	case name == ".github/workflows/ci.yaml":
		d.fullRun(v1, false, name)
		d.fullRun(v2, false, name)
		d.sync = true
		return
	case name == ".github/workflows/benchmarks.yml" || strings.HasPrefix(name, ".github/benchmarks/"):
		d.mods[v1].benchFull = true
		d.mods[v2].benchFull = true
		d.note("benchmarks: every package (%s)", name)
		return
	case strings.HasPrefix(name, ".github/architecture/") || strings.HasPrefix(name, ".github/coverage/"):
		d.fullRun(v1, false, name)
		return
	case name == ".github/sync-manifest":
		d.sync = true
		d.note("sync-check: %s", name)
		return
	case name == ".github/dependabot.yml":
		d.note("no job reads %s", name)
		return
	case strings.HasPrefix(name, ".github/"):
		// The classifier itself and anything else under .github.
		d.fullRun(v1, true, name)
		d.fullRun(v2, true, name)
		d.sync = true
		return
	}
	mod, rel := v1, name
	if r, ok := strings.CutPrefix(name, "v2/"); ok {
		mod, rel = v2, r
	}
	m := d.mods[mod]
	dir := path.Dir(rel)
	parts := strings.Split(dir, "/")
	isGo := path.Ext(rel) == ".go"
	test := strings.HasSuffix(rel, "_test.go")
	switch {
	case slices.Contains(parts, "_examples"):
		same := isGo && sameTokens(before, after, test)
		if same && !copies[path.Base(rel)] {
			return
		}
		m.examples = true
		if mod == v1 && strings.HasPrefix(rel, "_examples/") {
			m.conformance = true
		}
		// internal/parity scans every non-test Go file of the v1 tree, examples included.
		if mod == v1 && isGo && !test && !same {
			m.gates = true
		}
		d.note("%s: examples (%s)", mod, name)
		return
	case slices.Contains(parts, "_experiments"):
		// Every change, comments and formatting included: make experiments is the only
		// golangci-lint run of these standalone modules.
		if mod != v2 {
			d.fullRun(mod, true, "unexpected experiment path "+name)
			return
		}
		m.experiments = true
		d.note("%s: experiments (%s)", mod, name)
		return
	case slices.Contains(parts, "testdata"):
		d.fullRun(mod, true, "test fixture "+name)
		return
	}
	for _, part := range parts {
		if part != "." && (strings.HasPrefix(part, "_") || strings.HasPrefix(part, ".")) {
			d.fullRun(mod, true, "unclassified directory of "+name)
			return
		}
	}
	switch base := path.Base(rel); {
	case base == "go.mod" || base == "go.sum" || base == "go.work" || base == "go.work.sum":
		d.fullRun(mod, true, "module file "+name)
		return
	case dir == "." && (base == "Makefile" || base == ".golangci.yml"):
		d.fullRun(mod, false, name)
		if mod == v1 && base == "Makefile" {
			d.sync = true
		}
		return
	case isGo:
		d.goFile(mod, status, name, rel, before, after)
		return
	case sources[path.Ext(rel)]:
		m.changes = append(m.changes, change{dir: dir, name: name, before: before, after: after})
		m.gates = true
		return
	}
	d.fullRun(mod, true, "unclassified file "+name)
}

// sameTokens reports whether two versions of a Go file have the same token stream. A file
// that is added, deleted or does not parse is never the same.
func sameTokens(before, after []byte, test bool) bool {
	if before == nil || after == nil {
		return false
	}
	a, errA := tokens(before, test)
	b, errB := tokens(after, test)
	return errA == nil && errB == nil && slices.Equal(a, b)
}

func (d *decision) goFile(mod, status, name, rel string, before, after []byte) {
	m := d.mods[mod]
	dir := path.Dir(rel)
	test := strings.HasSuffix(rel, "_test.go")
	if before != nil && after != nil && !slices.Equal(constraints(before), constraints(after)) {
		d.fullRun(mod, true, "build constraint of "+name)
		return
	}
	if sameTokens(before, after, test) {
		// Comments and formatting: golangci-lint and gofmt read every comment, so the lint
		// job runs and no package is tested.
		m.lint = true
		d.note("%s: lint (comments or formatting of %s)", mod, name)
		// Comments that gates read: Covers markers of the parity gate in v1 tests, and the
		// comments of the files frozenGoFiles names (TestFrozenContract).
		if mod == v1 && test && (bytes.Contains(before, []byte("// Covers ")) || bytes.Contains(after, []byte("// Covers "))) {
			m.gates = true
			d.note("%s: gates (Covers marker file %s)", mod, name)
		} else if mod == v2 && d.frozenFile(rel) {
			m.gates = true
			d.note("%s: gates (comment of the frozen file %s)", mod, name)
		}
		return
	}
	m.gates = true
	m.changes = append(m.changes, change{dir: dir, name: name, test: test, before: before, after: after})
}

// attribute maps the source changes of a module to packages of its graph.
func (d *decision) attribute(mod, root string) error {
	m := d.mods[mod]
	if len(m.changes) == 0 || (m.full && m.benchFull) {
		return nil
	}
	g, err := loadGraph(root)
	if err != nil {
		return err
	}
	m.graph = g
	for _, c := range m.changes {
		if g.byDir[c.dir] == nil {
			d.fullRun(mod, true, "cannot attribute "+c.name+" to a package")
			return nil
		}
	}
	return nil
}

// result is the job input of one module.
type result struct {
	run, examples, experiments, gates, conformance bool
	packages, bench                                []string
}

func patterns(set map[string]*pkg) []string {
	var list []string
	for _, p := range set {
		if p.dir == "." {
			list = append(list, ".")
		} else {
			list = append(list, "./"+p.dir)
		}
	}
	sort.Strings(list)
	return list
}

func (d *decision) result(mod, root string) (result, error) {
	m := d.mods[mod]
	r := result{
		run:         m.full || m.lint,
		examples:    m.full || m.examples,
		experiments: m.full || m.experiments,
		gates:       m.full || m.gates,
		conformance: m.full || m.conformance,
	}
	if m.full {
		r.packages = []string{"./..."}
	}
	if m.benchFull {
		r.bench = []string{"./..."}
	}
	g := m.graph
	if g == nil || (m.full && m.benchFull) {
		return r, nil
	}
	code := map[string]bool{}
	tested := map[string]*pkg{}
	var testChanges []change
	for _, c := range m.changes {
		p := g.byDir[c.dir]
		if c.test {
			tested[p.path] = p
			testChanges = append(testChanges, c)
		} else {
			code[p.path] = true
		}
	}
	impacted := g.dependents(code)
	for path, p := range impacted {
		tested[path] = p
	}
	// The v1 examples and the Node runs of the conformance job build against the module
	// through a replace directive; the v2 examples job only compares files.
	if len(code) > 0 && mod == v1 {
		r.examples = true
		r.conformance = true
	}
	run := map[string]*pkg{}
	for path, p := range tested {
		if p.listed {
			run[path] = p
		} else if p.tags["conformance"] {
			r.conformance = true
		}
	}
	if !m.full && len(run) > 0 {
		r.run = true
		r.packages = patterns(run)
	}
	if m.benchFull {
		return r, nil
	}
	bench := map[string]*pkg{}
	type benchInfo struct {
		has   bool
		reach map[string]bool // nil: every declaration counts as reached
	}
	infos := map[string]benchInfo{}
	has := func(p *pkg) (bool, map[string]bool, error) {
		if info, ok := infos[p.path]; ok {
			return info.has, info.reach, nil
		}
		ok, reach, err := benchReach(filepath.Join(root, filepath.FromSlash(p.dir)))
		if err != nil {
			return false, nil, err
		}
		infos[p.path] = benchInfo{ok, reach}
		return ok, reach, nil
	}
	for _, p := range impacted {
		ok, _, err := has(p)
		if err != nil {
			return r, err
		}
		if ok && p.listed {
			bench[p.path] = p
		}
	}
	for _, c := range testChanges {
		p := g.byDir[c.dir]
		ok, reach, err := has(p)
		if err != nil {
			return r, err
		}
		if ok && p.listed && (touchesBench(c.before, reach) || touchesBench(c.after, reach)) {
			bench[p.path] = p
		}
	}
	r.bench = patterns(bench)
	return r, nil
}

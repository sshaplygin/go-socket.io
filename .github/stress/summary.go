package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// maxLines is the most lines of one output block the summary quotes; the JSON stream has
// the rest.
const maxLines = 60

// event is one line of a go test -json stream (cmd/test2json). Build events carry
// ImportPath instead of Package.
type event struct {
	Action, Package, Test, Output, ImportPath string
}

type testState struct {
	runs, failed, skipped, open int
	current, first              []string // output of the run in progress; of the first failed run
}

type pkgState struct {
	name, seed, result string
	runs, races        int
	output, firstRace  []string // output without a test; the first race report
	capturing          bool     // inside the first race report
	raceTests          map[string]int
	tests              map[string]*testState
}

type packages map[string]*pkgState

func (pkgs packages) get(name string) *pkgState {
	p := pkgs[name]
	if p == nil {
		p = &pkgState{name: name, result: "no result", raceTests: map[string]int{}, tests: map[string]*testState{}}
		pkgs[name] = p
	}
	return p
}

func (p *pkgState) test(name string) *testState {
	t := p.tests[name]
	if t == nil {
		t = &testState{}
		p.tests[name] = t
	}
	return t
}

// summarize reads a go test -json stream and renders its Markdown summary.
func summarize(r io.Reader) (string, error) {
	pkgs, bad := packages{}, 0
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.Action == "" {
			bad++
			continue
		}
		if e.Package == "" {
			if e.Action == "build-output" { // attached to the package named before " ["
				name, _, _ := strings.Cut(e.ImportPath, " [")
				pkgs.get(name).output = append(pkgs.get(name).output, strings.TrimSuffix(e.Output, "\n"))
			}
			continue
		}
		p := pkgs.get(e.Package)
		switch e.Action {
		case "output":
			p.line(e.Test, strings.TrimSuffix(e.Output, "\n"))
		case "run":
			t := p.test(e.Test)
			t.runs, t.open, t.current, p.runs = t.runs+1, t.open+1, nil, p.runs+1
		case "pass", "fail", "skip":
			if e.Test == "" {
				p.result = e.Action
				continue
			}
			t := p.test(e.Test)
			t.open--
			if e.Action == "fail" {
				t.failed++
				if t.first == nil {
					t.first = t.current
				}
			} else if e.Action == "skip" {
				t.skipped++
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read the stream: %w", err)
	}
	return markdown(pkgs, bad), nil
}

// line records one output line of the package, attributed to test when it is not empty.
func (p *pkgState) line(test, line string) {
	if test == "" {
		if s, ok := strings.CutPrefix(line, "-test.shuffle "); ok {
			p.seed = s
		}
		p.output = append(p.output, line)
	} else {
		p.test(test).current = append(p.test(test).current, line)
	}
	if line == "WARNING: DATA RACE" {
		p.races++
		if test == "" {
			test = "no test"
		}
		p.raceTests[test]++
		p.capturing = p.firstRace == nil
	}
	if p.capturing {
		p.firstRace = append(p.firstRace, line)
		p.capturing = line != "=================="
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// details renders one collapsible item with a clipped output block.
func details(b *strings.Builder, title string, blocks ...[]string) {
	var lines []string
	for _, block := range blocks {
		lines = append(lines, block...)
	}
	if len(lines) > maxLines {
		lines = append(lines[:maxLines:maxLines], fmt.Sprintf("… %d more lines", len(lines)-maxLines))
	}
	fmt.Fprintf(b, "<details><summary>%s</summary>\n\n```text\n%s\n```\n\n</details>\n\n", title, strings.Join(lines, "\n"))
}

func plural(n int, word string) string {
	if n != 1 {
		word += "s"
	}
	return fmt.Sprintf("%d %s", n, word)
}

func markdown(pkgs packages, bad int) string {
	var b, failing, pkgFail, hung, races strings.Builder
	b.WriteString("## Stress summary\n\n| Package | Shuffle seed | Result | Test runs | Failed | Skipped | Unfinished | Race reports |\n| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	noResult := 0
	for _, name := range sortedKeys(pkgs) {
		p := pkgs[name]
		failed, skipped, open := 0, 0, 0
		for _, name := range sortedKeys(p.tests) {
			t := p.tests[name]
			failed, skipped, open = failed+t.failed, skipped+t.skipped, open+t.open
			if t.failed > 0 {
				details(&failing, fmt.Sprintf("`%s` `%s`: %d of %d runs failed", p.name, name, t.failed, t.runs), t.first)
			}
			if t.open > 0 {
				details(&hung, fmt.Sprintf("`%s` `%s` started and did not end", p.name, name), t.current, p.output)
			}
		}
		if p.result == "fail" && failed == 0 && open == 0 {
			details(&pkgFail, fmt.Sprintf("`%s` failed without a failed test", p.name), p.output)
		}
		if p.races > 0 {
			var attributed []string
			for _, name := range sortedKeys(p.raceTests) {
				attributed = append(attributed, fmt.Sprintf("%s (%d)", name, p.raceTests[name]))
			}
			details(&races, fmt.Sprintf("`%s`: %s, attributed to %s; a race inside parallel tests can be attributed to another test", p.name, plural(p.races, "race report"), strings.Join(attributed, ", ")), p.firstRace)
		}
		seed := p.seed
		if seed == "" {
			seed = "—"
		}
		if p.result == "no result" {
			noResult++
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %d | %d | %d | %d | %d |\n", p.name, seed, p.result, p.runs, failed, skipped, open, p.races)
	}
	b.WriteString("\n")
	for _, s := range []struct {
		heading string
		body    *strings.Builder
	}{{"Failing tests", &failing}, {"Package-level failures", &pkgFail}, {"Tests that never ended", &hung}, {"Race reports", &races}} {
		if s.body.Len() > 0 {
			fmt.Fprintf(&b, "### %s\n\n%s", s.heading, s.body.String())
		}
	}
	if failing.Len()+pkgFail.Len()+hung.Len()+races.Len() == 0 {
		b.WriteString("No failed tests, unfinished tests or race reports.\n\n")
	}
	if bad > 0 {
		fmt.Fprintf(&b, "%s were not go test -json events.\n\n", plural(bad, "line"))
	}
	if noResult > 0 {
		verb := map[bool]string{true: "has", false: "have"}[noResult == 1]
		fmt.Fprintf(&b, "%s %s no result; the stream may be incomplete.\n\n", plural(noResult, "package"), verb)
	}
	return b.String()
}

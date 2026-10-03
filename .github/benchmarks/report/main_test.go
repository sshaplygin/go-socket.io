package main

import (
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func number(text string) *big.Rat {
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		panic(text)
	}
	return value
}

func TestMeasurements(t *testing.T) {
	results, err := measurements(`goos: linux
pkg: example/one
BenchmarkEncode-4 100 100 ns/op 0 B/op 0 allocs/op
BenchmarkEncode-4 100 1000 ns/op 0 B/op 0 allocs/op
BenchmarkEncode-4 100 200 ns/op 0 B/op 0 allocs/op
pkg: example/two
BenchmarkEncode-4 100 9 ns/op
BenchmarkEncode-4 100 1.1e1 ns/op
PASS
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[benchmark{"example/one", "Encode-4"}].Cmp(number("200")) != 0 ||
		results[benchmark{"example/two", "Encode-4"}].Cmp(number("10")) != 0 {
		t.Fatalf("unexpected medians: %v", results)
	}
}

func TestInvalidMeasurements(t *testing.T) {
	for _, text := range []string{
		"PASS\n",
		"BenchmarkEncode-4 100 5 ns/op\n",
		"pkg: p\nBenchmarkEncode-4 0 5 ns/op\n",
		"pkg: p\nBenchmarkEncode-4 100 5 ns/op extra\n",
		"pkg: p\nBenchmarkEncode-4 100 5 B/op\n",
		"pkg: p\nBenchmarkEncode-4 100 NaN ns/op\n",
		"pkg: p\nBenchmarkEncode-4 100 Infinity ns/op\n",
		"pkg: p\nBenchmarkEncode-4 100 -1 ns/op\n",
		"pkg: p\nBenchmarkEncode-4 100 invalid ns/op\n",
	} {
		if _, err := measurements(text); err == nil {
			t.Errorf("accepted invalid measurements: %q", text)
		}
	}
}

func TestDuration(t *testing.T) {
	for _, tt := range []struct{ value, want string }{
		{"80.04", "80.04 ns"}, {"2117", "2.117 µs"}, {"2127.5", "2.128 µs"},
		{"2000000", "2 ms"}, {"3000000000", "3 s"}, {"0", "0 ns"},
		{"0.012345", "0.01235 ns"},
	} {
		if got := duration(number(tt.value)); got != tt.want {
			t.Errorf("duration(%s) = %s, want %s", tt.value, got, tt.want)
		}
	}
	if duration(nil) != "—" {
		t.Fatal("missing duration must be marked unavailable")
	}
}

func TestDelta(t *testing.T) {
	for _, tt := range []struct{ before, after, want string }{
		{"0", "0", "0"}, {"10", "0", "-100"},
		{"100", "119.95", "20.0"}, {"100", "80.05", "-20.0"},
		{"100", "99.99", "0"},
	} {
		got := delta(number(tt.before), number(tt.after))
		if got == nil || got.Cmp(number(tt.want)) != 0 {
			t.Errorf("delta(%s, %s) = %v, want %s", tt.before, tt.after, got, tt.want)
		}
	}
	if delta(number("0"), number("1")) != nil || delta(nil, number("1")) != nil || delta(number("1"), nil) != nil {
		t.Fatal("missing and nonzero-from-zero changes must be marked unavailable")
	}
}

func TestReport(t *testing.T) {
	base := make(map[benchmark]*big.Rat)
	for _, name := range []string{"slower", "faster", "same", "removed"} {
		base[benchmark{"p", name}] = number("100")
	}
	head := map[benchmark]*big.Rat{
		{"p", "slower"}: number("120"), {"p", "faster"}: number("80"),
		{"p", "same"}: number("101"), {"p", "added"}: number("50"),
	}
	report := render(base, head, strings.Repeat("a", 40), strings.Repeat("b", 40), "B/op\nallocs/op\np=0.01\n")
	for _, want := range []string{
		marker, "Base `aaaaaaaaaaaa` → PR `bbbbbbbbbbbb`", "3 benchmark(s) compared",
		"1 regression(s) at ≥20%", "1 improvement(s) at ≥20%", "2 not comparable",
		"| slower | 100 ns | 120 ns | +20.0% | ⚠️ regression |",
		"| faster | 100 ns | 80 ns | -20.0% | ✅ improved |",
		"| same | 100 ns | 101 ns | +1.0% | ℹ️ below threshold |",
		"| removed | 100 ns | — | — | ➖ not comparable |",
		"| added | — | 50 ns | — | ➖ not comparable |",
		"<details>", "B/op\nallocs/op\np=0.01",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestPackageTables(t *testing.T) {
	data := map[benchmark]*big.Rat{
		{"github.com/sshaplygin/go-socket.io/engineio/payload", "Decoder-4"}: number("200"),
		{"github.com/sshaplygin/go-socket.io/engineio/packet", "Decoder-4"}:  number("100"),
		{"github.com/sshaplygin/go-socket.io/engineio/packet", "Encoder-4"}:  number("50"),
	}
	report := render(data, data, "base", "head", "details")
	packet, payload := strings.Index(report, "### engineio/packet\n"), strings.Index(report, "### engineio/payload\n")
	if packet < 0 || payload <= packet || strings.Count(report, "| Benchmark | base | PR | change | signal |") != 2 {
		t.Fatalf("expected two sorted package tables:\n%s", report)
	}
	if !strings.Contains(report[packet:payload], "| Decoder-4 | 100 ns |") ||
		!strings.Contains(report[packet:payload], "| Encoder-4 | 50 ns |") ||
		!strings.Contains(report[payload:], "| Decoder-4 | 200 ns |") ||
		strings.Contains(report[payload:], "| Encoder-4 |") {
		t.Fatalf("benchmark assigned to the wrong table:\n%s", report)
	}
}

func TestMarkdownEscaping(t *testing.T) {
	data := map[benchmark]*big.Rat{{"p", "name|<tag>_`[x]"}: number("10")}
	report := render(data, data, "base", "head", "```\nraw details\n```")
	for _, want := range []string{`name\|&lt;tag&gt;\_\` + "`" + `\[x\]`, "````text\n```\nraw details\n```\n````", "+0.0%"} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	args := []string{filepath.Join(dir, "base.txt"), filepath.Join(dir, "pr.txt"), filepath.Join(dir, "comparison.txt"), "base", "head"}
	for _, path := range args[:3] {
		if err := os.WriteFile(path, []byte("pkg: p\nBenchmarkEncode-4 100 5 ns/op\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if report, err := run(args); err != nil || !strings.Contains(report, "| Encode-4 | 5 ns | 5 ns | +0.0% |") {
		t.Fatalf("run: report=%q, err=%v", report, err)
	}
	if _, err := run(nil); err == nil {
		t.Fatal("missing arguments must fail")
	}
	if err := os.WriteFile(args[0], nil, 0600); err != nil {
		t.Fatal(err)
	}
	if report, err := run(args); err == nil || report != "" {
		t.Fatal("empty measurements must fail without producing a report")
	}
	args[0] = filepath.Join(dir, "missing.txt")
	if report, err := run(args); err == nil || report != "" {
		t.Fatal("missing file must fail without producing a report")
	}
}

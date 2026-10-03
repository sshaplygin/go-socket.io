package main

import (
	"cmp"
	"fmt"
	"html"
	"math/big"
	"os"
	"slices"
	"strconv"
	"strings"
)

const marker = "<!-- go-socket.io:benchmark-comparison -->"
const threshold = 20

type benchmark struct {
	pkg, name string
}

func measurements(text string) (map[benchmark]*big.Rat, error) {
	samples := make(map[benchmark][]*big.Rat)
	var pkg string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "pkg: ") {
			pkg = strings.TrimSpace(strings.TrimPrefix(line, "pkg: "))
		}
		if !strings.HasPrefix(line, "Benchmark") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		iterations, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		if pkg == "" || iterations == 0 || len(fields) < 4 || len(fields)%2 != 0 {
			return nil, fmt.Errorf("invalid benchmark measurement: %s", line)
		}
		var value *big.Rat
		for i := 2; i+1 < len(fields); i += 2 {
			if fields[i+1] == "ns/op" {
				value, _ = new(big.Rat).SetString(fields[i])
				break
			}
		}
		if value == nil || value.Sign() < 0 {
			return nil, fmt.Errorf("missing or invalid ns/op measurement: %s", line)
		}
		key := benchmark{pkg, strings.TrimPrefix(fields[0], "Benchmark")}
		samples[key] = append(samples[key], value)
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("no Go benchmark time measurements found")
	}
	medians := make(map[benchmark]*big.Rat, len(samples))
	for key, values := range samples {
		slices.SortFunc(values, func(a, b *big.Rat) int { return a.Cmp(b) })
		middle := len(values) / 2
		value := new(big.Rat).Set(values[middle])
		if len(values)%2 == 0 {
			value.Add(value, values[middle-1]).Quo(value, big.NewRat(2, 1))
		}
		medians[key] = value
	}
	return medians, nil
}

func duration(value *big.Rat) string {
	if value == nil {
		return "—"
	}
	for _, unit := range []struct {
		scale int64
		name  string
	}{{1e9, "s"}, {1e6, "ms"}, {1e3, "µs"}, {1, "ns"}} {
		if value.Cmp(big.NewRat(unit.scale, 1)) < 0 && unit.scale != 1 {
			continue
		}
		scaled := new(big.Rat).Quo(value, big.NewRat(unit.scale, 1))
		integer := new(big.Int).Quo(scaled.Num(), scaled.Denom())
		precision := max(0, 4-len(integer.String()))
		for small := new(big.Rat).Set(scaled); small.Sign() > 0 && small.Cmp(big.NewRat(1, 1)) < 0; {
			precision++
			small.Mul(small, big.NewRat(10, 1))
		}
		text := scaled.FloatString(precision)
		if strings.Contains(text, ".") {
			text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
		}
		return text + " " + unit.name
	}
	panic("unreachable")
}

func delta(before, after *big.Rat) *big.Rat {
	if before == nil || after == nil || (before.Sign() == 0 && after.Sign() != 0) {
		return nil
	}
	if before.Cmp(after) == 0 {
		return new(big.Rat)
	}
	value := new(big.Rat).Sub(after, before)
	value.Quo(value, before).Mul(value, big.NewRat(100, 1))
	rounded, _ := new(big.Rat).SetString(value.FloatString(1))
	return rounded
}

func markdown(text string) string {
	return strings.NewReplacer(
		"\\", "\\\\", "|", "\\|", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]",
	).Replace(html.EscapeString(text))
}

func render(base, head map[benchmark]*big.Rat, baseSHA, headSHA, details string) string {
	keys := make(map[benchmark]bool)
	for key := range base {
		keys[key] = true
	}
	for key := range head {
		keys[key] = true
	}
	names := make([]benchmark, 0, len(keys))
	for key := range keys {
		names = append(names, key)
	}
	slices.SortFunc(names, func(a, b benchmark) int {
		if order := cmp.Compare(a.pkg, b.pkg); order != 0 {
			return order
		}
		return cmp.Compare(a.name, b.name)
	})
	var rows strings.Builder
	regressions, improvements, unavailable := 0, 0, 0
	previousPackage := ""
	for _, key := range names {
		if key.pkg != previousPackage {
			pkg := strings.TrimPrefix(key.pkg, "github.com/sshaplygin/go-socket.io/")
			fmt.Fprintf(&rows, "\n### %s\n\n| Benchmark | base | PR | change | signal |\n| --- | ---: | ---: | ---: | --- |\n", markdown(pkg))
			previousPackage = key.pkg
		}
		before, after := base[key], head[key]
		change := delta(before, after)
		changeText, signal := "—", "ℹ️ below threshold"
		switch {
		case change == nil:
			unavailable++
			signal = "➖ not comparable"
		case change.Cmp(big.NewRat(threshold, 1)) >= 0:
			regressions++
			signal = "⚠️ regression"
		case change.Cmp(big.NewRat(-threshold, 1)) <= 0:
			improvements++
			signal = "✅ improved"
		}
		if change != nil {
			changeText = change.FloatString(1) + "%"
			if change.Sign() >= 0 {
				changeText = "+" + changeText
			}
		}
		fmt.Fprintf(&rows, "| %s | %s | %s | %s | %s |\n",
			markdown(key.name), duration(before), duration(after), changeText, signal)
	}
	var report strings.Builder
	fmt.Fprintf(&report, "%s\n## 📊 Go benchmarks: base vs PR\n\n🔍 Base `%.12s` → PR `%.12s`\n\n", marker, baseSHA, headSHA)
	fmt.Fprintf(&report, "**📊 %d benchmark(s) compared · ⚠️ %d regression(s) at ≥%d%% · ✅ %d improvement(s) at ≥%d%%",
		len(names)-unavailable, regressions, threshold, improvements, threshold)
	if unavailable > 0 {
		fmt.Fprintf(&report, " · ➖ %d not comparable", unavailable)
	}
	report.WriteString("**\n\nℹ️ Time is lower-is-better. Values are medians of repeated measurements. " +
		"Both revisions ran on the same GitHub-hosted VM and Go toolchain; " +
		"±20% is an advisory marker, not a merge gate. " +
		"Markers use the displayed percentage, not a statistical significance test.\n")
	report.WriteString(rows.String())
	fence := "```"
	for strings.Contains(details, fence) {
		fence += "`"
	}
	fmt.Fprintf(&report, "\n<details>\n<summary>Full benchstat results: timings, allocations and statistical comparisons</summary>\n\n%stext\n%s\n%s\n\n</details>\n",
		fence, strings.TrimRight(details, "\n"), fence)
	return report.String()
}

func run(args []string) (string, error) {
	if len(args) != 5 {
		return "", fmt.Errorf("usage: report BASE_LOG PR_LOG BENCHSTAT BASE_SHA HEAD_SHA")
	}
	inputs := make([]string, 3)
	for i := range inputs {
		data, err := os.ReadFile(args[i])
		if err != nil {
			return "", err
		}
		inputs[i] = string(data)
	}
	base, err := measurements(inputs[0])
	if err != nil {
		return "", fmt.Errorf("%s: %w", args[0], err)
	}
	head, err := measurements(inputs[1])
	if err != nil {
		return "", fmt.Errorf("%s: %w", args[1], err)
	}
	return render(base, head, args[3], args[4], inputs[2]), nil
}

func main() {
	report, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(report)
}

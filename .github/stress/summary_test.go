package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ev renders one go test -json event. output lines are emitted as separate output events,
// as test2json does, each with its trailing newline.
func ev(pkg, action, test string, output ...string) string {
	var b strings.Builder
	for _, line := range output {
		raw, _ := json.Marshal(map[string]string{"Action": "output", "Package": pkg, "Test": test, "Output": line + "\n"})
		b.Write(raw)
		b.WriteByte('\n')
	}
	if action != "output" {
		raw, _ := json.Marshal(map[string]string{"Action": action, "Package": pkg, "Test": test})
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

// test renders a complete run of one test: run, its output, and the terminal event.
func test(pkg, name, result string, output ...string) string {
	return ev(pkg, "run", name) + ev(pkg, result, name, append([]string{"=== RUN   " + name}, output...)...)
}

func seed(pkg, seed string) string {
	return ev(pkg, "start", "") + ev(pkg, "output", "", "-test.shuffle "+seed)
}

func section(t *testing.T, md, heading string) string {
	t.Helper()
	_, rest, ok := strings.Cut(md, "### "+heading+"\n")
	if !ok {
		t.Fatalf("no section %q in:\n%s", heading, md)
	}
	if i := strings.Index(rest, "\n### "); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func mustSummarize(t *testing.T, stream string) string {
	t.Helper()
	md, err := summarize(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	return md
}

func TestSummaryCountsFailuresAndKeepsTheFirstFailureOutput(t *testing.T) {
	const a = "example.com/a"
	md := mustSummarize(t, seed(a, "42")+
		test(a, "TestPass", "pass", "--- PASS: TestPass (0.00s)")+
		test(a, "TestSkip", "skip", "    a_test.go:6: not here", "--- SKIP: TestSkip (0.00s)")+
		test(a, "TestFail", "fail", "    a_test.go:7: boom", "--- FAIL: TestFail (0.00s)")+
		test(a, "TestPass", "pass", "--- PASS: TestPass (0.00s)")+
		test(a, "TestFail", "pass", "--- PASS: TestFail (0.00s)")+
		test(a, "TestFail", "fail", "    a_test.go:7: later", "--- FAIL: TestFail (0.00s)")+
		test(a, "TestPanic", "fail", "--- FAIL: TestPanic (0.00s)", "panic: kaboom [recovered, repanicked]", "goroutine 7 [running]:")+
		ev(a, "fail", "", "FAIL", "FAIL\texample.com/a\t0.3s"))
	if want := "| `example.com/a` | 42 | fail | 7 | 3 | 1 | 0 | 0 |"; !strings.Contains(md, want) {
		t.Errorf("missing package row %q in:\n%s", want, md)
	}
	failing := section(t, md, "Failing tests")
	for _, want := range []string{"`TestFail`: 2 of 3 runs failed", "a_test.go:7: boom", "`TestPanic`: 1 of 1 runs failed", "panic: kaboom"} {
		if !strings.Contains(failing, want) {
			t.Errorf("missing %q in failing tests:\n%s", want, failing)
		}
	}
	for _, unwanted := range []string{"later", "TestPass", "TestSkip"} {
		if strings.Contains(failing, unwanted) {
			t.Errorf("unexpected %q in failing tests:\n%s", unwanted, failing)
		}
	}
	for _, heading := range []string{"Package-level failures", "Tests that never ended", "Race reports"} {
		if strings.Contains(md, "### "+heading) {
			t.Errorf("unexpected section %q in:\n%s", heading, md)
		}
	}
}

func TestSummaryReportsNothingForAPassingRun(t *testing.T) {
	const a = "example.com/a"
	md := mustSummarize(t, seed(a, "7")+test(a, "TestPass", "pass", "--- PASS: TestPass (0.00s)")+ev(a, "pass", "", "PASS", "ok\texample.com/a\t0.1s"))
	if !strings.Contains(md, "| `example.com/a` | 7 | pass | 1 | 0 | 0 | 0 | 0 |") || strings.Contains(md, "\n### ") {
		t.Errorf("unexpected summary of a passing run:\n%s", md)
	}
	if !strings.Contains(md, "No failed tests, unfinished tests or race reports.") {
		t.Errorf("missing the all-clear line in:\n%s", md)
	}
}

func TestSummaryListsTestsThatNeverEndedWithTheTimeoutPanic(t *testing.T) {
	const c = "example.com/c"
	// Go 1.25 attributes the panic to the running test; older releases print it at package
	// level. Both shapes end in a package fail without a test.
	for name, panicOwner := range map[string]string{"test output": "TestHang", "package output": ""} {
		t.Run(name, func(t *testing.T) {
			md := mustSummarize(t, seed(c, "9")+
				test(c, "TestQuick", "pass", "--- PASS: TestQuick (0.00s)")+
				ev(c, "run", "TestHang")+ev(c, "output", "TestHang", "=== RUN   TestHang")+
				ev(c, "output", panicOwner, "panic: test timed out after 2s", "\trunning tests:", "\t\tTestHang (2s)", "goroutine 20 [running]:")+
				ev(c, "fail", "", "FAIL\texample.com/c\t2.3s"))
			if want := "| `example.com/c` | 9 | fail | 2 | 0 | 0 | 1 | 0 |"; !strings.Contains(md, want) {
				t.Errorf("missing package row %q in:\n%s", want, md)
			}
			hung := section(t, md, "Tests that never ended")
			for _, want := range []string{"`TestHang` started and did not end", "panic: test timed out after 2s", "TestHang (2s)"} {
				if !strings.Contains(hung, want) {
					t.Errorf("missing %q in:\n%s", want, hung)
				}
			}
			if strings.Contains(hung, "TestQuick") || strings.Contains(md, "### Package-level failures") || strings.Contains(md, "### Failing tests") {
				t.Errorf("a timeout is neither a test failure nor a package-level failure:\n%s", md)
			}
		})
	}
}

func TestSummaryCountsRacesByPackageAndNamesTheAttributedTest(t *testing.T) {
	const b = "example.com/b"
	report := []string{"==================", "WARNING: DATA RACE", "Read at 0x00c0 by goroutine 8:", "  example.com/b.TestRaceA.func1()", "=================="}
	md := mustSummarize(t, seed(b, "3")+
		ev(b, "run", "TestRaceA")+ev(b, "pause", "TestRaceA", "=== RUN   TestRaceA", "=== PAUSE TestRaceA")+
		ev(b, "run", "TestRaceB")+ev(b, "pause", "TestRaceB", "=== RUN   TestRaceB", "=== PAUSE TestRaceB")+
		ev(b, "cont", "TestRaceA", "=== CONT  TestRaceA")+ev(b, "cont", "TestRaceB", "=== CONT  TestRaceB")+
		ev(b, "output", "TestRaceB", report...)+
		ev(b, "fail", "TestRaceA", "    testing.go:1617: race detected during execution of test", "--- FAIL: TestRaceA (0.01s)")+
		ev(b, "fail", "TestRaceB", "    testing.go:1617: race detected during execution of test", "--- FAIL: TestRaceB (0.02s)")+
		ev(b, "fail", "", "FAIL", "FAIL\texample.com/b\t0.4s"))
	if want := "| `example.com/b` | 3 | fail | 2 | 2 | 0 | 0 | 1 |"; !strings.Contains(md, want) {
		t.Errorf("missing package row %q in:\n%s", want, md)
	}
	races := section(t, md, "Race reports")
	for _, want := range []string{"`example.com/b`: 1 race report", "TestRaceB (1)", "attributed to another test", "example.com/b.TestRaceA.func1()"} {
		if !strings.Contains(races, want) {
			t.Errorf("missing %q in race reports:\n%s", want, races)
		}
	}
	failing := section(t, md, "Failing tests")
	if !strings.Contains(failing, "`TestRaceA`: 1 of 1 runs failed") || !strings.Contains(failing, "`TestRaceB`: 1 of 1 runs failed") {
		t.Errorf("both tests failed:\n%s", failing)
	}
}

func TestSummaryReportsPackageLevelFailures(t *testing.T) {
	const d, e = "example.com/d", "example.com/e"
	build := `{"ImportPath":"example.com/d [example.com/d.test]","Action":"build-output","Output":"# example.com/d [example.com/d.test]\n"}` + "\n" +
		`{"ImportPath":"example.com/d [example.com/d.test]","Action":"build-output","Output":"d/d.go:3:23: cannot use \"no\" (untyped string constant) as int value\n"}` + "\n" +
		`{"ImportPath":"example.com/d [example.com/d.test]","Action":"build-fail"}` + "\n"
	md := mustSummarize(t, build+
		ev(d, "start", "")+ev(d, "fail", "", "FAIL\texample.com/d [build failed]")+
		seed(e, "5")+test(e, "TestOK", "pass", "--- PASS: TestOK (0.00s)")+
		ev(e, "fail", "", "==================", "WARNING: DATA RACE", "Write at 0x00c1 by goroutine 12:", "  example.com/e.leak()", "==================", "FAIL", "FAIL\texample.com/e\t0.2s"))
	failures := section(t, md, "Package-level failures")
	for _, want := range []string{"`example.com/d`", "[build failed]", "d/d.go:3:23: cannot use", "`example.com/e`", "example.com/e.leak()"} {
		if !strings.Contains(failures, want) {
			t.Errorf("missing %q in package-level failures:\n%s", want, failures)
		}
	}
	if !strings.Contains(md, "| `example.com/d` | — | fail | 0 | 0 | 0 | 0 | 0 |") || !strings.Contains(md, "| `example.com/e` | 5 | fail | 1 | 0 | 0 | 0 | 1 |") {
		t.Errorf("unexpected package rows:\n%s", md)
	}
	if !strings.Contains(section(t, md, "Race reports"), "`example.com/e`: 1 race report") || strings.Contains(md, "### Failing tests") {
		t.Errorf("a race outside a test is a race report, not a test failure:\n%s", md)
	}
}

func TestSummaryKeepsInterleavedPackagesApart(t *testing.T) {
	const a, b = "example.com/a", "example.com/b"
	// Parallel packages interleave their events; the same test name exists in both.
	stream := seed(a, "11") + seed(b, "22") +
		ev(a, "run", "TestX") + ev(b, "run", "TestX") +
		ev(a, "output", "TestX", "=== RUN   TestX", "    x_test.go:1: from a") + ev(b, "output", "TestX", "=== RUN   TestX", "    x_test.go:1: from b") +
		ev(b, "pass", "TestX", "--- PASS: TestX (0.00s)") + ev(a, "fail", "TestX", "--- FAIL: TestX (0.00s)") +
		ev(b, "pass", "", "PASS", "ok\texample.com/b\t0.1s") + ev(a, "fail", "", "FAIL", "FAIL\texample.com/a\t0.1s") +
		"go: downloading something\n" + // not an event: stderr merged into the stream
		seed("example.com/f", "33") + `{"Action":"run","Package":"example.com/f","Te` // truncated last line
	md := mustSummarize(t, stream)
	for _, want := range []string{
		"| `example.com/a` | 11 | fail | 1 | 1 | 0 | 0 | 0 |",
		"| `example.com/b` | 22 | pass | 1 | 0 | 0 | 0 | 0 |",
		"| `example.com/f` | 33 | no result | 0 | 0 | 0 | 0 | 0 |",
		"2 lines were not go test -json events",
		"1 package has no result; the stream may be incomplete",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	failing := section(t, md, "Failing tests")
	if !strings.Contains(failing, "`example.com/a` `TestX`: 1 of 1 runs failed") || !strings.Contains(failing, "from a") || strings.Contains(failing, "from b") {
		t.Errorf("output of another package leaked into the failure:\n%s", failing)
	}
}

func TestSummaryClipsLongOutput(t *testing.T) {
	const a = "example.com/a"
	lines := make([]string, 0, maxLines+10)
	for i := 0; i < maxLines+10; i++ {
		lines = append(lines, "line "+strings.Repeat("x", i%7))
	}
	md := mustSummarize(t, seed(a, "1")+ev(a, "run", "TestLong")+ev(a, "fail", "TestLong", lines...)+ev(a, "fail", "", "FAIL"))
	if strings.Count(md, "line ") != maxLines || !strings.Contains(md, "… 10 more lines") {
		t.Errorf("output not clipped to %d lines:\n%s", maxLines, md)
	}
}

func TestRunWritesTheSummaryFileAndStdout(t *testing.T) {
	dir := t.TempDir()
	in, out := filepath.Join(dir, "stress.json"), filepath.Join(dir, "summary.out")
	const a = "example.com/a"
	if err := os.WriteFile(in, []byte(seed(a, "1")+test(a, "TestFail", "fail", "--- FAIL: TestFail (0.00s)")+ev(a, "fail", "", "FAIL")), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout strings.Builder
	if err := run([]string{"-json", in, "-summary", out}, nil, &stdout); err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(file) != stdout.String() || !strings.Contains(stdout.String(), "`TestFail`: 1 of 1 runs failed") {
		t.Errorf("stdout:\n%s\nfile:\n%s", stdout.String(), file)
	}
	if err := run([]string{"-json", filepath.Join(dir, "missing.json")}, nil, &stdout); err == nil {
		t.Error("a missing stream is an error")
	}
	if err := run([]string{"-json", "-"}, strings.NewReader(seed(a, "2")+ev(a, "pass", "", "ok")), &stdout); err != nil || !strings.Contains(stdout.String(), "| `example.com/a` | 2 | pass |") {
		t.Errorf("stdin stream: %v\n%s", err, stdout.String())
	}
}

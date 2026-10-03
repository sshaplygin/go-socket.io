from decimal import Decimal
import unittest

from report import delta, duration, measurements, render


class ReportTest(unittest.TestCase):
    def test_medians_and_package_identity(self):
        results = measurements("""goos: linux
pkg: example/one
BenchmarkEncode-4 100 100 ns/op 0 B/op 0 allocs/op
BenchmarkEncode-4 100 1000 ns/op 0 B/op 0 allocs/op
BenchmarkEncode-4 100 200 ns/op 0 B/op 0 allocs/op
pkg: example/two
BenchmarkEncode-4 100 9 ns/op
BenchmarkEncode-4 100 11 ns/op
PASS
""")
        self.assertEqual(results, {
            ("example/one", "Encode-4"): Decimal(200),
            ("example/two", "Encode-4"): Decimal(10),
        })

    def test_invalid_measurements_fail(self):
        for text in [
            "PASS\n",
            "BenchmarkEncode-4 100 5 ns/op\n",
            "pkg: p\nBenchmarkEncode-4 0 5 ns/op\n",
            "pkg: p\nBenchmarkEncode-4 100 5 ns/op extra\n",
            "pkg: p\nBenchmarkEncode-4 100 5 B/op\n",
            *[f"pkg: p\nBenchmarkEncode-4 100 {value} ns/op\n"
              for value in ["NaN", "Infinity", "-1", "invalid"]],
        ]:
            with self.subTest(text=text), self.assertRaises((ValueError, ArithmeticError)):
                measurements(text)

    def test_duration_units(self):
        for value, expected in [
            ("80.04", "80.04 ns"), ("2117", "2.117 µs"),
            ("2127.5", "2.128 µs"),
            ("2000000", "2 ms"), ("3000000000", "3 s"), ("0", "0 ns"),
        ]:
            self.assertEqual(duration(Decimal(value)), expected)
        self.assertEqual(duration(None), "—")

    def test_report_signals_and_missing_benchmarks(self):
        base = {("p", name): Decimal(100) for name in ["slower", "faster", "same", "removed"]}
        head = {("p", name): Decimal(value) for name, value in [
            ("slower", 120), ("faster", 80), ("same", 101), ("added", 50),
        ]}
        report = render(base, head, "a" * 40, "b" * 40, "B/op\nallocs/op\np=0.01\n")
        self.assertIn("Base `aaaaaaaaaaaa` → PR `bbbbbbbbbbbb`", report)
        self.assertIn("3 benchmark(s) compared", report)
        self.assertIn("1 regression(s) at ≥20%", report)
        self.assertIn("1 improvement(s) at ≥20%", report)
        self.assertIn("2 not comparable", report)
        self.assertIn("| p/slower | 100 ns | 120 ns | +20.0% | ⚠️ regression |", report)
        self.assertIn("| p/faster | 100 ns | 80 ns | -20.0% | ✅ improved |", report)
        self.assertIn("| p/same | 100 ns | 101 ns | +1.0% | ℹ️ below threshold |", report)
        self.assertIn("| p/removed | 100 ns | — | — | ➖ not comparable |", report)
        self.assertIn("| p/added | — | 50 ns | — | ➖ not comparable |", report)
        self.assertIn("<details>", report)
        self.assertIn("B/op\nallocs/op\np=0.01", report)

    def test_zero_baseline_and_rounding(self):
        for before, after, expected in [
            ("0", "0", "0"), ("10", "0", "-100"),
            ("100", "119.95", "20.0"), ("100", "80.05", "-20.0"),
            ("100", "99.99", "0"),
        ]:
            self.assertEqual(delta(Decimal(before), Decimal(after)), Decimal(expected))
        self.assertIsNone(delta(Decimal(0), Decimal(1)))
        self.assertIsNone(delta(None, Decimal(1)))
        self.assertIsNone(delta(Decimal(1), None))

    def test_escape_benchmark_labels_and_preserve_details(self):
        data = {("p", "name|<tag>_`[x]"): Decimal(10)}
        report = render(data, data, "base", "head", "```\nraw details\n```")
        self.assertIn(r"p/name\|&lt;tag&gt;\_\`\[x\]", report)
        self.assertIn("````text\n```\nraw details\n```\n````", report)
        self.assertIn("+0.0%", report)


if __name__ == "__main__":
    unittest.main()

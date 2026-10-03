import argparse
from collections import defaultdict
from decimal import Decimal, ROUND_HALF_UP
import html
from pathlib import Path
import re
from statistics import median


MARKER = "<!-- go-socket.io:benchmark-comparison -->"
THRESHOLD = Decimal(20)


def measurements(text):
    samples = defaultdict(list)
    package = None
    for line in text.splitlines():
        if line.startswith("pkg: "):
            package = line[5:].strip()
        if not line.startswith("Benchmark"):
            continue
        fields = line.split()
        if len(fields) < 2 or not fields[1].isdigit():
            continue
        if not package or int(fields[1]) <= 0 or len(fields) < 4 or len(fields) % 2:
            raise ValueError(f"invalid benchmark measurement: {line}")
        metrics = dict(zip(fields[3::2], fields[2::2]))
        if "ns/op" not in metrics:
            raise ValueError(f"missing ns/op measurement: {line}")
        value = Decimal(metrics["ns/op"])
        if not value.is_finite() or value < 0:
            raise ValueError(f"invalid ns/op measurement: {line}")
        samples[package, fields[0].removeprefix("Benchmark")].append(value)
    if not samples:
        raise ValueError("no Go benchmark time measurements found")
    return {key: median(values) for key, values in samples.items()}


def duration(value):
    if value is None:
        return "—"
    for scale, unit in [(10**9, "s"), (10**6, "ms"), (10**3, "µs"), (1, "ns")]:
        if value >= scale or scale == 1:
            return f"{value / scale:.4g} {unit}"


def delta(before, after):
    if before is None or after is None or (before == 0 and after != 0):
        return None
    if before == after:
        return Decimal(0)
    value = ((after / before - 1) * 100).quantize(Decimal("0.1"), rounding=ROUND_HALF_UP)
    return value if value else Decimal(0)


def markdown(text):
    return re.sub(r"([\\|`*_\[\]])", r"\\\1", html.escape(text, quote=False))


def render(base, head, base_sha, head_sha, details):
    rows = []
    regressions = improvements = unavailable = 0
    names = sorted(base.keys() | head.keys())
    for key in names:
        before, after = base.get(key), head.get(key)
        change = delta(before, after)
        if change is None:
            unavailable += 1
            signal = "➖ not comparable"
        elif change >= THRESHOLD:
            regressions += 1
            signal = "⚠️ regression"
        elif change <= -THRESHOLD:
            improvements += 1
            signal = "✅ improved"
        else:
            signal = "ℹ️ below threshold"
        package, name = key
        package = package.removeprefix("github.com/sshaplygin/go-socket.io/")
        label = markdown(f"{package}/{name}")
        change_text = "—" if change is None else f"{change:+.1f}%"
        rows.append(f"| {label} | {duration(before)} | {duration(after)} | {change_text} | {signal} |")

    summary = [
        f"📊 {len(names) - unavailable} benchmark(s) compared",
        f"⚠️ {regressions} regression(s) at ≥{THRESHOLD}%",
        f"✅ {improvements} improvement(s) at ≥{THRESHOLD}%",
    ]
    if unavailable:
        summary.append(f"➖ {unavailable} not comparable")
    fence = "`" * max(3, 1 + max((len(s) for s in re.findall(r"`+", details)), default=0))
    return "\n".join([
        MARKER,
        "## 📊 Go benchmarks: base vs PR",
        "",
        f"🔍 Base `{base_sha[:12]}` → PR `{head_sha[:12]}`",
        "",
        f"**{' · '.join(summary)}**",
        "",
        "ℹ️ Time is lower-is-better. Values are medians of repeated measurements. "
        "Both revisions ran on the same GitHub-hosted VM and Go toolchain; "
        "±20% is an advisory marker, not a merge gate. "
        "Markers use the displayed percentage, not a statistical significance test.",
        "",
        "| Benchmark | base | PR | change | signal |",
        "| --- | ---: | ---: | ---: | --- |",
        *rows,
        "",
        "<details>",
        "<summary>Full benchstat results: timings, allocations and statistical comparisons</summary>",
        "",
        f"{fence}text",
        details.rstrip(),
        fence,
        "",
        "</details>",
        "",
    ])


def main():
    parser = argparse.ArgumentParser(description="Render the Go benchmark PR comparison.")
    parser.add_argument("base", type=Path)
    parser.add_argument("head", type=Path)
    parser.add_argument("comparison", type=Path)
    parser.add_argument("base_sha")
    parser.add_argument("head_sha")
    args = parser.parse_args()
    try:
        report = render(
            measurements(args.base.read_text(encoding="utf-8")),
            measurements(args.head.read_text(encoding="utf-8")),
            args.base_sha,
            args.head_sha,
            args.comparison.read_text(encoding="utf-8"),
        )
    except (OSError, ValueError, ArithmeticError) as error:
        parser.exit(1, f"{error}\n")
    print(report, end="")


if __name__ == "__main__":
    main()

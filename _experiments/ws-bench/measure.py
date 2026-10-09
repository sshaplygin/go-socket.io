#!/usr/bin/env python3
"""Run bounded, sequential comparisons and retain every raw result."""

import argparse
import json
import os
import pathlib
import platform
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=pathlib.Path)
    parser.add_argument("--repeats", type=int, default=3)
    parser.add_argument("--idle-connections", type=int, default=1000)
    args = parser.parse_args()
    if not 1 <= args.repeats <= 10 or not 1 <= args.idle_connections <= 10000:
        parser.error("repeats must be 1..10 and idle connections 1..10000")
    binary = pathlib.Path(__file__).resolve().parent / "ws-bench"
    env = dict(os.environ, GOMAXPROCS="4")
    cases = [(1, 32, 5000), (1, 1024, 5000), (1, 65536, 2000), (16, 1024, 1000), (16, 65536, 500)]
    output = {
        "platform": platform.platform(),
        "gomaxprocs": 4,
        "repeats": args.repeats,
        "note": "Sequential fresh server processes; alternate backend order by repeat. CPU/RSS not measured.",
        "runs": [],
    }
    # Exclusive create avoids silently overwriting a previous measurement.
    with args.output.open("x") as destination:
        for repeat in range(args.repeats):
            backends = ["gorilla", "gobwas-prototype"]
            if repeat % 2:
                backends.reverse()
            workloads = [("echo", c, size, messages) for c, size, messages in cases]
            workloads.append(("idle", args.idle_connections, 1024, 1))
            for mode, connections, size, messages in workloads:
                for backend in backends:
                    command = [str(binary), "-backend", backend, "-mode", mode,
                               "-connections", str(connections), "-size", str(size),
                               "-messages", str(messages), "-timeout", "60s"]
                    result = subprocess.run(command, env=env, text=True, capture_output=True, timeout=65)
                    if result.returncode:
                        output["failure"] = {"command": command, "stderr": result.stderr}
                        json.dump(output, destination, indent=2)
                        destination.write("\n")
                        raise SystemExit(f"measurement failed: {result.stderr}")
                    record = json.loads(result.stdout)
                    record["repeat"] = repeat + 1
                    output["runs"].append(record)
        json.dump(output, destination, indent=2)
        destination.write("\n")


if __name__ == "__main__":
    main()

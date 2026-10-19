#!/usr/bin/env bash
set -euo pipefail
: "${CONFORMANCE_REDIS_ADDR:?set CONFORMANCE_REDIS_ADDR to a real Redis address}"
: "${CONFORMANCE_REDIS_SOCKET:?set CONFORMANCE_REDIS_SOCKET to a real Redis Unix socket}"
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d)
pids=()
cleanup() {
  for pid in ${pids[@]+"${pids[@]}"}; do kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true; done
  rm -rf "$scratch"
}
trap cleanup EXIT
for example in redis-adapter redis-adapter-unix-socket; do
  address=$CONFORMANCE_REDIS_ADDR
  [[ $example != redis-adapter-unix-socket ]] || address=$CONFORMANCE_REDIS_SOCKET
  echo "==> $example two-server Node chat"
  (cd "$root/_examples/$example" && go build -o "$scratch/server" .)
  urls=()
  prefix="socketio-example-$$-$RANDOM"
  for side in 0 1; do
    (cd "$root/_examples/$example" && exec "$scratch/server" -addr 127.0.0.1:0 -redis "$address" -prefix "$prefix") >"$scratch/$side.log" 2>&1 &
    pid=$!; pids+=("$pid")
    url=
    for ((attempt=0; attempt<100; attempt++)); do
      if ! kill -0 "$pid" 2>/dev/null; then cat "$scratch/$side.log" >&2; exit 1; fi
      url=$(sed -n 's/^Listening on \(http:\/\/127\.0\.0\.1:[0-9]*\)$/\1/p' "$scratch/$side.log")
      [[ -z $url ]] || break
      sleep 0.1
    done
    [[ -n $url ]] || { cat "$scratch/$side.log" >&2; exit 1; }
    urls+=("$url")
  done
  if ! node "$root/_examples/default-http/client.js" "${urls[0]}" "${urls[1]}"; then cat "$scratch/0.log" "$scratch/1.log" >&2; exit 1; fi
  for pid in ${pids[@]+"${pids[@]}"}; do kill "$pid"; wait "$pid" 2>/dev/null || true; done
  pids=()
done

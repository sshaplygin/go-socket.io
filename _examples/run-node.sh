#!/usr/bin/env bash
set -euo pipefail
if [[ -z ${CONFORMANCE_NODE_MODULES:-} ]]; then
  echo 'examples-node: set CONFORMANCE_NODE_MODULES to the pinned socket.io-client node_modules directory' >&2
  exit 1
fi
case "$CONFORMANCE_NODE_MODULES" in
  /*) ;;
  *) echo 'examples-node: CONFORMANCE_NODE_MODULES must be absolute' >&2; exit 1 ;;
esac
export NODE_PATH
NODE_PATH=$(cd "$CONFORMANCE_NODE_MODULES" && pwd)
node -e 'require("socket.io-client")'
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d)
server_pid=
cleanup() {
  if [[ -n $server_pid ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
  rm -rf "$scratch"
}
trap cleanup EXIT
for script in "$root"/_examples/*/client.js; do
  example=${script%/client.js}
  echo "==> ${example##*/} Node client"
  (cd "$example" && go build -o "$scratch/server" .)
  (cd "$example" && exec "$scratch/server" -addr 127.0.0.1:0) >"$scratch/server.log" 2>&1 &
  server_pid=$!
  url=
  for ((attempt=0; attempt<100; attempt++)); do
    if ! kill -0 "$server_pid" 2>/dev/null; then cat "$scratch/server.log" >&2; exit 1; fi
    url=$(sed -n 's/^Listening on \(http:\/\/127\.0\.0\.1:[0-9]*\)$/\1/p' "$scratch/server.log")
    [[ -z $url ]] || break
    sleep 0.1
  done
  if [[ -z $url ]]; then echo "example did not become ready: $example" >&2; cat "$scratch/server.log" >&2; exit 1; fi
  if ! node "$script" "$url"; then cat "$scratch/server.log" >&2; exit 1; fi
  kill "$server_pid"
  wait "$server_pid" 2>/dev/null || true
  server_pid=
done

bash "$root/_examples/run-redis-node.sh"

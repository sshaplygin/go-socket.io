#!/usr/bin/env bash
set -euo pipefail

script="$(dirname "$0")/aggregate.sh"
check() {
  local name="$1" expected="$2" results="$3" actual
  if RESULTS="$results" bash "$script" >/dev/null 2>&1; then
    actual=pass
  else
    actual=fail
  fi
  if [[ "$actual" != "$expected" ]]; then
    printf '%s: got %s, want %s\n' "$name" "$actual" "$expected" >&2
    exit 1
  fi
}

check success pass '{"changes":{"result":"success"},"test":{"result":"success"}}'
check skipped pass '{"changes":{"result":"success"},"test":{"result":"skipped"}}'
check all-skipped pass '{"test":{"result":"skipped"},"lint":{"result":"skipped"}}'
check failure fail '{"changes":{"result":"success"},"test":{"result":"failure"},"lint":{"result":"skipped"}}'
check cancelled fail '{"changes":{"result":"success"},"test":{"result":"cancelled"},"lint":{"result":"skipped"}}'
check classifier-failure fail '{"changes":{"result":"failure"},"test":{"result":"skipped"}}'
check unknown fail '{"test":{"result":"pending"}}'
check missing-result fail '{"test":{}}'
check empty-object fail '{}'
check array fail '[{"result":"success"}]'
check invalid-json fail '{'
check missing-input fail ''
printf 'aggregate result checks passed\n'

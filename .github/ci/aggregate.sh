#!/usr/bin/env bash
set -euo pipefail

# Fail closed on absent or malformed results as well as unsuccessful jobs.
printf '%s\n' "${RESULTS:?missing dependency results}" | jq -e '
  type == "object" and length > 0 and
  all(.[]; .result == "success" or .result == "skipped")
'

#!/usr/bin/env bash
set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.."

# Inherited upstream tags declare another module path and are not v1 baselines
# for this fork. An explicit base is useful for reproducing a release check.
base=${API_BASE:-}
if [[ -z "$base" ]]; then
  while IFS= read -r tag; do
    [[ "$tag" =~ ^v1\.[0-9]+\.[0-9]+$ ]] || continue
    module=$(git show "$tag:go.mod" 2>/dev/null | awk '$1 == "module" { print $2; exit }') || continue
    if [[ "$module" == github.com/sshaplygin/go-socket.io ]]; then
      base=$tag
      break
    fi
  done < <(git tag --list 'v1.*' --sort=-version:refname)
fi
if [[ -z "$base" ]]; then
  echo 'API compatibility: no published fork v1 baseline yet; the gate activates with v1.5.0.'
  exit 0
fi

# gorelease exits unsuccessfully on incompatible changes in a stable v1 module.
go run golang.org/x/exp/cmd/gorelease@v0.0.0-20261014143641-f0251eb4aa3e -base="$base"

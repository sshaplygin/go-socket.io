# Contributing

How to build, test and where documentation lives: [CLAUDE.md](CLAUDE.md). What is
planned and in which order: [docs/ROADMAP.md](docs/ROADMAP.md).

## Pull requests

1. Open an issue first for anything larger than a bug fix, so the change can be
   matched against the roadmap.
2. Branch from `master`, one topic per branch, named `<area>/<short-topic>`
   (for example `engineio/payload-v4`, `redis/request-timeout`). A fix for the v1 line
   branches from `v1.x` and targets `v1.x` once that branch exists (see Releases).
3. Every PR: `make lint test` green locally, a test for each fix, `CHANGELOG.md`
   entry under `Unreleased`, docs updated in their owner file only.
4. Commit messages and PR descriptions state the problem, the change, and what was
   verified with which command. No generated footers or trailers.
5. Squash-merge. The PR title becomes the commit subject. A PR is merged with its
   commits kept only when [docs/ROADMAP.md](docs/ROADMAP.md) names it, for a series of
   moves and edits whose separate commits must survive (rename detection).

## Releases

- Tags follow SemVer. The v1 line lives on the branch `v1.x`, cut from `master`
  without a tag (see the roadmap, Stage 1b). The branch is not named `v1`:
  `go list -m <module>@v1` is a semver prefix query that resolves the tag `v1.4.2` and
  ignores branches, while `@v1.x` resolves the branch tip. `v1.x` tags, `v1.5.0` first, are cut
  from `v1.x` only, on the owner's command; `master` is not tagged `v1.x`. Fixes for the
  v1 line land on `v1.x` only. A `v1.x` fix that also applies to `master` is
  forward-ported by its author in a separate PR to `master`, written against `master`'s
  layout, with its own test and the subject suffix `(forward-port of #<N>)`; the roadmap
  may hold forward-ports while a refactoring that renames files is in flight. `v2.x`
  tags are cut from `master`.
- Sub-modules under `adapters/` are tagged as `adapters/<name>/vX.Y.Z`.
- Before tagging: move the `Unreleased` section of `CHANGELOG.md` under the new
  version with the date, run the full CI matrix, and record benchmark numbers the
  roadmap asks for in the changelog entry.
- Until `v1.5.0` is tagged, pkg.go.dev links and `go get` commands use `@v1.x`: the
  fork's `v1.4.x` tags carry the upstream module path, and an unversioned `go get`
  resolves `v1.4.2` and fails. The `v1.5.0` release commit is made on `v1.x` when the
  owner orders the tag. It renames the `CHANGELOG.md` heading `Unreleased` to `v1.5.0`,
  switches `@v1.x` to `@v1.5.0` in the pkg.go.dev links of `README.md`,
  `engineio/README.md` and the released `CHANGELOG.md` section and in the `README.md`
  install command, and removes the `README.md` sentence saying to use the branch until a
  release is tagged. The tag is created on that commit.

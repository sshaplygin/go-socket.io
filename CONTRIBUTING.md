# Contributing

How to build, test and where documentation lives: [CLAUDE.md](CLAUDE.md). What is
planned and in which order: [docs/ROADMAP.md](docs/ROADMAP.md).

## Pull requests

1. Open an issue first for anything larger than a bug fix, so the change can be
   matched against the roadmap.
2. Branch from `master`, one topic per branch, named `<area>/<short-topic>`
   (for example `engineio/payload-v4`, `redis/request-timeout`). A `v1.5.x` fix branches
   from `v1` and targets `v1` (see Releases).
3. Every PR: `make lint test` green locally, a test for each fix, `CHANGELOG.md`
   entry under `Unreleased`, docs updated in their owner file only.
4. Commit messages and PR descriptions state the problem, the change, and what was
   verified with which command. No generated footers or trailers.
5. Squash-merge. The PR title becomes the commit subject. The Stage 1b refactoring PRs
   (steps 1–4 in [docs/ROADMAP.md](docs/ROADMAP.md)) keep their commits instead, so
   that renames stay pure moves.

## Releases

- Tags follow SemVer. `v1.x` tags are cut from `master` up to `v1.5.0` and from the
  branch `v1` afterwards. Stage 1b cuts `v1` from the `v1.5.0` release commit; `v1.5.x`
  patches land on `v1` only, and from the first Stage 1b commit `master` is never tagged
  `v1.x` (its API breaks v1 consumers). `v2.x` tags are cut from `master`.
- Sub-modules under `adapters/` are tagged as `adapters/<name>/vX.Y.Z`.
- Before tagging: move the `Unreleased` section of `CHANGELOG.md` under the new
  version with the date, run the full CI matrix, and record benchmark numbers the
  roadmap asks for in the changelog entry.
- Until `v1.5.0` is tagged, pkg.go.dev links and `go get` commands use `@master`: the
  fork's `v1.4.x` tags carry the upstream module path, and an unversioned `go get`
  resolves `v1.4.2` and fails. The `v1.5.0` release commit switches the pkg.go.dev
  links in `README.md`, `engineio/README.md` and the released `CHANGELOG.md` section
  to `@v1.5.0`, changes the `README.md` install command to `@v1.5.0`, and removes the
  `README.md` sentence saying to use `@master` until a release is tagged.

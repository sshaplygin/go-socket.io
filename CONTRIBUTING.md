# Contributing

How to build, test and where documentation lives: [CLAUDE.md](CLAUDE.md). What is
planned and in which order: [docs/ROADMAP.md](docs/ROADMAP.md).

## Pull requests

1. Open an issue first for anything larger than a bug fix, so the change can be
   matched against the roadmap.
2. Branch from `master`, one topic per branch, named `<area>/<short-topic>`
   (for example `engineio/payload-v4`, `redis/request-timeout`).
3. Every PR: `make lint test` green locally, a test for each fix, `CHANGELOG.md`
   entry under `Unreleased`, docs updated in their owner file only.
4. Commit messages and PR descriptions state the problem, the change, and what was
   verified with which command. No generated footers or trailers.
5. Squash-merge. The PR title becomes the commit subject.

## Releases

- Tags follow SemVer. `v1.x` tags are cut from `master` until v2 lands; after that
  `master` is v2 and the `v1` branch receives fixes only.
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

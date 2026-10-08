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
   entry under `Unreleased` (unless the roadmap exempts the PR and names where its
   change is recorded), docs updated in their owner file only.
4. Commit messages and PR descriptions state the problem, the change, and what was
   verified with which command. No generated footers or trailers.
5. Squash-merge. The PR title becomes the commit subject. A PR is merged with its
   commits kept only when [docs/ROADMAP.md](docs/ROADMAP.md) names it, for a series of
   moves and edits whose separate commits must survive (rename detection). Those PRs
   use "Rebase and merge", which needs the repository setting "Allow rebase merging"
   and adds no merge commit; the commit SHAs change, so the roadmap's gates read
   `master`, not the PR branch.

## Releases

This section owns the tagging rule, the `v1.x` branch rule, the release-commit
procedure and the `CHANGELOG.md` handling across branches; the roadmap links here.

- Tags follow SemVer. The v1 line lives on the branch `v1.x`, not `v1`:
  `go list -m <module>@v1` is a semver prefix query that resolves the highest `v1.*` tag
  (`v1.4.2`) and ignores branches, while `@v1.x` resolves the branch tip as a
  pseudo-version. `v1.x` tags, `v1.5.0` first, are cut from `v1.x` only, on the owner's
  command; `master` is not tagged `v1.x`, so consumers of `@master` follow v2 work.
  `v2.x` tags are cut from `master`. Fixes for the v1 line land on `v1.x` only. A
  `v1.x` fix that also applies to `master` is forward-ported by its author in a separate
  PR to `master`, written against `master`'s layout, with its own test and the subject
  suffix `(forward-port of #<N>)` and no `CHANGELOG.md` entry (the original's entry
  reaches `master` with the release forward-port, see below); the roadmap may hold forward-ports while a
  refactoring that renames files is in flight.
- Sub-modules under `adapters/` are tagged as `adapters/<name>/vX.Y.Z`.
- Before tagging: rename the `Unreleased` heading of `CHANGELOG.md` to
  `## <version> - <date>` (the section moves under the new version), run the full CI
  matrix, and record benchmark numbers the roadmap asks for in the changelog entry.
- While a line has no tag with the fork's module path (the `v1.4.x` tags carry the
  upstream path, and an unversioned `go get` resolves `v1.4.2` and fails), pkg.go.dev
  links and `go get` commands name the branch: `@v1.x`. The first release commit
  (`v1.5.0`) is made on `v1.x` when the owner orders the tag: it renames the heading as
  above, switches `@v1.x` to `@v1.5.0` in the pkg.go.dev links of `README.md`,
  `engineio/README.md` and the released `CHANGELOG.md` section and in the `README.md`
  install command, and removes the `README.md` sentence that tells users to use the
  branch until a release is tagged. The tag is created on that commit.
- `CHANGELOG.md` across branches: each branch writes entries under its own `Unreleased`
  heading, and an entry that a branch already carries from the other one is never
  repeated under `master`'s `Unreleased`. The release commit is forward-ported to
  `master` in a separate PR that makes `master`'s section for that release equal to the
  released one on `v1.x`. A tag on `master` moves only `master`'s own `Unreleased`
  entries. Where the roadmap exempts a PR from the entry, it says where the change is
  recorded instead.

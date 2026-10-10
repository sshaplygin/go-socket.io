# Contributing

How to build, test and where documentation lives: [CLAUDE.md](CLAUDE.md). What is
planned and in which order: [docs/ROADMAP.md](docs/ROADMAP.md).

## Pull requests

1. Open an issue first for anything larger than a bug fix, so the change can be
   matched against the roadmap.
2. Branch from `master`, one topic per branch, named `<area>/<short-topic>`
   (for example `engineio/payload-v4`, `redis/request-timeout`). Both lines target
   `master`: a v1 change edits the files at the repository root, a v2 change edits `v2/`.
3. Every PR: `make lint test` green locally in each module it touches (`make -C v2 lint test`
   for `v2/`), a test for each fix, a `CHANGELOG.md` entry under `Unreleased` in the changelog
   of that module (the root file for v1, `v2/CHANGELOG.md` for v2; unless the roadmap
   exempts the PR and names where its change is recorded), docs updated in their owner file only.
4. Commit messages and PR descriptions state the problem, the change, and what was
   verified with which command. No generated footers or trailers.
5. Squash-merge. The PR title becomes the commit subject. A PR is merged with its
   commits kept only when [docs/ROADMAP.md](docs/ROADMAP.md) names it, for a series of
   moves and edits whose separate commits must survive (rename detection). Those PRs
   use "Rebase and merge", which needs the repository setting "Allow rebase merging"
   and adds no merge commit; the commit SHAs change, so the roadmap's gates read
   `master`, not the PR branch.

## Releases

This section owns the release-commit procedure and the `CHANGELOG.md` handling of the two
modules. The tag policy and the rule of the frozen `v1.x` branch are decisions of
[docs/ROADMAP.md](docs/ROADMAP.md#repository-layout); the bullets below summarize them.

- Two modules on one branch, `master`: v1 at the repository root
  (`github.com/sshaplygin/go-socket.io`) and v2 in `v2/`
  (`github.com/sshaplygin/go-socket.io/v2`). Tags follow SemVer and the forms of
  [docs/ROADMAP.md](docs/ROADMAP.md#repository-layout): `v1.X.Y` for the root module,
  `v2.X.Y` for the v2 module (no `v2/` prefix), `v2/<dir>/vN.X.Y` for a module nested in
  `v2/` (for example `v2/contrib/otel/v2.0.0`, whose module path ends in `/v2`).
  `v2/v2.0.0` and `contrib/otel/v2.0.0` (no `v2/` prefix) do not resolve and are not used.
- No tag of any module is created until the owner declares that `master` fully supports
  the v1 protocol line (Socket.IO v4 / Engine.IO v3) with example implementations. Then
  `v1.5.0` is tagged on `master` on the owner's explicit order, and v2 tags follow it,
  each on the owner's order. Tags are cut from `master` only.
- The branch `v1.x` is frozen at the tree the repository root was restored from: no PR
  targets it, no workflow or Dependabot entry of `master` targets it, and nothing is
  forward-ported from it. Its fate is the owner's later decision.
- Before tagging: rename the `Unreleased` heading of that module's `CHANGELOG.md` to
  `## <version> - <date>` (the section moves under the new version), run the full CI
  matrix, and record benchmark numbers the roadmap asks for in the changelog entry.
- `CHANGELOG.md` per module: the root file records v1, `v2/CHANGELOG.md` records v2, and
  an entry is never repeated in the other. Where the roadmap exempts a PR from the
  entry, it says where the change is recorded instead.
- While a module has no tag with the fork's module path (the `v1.4.x` tags carry the
  upstream path, and an unversioned `go get` resolves `v1.4.2` and fails), `go get`
  commands name the branch: `@master` (`go get github.com/sshaplygin/go-socket.io@master`,
  `go get github.com/sshaplygin/go-socket.io/v2@master`). pkg.go.dev links carry no
  version. The first release commit (`v1.5.0`) is made on `master` when the owner orders
  the tag: it renames the heading of the root `CHANGELOG.md` as above, switches `@master`
  to `@v1.5.0` in the root `README.md` install command, adds `@v1.5.0` to the pkg.go.dev
  links of `README.md`, `engineio/README.md` and the released `CHANGELOG.md` section, and
  removes the `README.md` sentence that tells users to use `master` until a release is
  tagged. The tag is created on that commit.

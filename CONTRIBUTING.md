# Contributing

How to build, test and where documentation lives: [CLAUDE.md](CLAUDE.md). What is
planned and in which order: [docs/ROADMAP.md](docs/ROADMAP.md).

## Pull requests

1. Open an issue first for anything larger than a bug fix, so the change can be
   matched against the roadmap.
2. Branch from `master`, one topic per branch, named `<area>/<short-topic>`
   (for example `engineio/payload-v4`, `redis/request-timeout`); a PR of a work package uses
   its registry branch prefix in place of `<area>/` (for example `v1-a/namespace-api`). Both lines target
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
6. A bug fix is test first. The PR's first commit adds only the tests that show the bug,
   each under the `// Covers <ID>` marker of its `docs/PARITY.md` row when it has one, and
   they fail on that commit; a later commit of the same PR fixes the code and makes them pass.
   A failure that counts (red) is an assertion failure, a panic (a test timeout included) or a
   runtime abort (`fatal error:`, such as a concurrent map write or a stack overflow), never a
   build or setup failure of the test. The PR body names that commit
   (`red commit: <sha>`) and each test (`fails without the fix: <TestName>`). This is how
   "a test for each fix" (item 3) and "every fix carries a test that fails without it"
   ([CLAUDE.md](CLAUDE.md), *Conventions*) are shown: the PR body and the PR's commit list
   keep the evidence after the squash merge, the CI run of the first commit is expected to be
   red, and the check is the `tdd` helper of [docs/ROADMAP.md](docs/ROADMAP.md) (Stage V1,
   *Wave V1G*), run on the PR head before the merge.

## Work packages

The remaining Stage V1 work is split into packages that the owner gives to separate
takers, people or agents: their PRs, paths, entry and exit gates, the parallel matrix and
the registry are in [docs/ROADMAP.md](docs/ROADMAP.md#stage-v1-work-packages).

**Taking and handing back.** A package has one taker at a time. The registry is edited only
by the coordinator: the owner, or the one person or agent the owner names in writing to
coordinate the packages. A taker never edits it and never opens a registry PR. Every
registry change is a PR of the coordinator that changes only registry rows; the coordinator
merges registry PRs one at a time, and merges one itself only as the owner or with the
owner's merge authorization in writing (*Rules*). A registry PR edits `docs/ROADMAP.md`, so
it goes through the usual review with the three reviewers like any roadmap edit (CLAUDE.md,
*Roadmap validation*); a pure status edit is not exempt.

- `open` to `taken`: the coordinator sets the status and the taker's name only once the
  entry check of the package's next unmerged PR passes on `master`; the registry PR body
  quotes that command and its exit status, and the coordinator re-runs it on the registry
  PR's head, with `master` merged in, right before merging. So `taken` means that the next
  PR was free to start when the row was set, which is how the entry fence reads the status
  (`sy` and the SYNC arm), and C and SY are never `taken` together. Only after that registry
  PR has merged does the owner start the taker, with the prompt below; a row that is
  `taken` is not started by anyone else.
- After each merged PR of the package, the taker reports it (*Reporting back*) and the
  coordinator sets the last merged PR in the row. When that PR was the package's last, the
  same registry PR sets the row to `done`; `done` needs no entry check, since no PR of the
  package is left. While a later PR of the package waits for a merge in another package
  (B's V1-16 for A's V1-4, F's V1-13 for V1-10, V1-11 and V1-19), the row stays `taken`
  with its last merged PR: no gate reads the `taken` status of B, D, E, F or G as started
  (the entry fence reads `taken` only of SY and C, and of the others only `done`).
- When the taker stops, or the owner stops it, the taker's unmerged PRs of the package are
  closed first (by the taker, or else by the coordinator), and then the coordinator sets the
  row back to `open` with the last merged PR, so that `open` never stands beside a PR in
  flight; the next taker continues from there. `open` needs no entry check either.

**Reading list.** [CLAUDE.md](CLAUDE.md); this file; the whole roadmap section above, its
paragraphs included (the path block is the widest set: the *Paths* paragraph narrows it per
PR and the paragraph after the block excludes paths; the paragraphs after the `O`-pair table
set the integrator fallback and the order of SY); for each PR of the package its row
of the Stage V1 PR table, its wave row, its `### V1-<n> contract` if it has one, the Stage V1
per-PR DoD and the checks named for that PR, the section its scope row points to
(*Conformance contract*, Stage 7, *Shared-file drift check*, ...), and the rows of
[docs/PARITY.md](docs/PARITY.md) whose `Plan` names the PR. Nothing else is required.

**Rules, without exception.**

- Every PR is reviewed by three independent reviewers, QA, Critic and Reviewer (the roles
  of CLAUDE.md *Roadmap validation*): read-only, none sees another's findings before
  submitting its own, at most three iterations; each finding is fixed in its own commit.
  A PR still rejected after the third iteration stops and goes to the owner.
- The executor and the reviewers use the default tier of agent the owner sets; the
  strongest tier only after three rejected iterations of one PR and the owner's go.
- A PR merges only with the three approvals of its last iteration, reached within three
  iterations, and every CI job green. The owner merges it. A taker merges its own PR only
  when the owner has given that taker merge authorization in writing, in an owner comment
  on the PR itself, and only on those terms; without such a comment the taker never merges.
- No commit message, PR text, review comment or file names or credits an assistant, a
  model or its provider (footers and trailers: *Pull requests* item 4).
- No tag and no version bump: tags and the release commit follow *Releases*, on the
  owner's order only.
- `master`, `v1.x` and any branch another person pushed to are never rewritten or
  force-pushed; `master` reaches an open PR branch by a merge, not by a rebase of pushed
  commits. Where a wave row says that a PR rebases on another (V1-16 and V1-17), it means
  that the PR starts from, or merges in, a `master` that has the other.
- Repository text is English.
- A PR edits only its package's paths and, by row, the files the roadmap section names for
  every package. A change outside them, or of a contract, is a plan change: stop and ask
  the owner (CLAUDE.md: public API goes through the roadmap first).
- The entry check of a PR passes on `master` before the PR starts, and its exit gate on
  the head before review.

**Shared files.** Where the roadmap names an integrator for a file, the worker's PR carries
its own hunk, lists the files in a body line `integrator: <package> <path>...`, and merges
after the integrator's taker has commented `integrated: <path>...` on it; when an open PR
of the integrator edits the same file, the worker's PR merges after it, with `master`
merged in and the DoD re-run. When the integrating package has no taker, the roadmap's
[fallback](docs/ROADMAP.md#stage-v1-work-packages) applies (the paragraph after the `O`-pair
table). Entry checks gate starts, not merges: before merging a PR of a package in an `O`
pair, the merger checks the Order column of the roadmap's `O`-pair table on the current
`master`, and a PR whose order is not met yet waits. In `docs/PARITY.md` and `CHANGELOG.md` a
PR edits only its own rows; a conflict there keeps both sides. The registry is the
coordinator's alone (*Taking and handing back*).

**Reporting back,** to the owner, per PR: the PR number and head SHA; the result of every
CI job; the entry and exit commands run, with their output or counts; the three verdicts
of each review iteration and the commit of each fixed finding; what was not run and why;
whether the PR has merged; the command and output of the entry check of the package's next
PR on `master` after the merge, or that the PR was the package's last; any blocker, with the
options and a recommendation. From this report the coordinator updates the registry row.

**Prompt for a taker.** Replace `<PKG>`, `<PR>`, `<TAKER>` and `<BRANCH>` (the registry's
branch prefix and a short topic):

```text
Repository github.com/sshaplygin/go-socket.io, base branch master. You are <TAKER>, the
taker of work package <PKG> (docs/ROADMAP.md, "Stage V1 work packages"), and you do PR
<PR> of it, nothing else.

Read first, and only: CLAUDE.md; CONTRIBUTING.md, "Pull requests" and "Work packages";
the reading list of "Work packages" for <PKG> and <PR>.

You never edit the registry and never open a registry PR: the coordinator does
("Taking and handing back").

Before you edit: the registry row <PKG> on origin/master says taken by <TAKER>, and the
entry check of <PR> (the one command after the Entry fence of the roadmap section, with
<PR>) prints "entry: 0" on origin/master. If the row is not taken by <TAKER>, stop and
report. If the entry check fails, start nothing, report its output and stop; the row stays
taken while <PR> waits.

Work in your own git worktree on branch <BRANCH> from origin/master. Edit only the paths
of <PKG> that the section allows <PR> (path block, narrowed by its paragraphs), plus, by row, the PARITY.md rows of <PR>, its CHANGELOG.md entries and nothing
of another PR. Follow every rule of "Work packages" without exception. Title the commits
and the PR as the Stage V1 PR sequence requires.

Done when: the Exit gate of <PKG> for <PR> (the packages table) passes on the head,
the PR to master is open with a factual body (problem, change, what was verified with
which command and its numbers, what was not run), the three reviewers approved within
three iterations, CI is green, and the PR has merged or waits for the owner's merge. Then
report back as "Work packages" lists, with the PR number, whether it has merged, and the
entry check of the package's next PR on origin/master (or that <PR> was its last). If you
stop before that, close your unmerged PRs and report that you stopped.
```

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

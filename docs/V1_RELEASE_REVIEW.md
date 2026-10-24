# V1 release review

This review checks whether the v1 module `github.com/sshaplygin/go-socket.io` is ready
to be published as `v1.5.0`. It was carried out on 2026-10-21 against `master` at
`7311fbe`. The `v2/` module is out of scope.

A follow-up validation is recorded in [Follow-up validation](#follow-up-validation). It
ran on 2026-10-21 against `ebc9f5e`, the head of PR #144, and was fact-checked on
2026-10-22 against master's squash commit `0fa1ead`, which has the identical tree. Both
differ from `7311fbe` only in this review and its CLAUDE.md ownership entry, so the
runtime code is identical. Corrections are made in the sections they concern; the
Follow-up validation section records which findings were reproduced and which were only
source-confirmed. Counts and timings elsewhere come from the original `7311fbe` runs
unless that section reproduced them.

The document has three parts:

- **Findings:** the [verdict](#verdict-ready-after-fixes) and [scorecard](#scorecard),
  then [Release blockers](#release-blockers) through the
  [finding catalogue](#finding-catalogue). They come from the original review and were
  corrected where the follow-up validation and the 2026-10-22 fact-check required it.
- **[Work plan](#work-plan):** pull-request-sized work items (W1–W44) and owner decisions
  (D1–D6) that cover every catalogue ID except the editorial `CL-14` (not planned), with
  their order, acceptance criteria and status.
- **[Follow-up validation](#follow-up-validation):** the evidence log, with a status and
  the evidence for each finding from the follow-up and the fact-check.

## Method

Eight independent reviews were run, one per dimension:

1. clarity
2. architecture
3. tests
4. Go API compatibility
5. protocol interoperability
6. performance
7. correctness, concurrency and security
8. release engineering

Every blocker and major finding was then re-checked by a separate reviewer. The re-checker
tried to refute the finding: they re-read the code, re-ran the commands and, where
possible, reproduced the defect. Minor and nit findings were not re-checked. A final pass
merged duplicates across dimensions and resolved contradictions.

A follow-up validation then re-ran the release-relevant checks and assessed all 85
catalogue entries, and a fact-check on 2026-10-22 re-ran its probes and measurements. Both
are recorded in the [evidence log](#follow-up-validation). The [work plan](#work-plan) was
written from the corrected findings.

Severity levels:

- **blocker:** must be fixed before `v1.5.0` is tagged.
- **major:** should be fixed before the tag, or deferred explicitly in writing.
- **minor:** fix soon after the release.
- **nit:** cosmetic.

Finding IDs (`CL-`, `ARCH-`, `T`, `API-`, `interop-`, `PERF-`, `C`, `REL-`) refer to the
[finding catalogue](#finding-catalogue).

Terms: ACK is a Socket.IO acknowledgement (a callback answered by the peer); SID is the
Engine.IO session ID; EIO3 is Engine.IO protocol version 3, used by socket.io 2.x; sumdb
is the Go checksum database; the websocket "nag" is the per-frame goroutine and 30 s timer
that logs a DEBUG record when a frame stays open; `dupl` and `gocyclo` are Go
clone-detection and cyclomatic-complexity tools. Upstream is
`github.com/googollee/go-socket.io` (latest stable release `v1.7.0`; `v1.4.2` used the
separate `github.com/googollee/go-engine.io` `v1.4.1`); the previous master is `3dd094d`,
the master commit before #143; `ST1000` and `ST1020`–`ST1022` are staticcheck
documentation checks.

## Verdict: ready after fixes

The engineering base is strong:

- Every repository gate passes: lint, vet, architecture, parity, coverage at 93.5–93.6%
  (varies by run), race, stress and examples.
- The conformance suite passes 62/62 with both `socket.io-client` 1.7.4 and 2.5.0.
- The three committed decoder fuzz targets ran 0.88–4.2 million executions each without a
  crash (see [Fuzzing](#fuzzing)).
- The package graph matches [ARCHITECTURE.md](ARCHITECTURE.md).

One confirmed blocker remains: [B1](#b1-permanent-session-deadlock).

Seven major findings should be fixed before the release or explicitly deferred:

- four concern documentation and release communication (M1–M3, M7);
- three concern runtime behavior or performance (M4–M6).

`v1.5.0` is the first usable release of this module path, so it also fixes the public
API surface for the life of v1. Settle the API choices in
[Decisions required before tagging](#decisions-required-before-tagging) before the tag:
once tagged, later v1 releases can add to the API but cannot remove or change it.
Keeping the current behavior is a valid outcome.

The [work plan](#work-plan) sets the order: [W1](#w1-fix-the-permanent-session-deadlock) for B1, W2–W10 before the tag, and
D1–D6 for these decisions.

## Scorecard

| Dimension | Score | Verdict | Reason |
| --- | --- | --- | --- |
| Clarity | 5/10 | ready with fixes | **Good:** reference docs are accurate and every `V1.md` snippet compiles. **Gaps:** there is no inline quick start or migration note, 9 of 12 packages have no package doc, and the `OnError` and Redis callback contracts are documented incorrectly. |
| Architecture | 7/10 | ready with fixes | **Good:** the package graph is clean, there is no dead code, and a consumer of the root package depends only on gorilla/websocket. **Gaps:** the adapter, alias and typed-event contracts need decisions before the API freeze. |
| Tests | 7/10 | ready | **Good:** coverage is 93.5–93.6% (varies by run), tests check behavior, and race, stress and fuzz runs are green. **Gaps:** there is no goroutine-leak gate, and no scenario has a non-reading client that keeps sending packets (pings or messages) while the server emits, which triggers B1. |
| API compatibility | 4/10 | not ready until migration docs exist | **Status:** semver holds formally, because the module path is new. **Problem:** the upstream API breaks, the changed defaults and the Redis wire break have no migration notes. |
| Interoperability | 8/10 | ready with fixes | **Good:** 62/62 conformance and 7/7 `examples-node` with each pinned client. **Gaps:** `forceBase64` over websocket is not supported, and several wire differences are undocumented. |
| Performance | 6/10 | ready with fixes | **Good:** outbound queues are bounded and slow consumers are evicted cleanly. **Gaps:** the JSON encoder from #143 regresses broadcast cost by an ad hoc measurement (no committed benchmark yet; see [M6](#m6-json-encoder-from-143-regresses-encode-cost), W2 and W3), Redis `PUBLISH` can block indefinitely, and nothing benchmarks Socket.IO-level code. |
| Correctness and security | 6/10 | not ready | **Good:** decoders are robust, limits are on by default, and race runs are clean. **Problem:** any client can trigger a session deadlock (B1). |
| Release engineering | 7/10 | ready with fixes | **Good:** CI is green on `7311fbe` and `go.mod` is tidy. **Gaps:** release notes are missing, the version number and inherited tags need a decision, and the stress job never runs on push. |

## Work plan

This plan turns every finding in the [finding catalogue](#finding-catalogue), except
`CL-14` (not planned), into work items of pull-request size. Facts about each defect are owned by the section the item
links to (B1, M1–M7, Decisions, Backlog, catalogue); an item repeats only what is needed
to do the work. The status of each item is owned by its **Status** line and nowhere else.

### How to use this plan

**Execution order.**

1. Start [W1](#w1-fix-the-permanent-session-deadlock) (the blocker) and
   [W2](#w2-add-socketio-level-benchmarks) (benchmarks) in parallel.
2. After W2 merges: [W3](#w3-restore-encoder-performance) and
   [W4](#w4-bound-redis-broadcast-publication); [W5](#w5-report-redis-broadcast-failures)
   after W4 (same file).
3. Any time: [W6](#w6-document-the-cors-default),
   [W7](#w7-document-the-redis-wire-break-and-upgrade-procedure),
   [W9](#w9-add-a-lifecycle-sketch-and-runnable-examples).
   [W8](#w8-write-the-migration-guide) after W5, W6, W7 and D1–D6 (including any API PR
   they add), because it links to the first three and describes the tagged API.
4. The owner decides [D1](#d1-version-number-and-inherited-tags)–[D6](#d6-exported-test-helpers)
   as early as possible. A decision to change the API adds its implementation as a
   before-tag PR, recorded in that D item's Status line (see Marking progress).
5. [W10](#w10-prepare-the-release-commit) last; it is the release PR. Only the owner tags.
6. After the tag: W11–W44 in numeric order unless **Depends on** says otherwise. Every
   performance item needs W2 merged first. W12 lands before W23. W38 needs the tag.

A major (M1–M7) that is not fixed before the tag is deferred only in writing: the owner
sets the item's Status to `deferred — <reason> (<date>)` and the release notes state the
deferral (for M6, the encode cost from W2's `BenchmarkEncode/structs100` and
`BenchmarkBroadcastWebsocket/structs100-10`, measured with the manual comparison against
`3dd094d` below and quoted with its command).

**Conventions** (from CONTRIBUTING.md and CLAUDE.md).

- Run `git fetch origin` and branch from `origin/master`, one topic per branch and PR; a
  local `master` branch may have diverged from `origin/master` and must not be used as a
  base. Do not rewrite `master`, `v1.x` or a published branch of someone else.
- The PR description states the problem, the resulting behavior and the validation, with
  the real output of the commands that were run, and names checks that were not run. No
  generated footers or trailers.
- A bug fix includes a behavioral regression test and shows that it fails without the fix:
  run the new test on a branch from `origin/master` that contains only the test, and quote
  the failure in the PR.
- Concurrency changes are verified with `-race`, and with
  `make test-stress PKGS='<affected packages>'` for the packages in `STRESS_PKGS`
  (Makefile).
- Before review, run `make lint`, `make test`, `make test-race`, `make gates` and
  `make cover-check` (floor 91.7%, 0 uncovered exported functions). Run `make examples`
  when an example or public behavior used by an example changes, and `make conformance`
  (Node and Redis, see CLAUDE.md "Interoperability") when wire behavior changes. CI runs
  the selected jobs; validate CI selection changes with `go test ./.github/changes`.
- Update the owning document from the CLAUDE.md documentation table instead of repeating a
  fact in several files. Public API changes also update godoc and the consumer fixtures in
  `internal/parity`.
- No tag, release, branch deletion, repository-settings change or git-identity change
  without the owner. The owner, or someone the owner has explicitly delegated to, merges
  by squash merge unless the owner says otherwise.

In this plan, "on master" and "fails on master" mean the current `origin/master` after
`git fetch origin`, at the time of the PR.

**Performance rule.** Every performance change or claim is captured by a benchmark
committed to the repository: a `Benchmark*` function in the owning package. The Benchmarks
workflow (`.github/workflows/benchmarks.yml`, pull requests to `master` only) benchmarks, on
the PR base and head, every package with benchmarks that the PR changes or that depends on
a changed package, plus packages whose benchmark code changed (`.github/changes`). A change
under `.github/benchmarks/` or to the workflow benchmarks every package. A new push cancels
a running comparison, and a push that changes no measured package since the previous head
skips it. So the last push that touched measured code must have a completed run: if it was
cancelled, re-run it (`gh run rerun <run-id>`) before quoting the report. Unless an item
names another base, the base revision is the PR base and the command is the workflow's
`go test -mod=readonly -run '^$' -bench . -benchmem -count=10 <selected packages>`; an item
that relies on a custom metric quotes a manual run of
`SOCKETIO_BENCH_METRICS=1 go test -run '^$' -bench '<name>' -benchmem -count=10 <pkg>` on
base and head, compared with `benchstat`. A performance fix is done only when that
comparison, or a manual `benchstat` run against the base revision the item names, shows the
before/after on a committed benchmark. Numbers from throwaway benchmarks may appear in this
review only when labelled "ad hoc measurement (no committed benchmark yet)" and linked to
the item that adds the committed benchmark. A value that no committed benchmark can ever
capture, such as a `v1.4.2` baseline (the benchmarks use API that `v1.4.2` lacks, see
[W25](#w25-remove-extra-json-passes-from-decoding)), is labelled "historical ad hoc
measurement; not reproducible by a committed benchmark"; it is context only and is never
quoted as a performance claim, in a PR or in the release notes. The workflow's report accepts only the units
`ns/op`, `B/op`, `allocs/op` and `MB/s`; any custom metric (`b.ReportMetric`) is emitted
only when the environment variable `SOCKETIO_BENCH_METRICS=1` is set, following
`v2/engineio/idle_bench_test.go`, which reports its custom metrics only when `IDLE_CONNS`
is set.

Manual comparison against a fixed revision (used by W3):

```sh
base=$(mktemp -d)
git worktree add "$base" 3dd094d
cp parser/bench_test.go "$base/parser/"
cp broadcast_bench_test.go "$base/"
(cd "$base" && go test -run '^$' -bench 'Encode|BroadcastWebsocket' -benchmem -count=10 ./parser . ) > old.txt
go test -run '^$' -bench 'Encode|BroadcastWebsocket' -benchmem -count=10 ./parser . > new.txt
benchstat old.txt new.txt   # golang.org/x/perf/cmd/benchstat
git worktree remove --force "$base"
```

Copy only benchmark files that compile at the base revision; both files above use only
public API that exists at `3dd094d`.

**Marking progress.** Progress of the `v1.5.0` release work is tracked in this document,
by owner decision of 2026-10-22. This is the single exception to the CONTRIBUTING.md rule
that v1 has no standalone changelog or completed-stage ledger, and CLAUDE.md assigns this
file the findings, the work plan and its progress. Status values:

- W items: `open` → `in progress — #<PR>` → `done — #<PR>`. The item's own PR sets
  `in progress — #<PR>` (its own number). After the merge, the owner, or the next PR that
  touches this document, changes it to `done — #<PR>`. No merge commit SHA is ever
  required, because the squash-merge commit does not exist while the PR is open. A
  performance item's PR adds the result of its benchmark comparison to the Status line, for
  example `in progress — #150; BenchmarkEncode/structs100: <base> → <head> allocs/op`, and
  the result stays when the Status becomes `done`. An item that lands as several PRs lists
  each one (`in progress — #<PR1>, #<PR2>`) and becomes `done` after the last one merges.
- D items: `open (awaiting owner decision)` → `decided — option <X>[ + <Y>] (<date>)`,
  recorded by the owner, for example `decided — option A + R1 (2026-10-25)` for D1. When
  the decision needs a PR, that PR appends `; in progress — #<PR>`, and after the merge the
  owner or the next PR touching this document changes it to `; done — #<PR>`. When a W item
  implements the decision (W10 implements D1), the W item's PR is that PR: it appends
  `; in progress — #<PR>` to the D item's Status line as well as setting its own.
- Deferral of a major: `deferred — <reason> (<date>)`, recorded by the owner (see above).

When the fix merges, the [finding catalogue](#finding-catalogue) row of each finding the item
lists carries `Fixed by #<PR> (W<n>).` (or `(D<n>)` for a D item's PR) at the end of its
Recommendation cell. The item's own PR appends the note, so it lands with the merge. A W
item that implements a D decision also appends `Fixed by #<PR> (D<n>).` to the rows of that
D item's findings (W10 does this for D1's API-2, REL-7 and CL-10). A finding that is closed
only by an owner-only step after the merge (W10: REL-2, REL-3 and REL-11) gets no note from
the item's PR; the owner appends `Fixed by #<PR> (W<n>).` once the step is done, and a
finding whose optional step is never done keeps no note.

Apart from its own Status line, its `Fixed by` notes, the Status line and `Fixed by` notes
of the D item it implements, and `done` updates of items whose PRs
have already merged, the item's PR changes no part of this document. Apart from the Status
lines and the `Fixed by` notes, do not rewrite the review's findings, the Backlog or the Follow-up
validation evidence log when an item is done; they record the state at `7311fbe`. This
progress tracking is not a v1 changelog: after the tag, the owner decides whether the
remaining items (W11–W44) move to GitHub issues, and this document then keeps only the
findings and the evidence.

**Standard definition of done** (every item refers to it): the PR is squash-merged by the
owner or the owner's explicit delegate; the CI workflow is green on the merge commit, and
the Benchmarks workflow ran on the PR where the item touches a measured package; the owning
documents are updated; the item's PR set its Status line to `in progress — #<PR>` and
appended the `Fixed by` notes as described in Marking progress, and the Status line was
changed to `done — #<PR>` after the merge.

**Item index.**

| Item | Title | Priority | Findings | Depends on |
| --- | --- | --- | --- | --- |
| [W1](#w1-fix-the-permanent-session-deadlock) | Fix the permanent session deadlock | blocker | C1, REL-12 | — |
| [W2](#w2-add-socketio-level-benchmarks) | Add Socket.IO-level benchmarks | before tag | PERF-10 | — |
| [W3](#w3-restore-encoder-performance) | Restore encoder performance | before tag | PERF-1, T1 | W2 |
| [W4](#w4-bound-redis-broadcast-publication) | Bound Redis broadcast publication | before tag | PERF-3 | W2 |
| [W5](#w5-report-redis-broadcast-failures) | Report Redis broadcast failures | before tag | ARCH-1, CL-2 | W4 |
| [W6](#w6-document-the-cors-default) | Document the CORS default | before tag | API-3, C5, CL-9, interop-9 | — |
| [W7](#w7-document-the-redis-wire-break-and-upgrade-procedure) | Document the Redis wire break and upgrade procedure | before tag | API-4 | — |
| [W8](#w8-write-the-migration-guide) | Write the migration guide | before tag | API-1, CL-4, REL-1, API-5, API-6, API-7, API-8, API-13 | W5, W6, W7, D1–D6 |
| [W9](#w9-add-a-lifecycle-sketch-and-runnable-examples) | Add a lifecycle sketch and runnable examples | before tag | CL-1 | — |
| [W10](#w10-prepare-the-release-commit) | Prepare the release commit | before tag | API-10, REL-6, REL-2, REL-3, REL-11 | W1, W2; W3–W9 or written deferrals; D1–D6 |
| [D1](#d1-version-number-and-inherited-tags) | Version number and inherited tags | owner decision | API-2, REL-7, CL-10 | — |
| [D2](#d2-aliases-to-internal-packages) | Aliases to internal packages | owner decision | ARCH-2, API-9 | — |
| [D3](#d3-typed-events-and-client-bridge-methods) | Typed events and client bridge methods | owner decision | ARCH-3, CL-11 | W9 (option A docs) |
| [D4](#d4-adapter-contract) | Adapter contract | owner decision | ARCH-5 | W5 (same comments) |
| [D5](#d5-engine-connection-access) | Engine connection access | owner decision | ARCH-6 | — |
| [D6](#d6-exported-test-helpers) | Exported test helpers | owner decision | ARCH-11, API-12 | — |
| [W11](#w11-reject-duplicate-session-ids) | Reject duplicate session IDs | after release | C3 | — |
| [W12](#w12-close-frame-readers-on-errors-and-gate-goroutine-leaks) | Close frame readers on errors and gate goroutine leaks | after release | T2 | — |
| [W13](#w13-document-and-log-the-onerror-contract) | Document and log the OnError contract | after release | CL-3, C4 | — |
| [W14](#w14-document-dynamic-namespace-retention) | Document dynamic namespace retention | after release | C2, ARCH-9 | — |
| [W15](#w15-support-engineio-base64-over-websocket) | Support Engine.IO base64 over websocket | after release | interop-1 | W6 |
| [W16](#w16-document-the-remaining-wire-differences) | Document the remaining wire differences | after release | interop-2, interop-3, interop-4, interop-5, interop-6 | W6 |
| [W17](#w17-conformance-heartbeat-and-redis-teardown) | Conformance heartbeat and Redis teardown | after release | T3, interop-7, interop-8 | — |
| [W18](#w18-stop-per-packet-polling-timers) | Stop per-packet polling timers | after release | PERF-2 | W2 |
| [W19](#w19-size-the-redis-publishing-pool) | Size the Redis publishing pool | after release | PERF-4 | W2, W4 |
| [W20](#w20-give-the-polling-client-its-own-http-transport) | Give the polling client its own HTTP transport | after release | PERF-5 | W2 |
| [W21](#w21-prepare-broadcasts-once-and-skip-the-single-room-dedup-map) | Prepare broadcasts once and skip the single-room dedup map | after release | PERF-6, PERF-12 | W2 |
| [W22](#w22-copy-packets-only-when-an-ack-id-is-added) | Copy packets only when an ACK ID is added | after release | PERF-7 | W2 |
| [W23](#w23-remove-per-frame-websocket-timers) | Remove per-frame websocket timers | after release | PERF-8 | W2, W12 |
| [W24](#w24-look-up-room-members-without-a-scan) | Look up room members without a scan | after release | PERF-9 | W2 |
| [W25](#w25-remove-extra-json-passes-from-decoding) | Remove extra JSON passes from decoding | after release | PERF-11 | W2 |
| [W26](#w26-reduce-the-idle-connection-footprint) | Reduce the idle connection footprint | after release | ARCH-4, PERF-13, ARCH-13 | W2 |
| [W27](#w27-package-documentation-and-lint-checks) | Package documentation and lint checks | after release | CL-5 | W9 |
| [W28](#w28-adapter-godoc-and-redis-log-keys) | Adapter godoc and Redis log keys | after release | CL-6, CL-7, ARCH-12 | — |
| [W29](#w29-bound-redis-registration-time-in-tests-and-docs) | Bound Redis registration time in tests and docs | after release | T4 | — |
| [W30](#w30-document-which-server-api-to-use) | Document which server API to use | after release | CL-8 | — |
| [W31](#w31-document-the-close-drain-and-upgrade-state-machines) | Document the close, drain and upgrade state machines | after release | CL-12 | — |
| [W32](#w32-cancellable-pause-and-explicit-close-state) | Cancellable pause and explicit close state | after release | ARCH-8 | W31 |
| [W33](#w33-pair-or-share-role-neutral-runtime-pieces) | Pair or share role-neutral runtime pieces | after release | ARCH-7 | — |
| [W34](#w34-example-module-hygiene) | Example module hygiene | after release | CL-13, REL-10 | — |
| [W35](#w35-encoder-and-round-trip-fuzz-targets) | Encoder and round-trip fuzz targets | after release | T5 | W3 |
| [W36](#w36-replace-sleeps-in-pauser-tests) | Replace sleeps in pauser tests | after release | T6 | — |
| [W37](#w37-ci-job-with-goexperimentjsonv2) | CI job with GOEXPERIMENT=jsonv2 | after release | ARCH-10 | — |
| [W38](#w38-api-compatibility-gate) | API compatibility gate | after release | API-11 | tag |
| [W39](#w39-stress-on-push-and-an-aggregate-required-check) | Stress on push and an aggregate required check | after release | REL-4, REL-5 | — |
| [W40](#w40-pin-ci-tools-and-move-actions-off-node-20) | Pin CI tools and move actions off Node 20 | after release | REL-9 | — |
| [W41](#w41-compile-tests-on-32-bit-platforms) | Compile tests on 32-bit platforms | after release | REL-8 | — |
| [W42](#w42-fail-the-benchmark-comparison-on-lost-samples) | Fail the benchmark comparison on lost samples | after release | REL-13 | — |
| [W43](#w43-harden-the-w1-locking-documentation-and-regression-test) | Harden the W1 locking documentation and regression test | after release | T7, CL-15 | — |
| [W44](#w44-isolate-the-testpollingbinary-nil-reader-crash) | Isolate the TestPollingBinary nil-reader crash | after release | T8 | — |

**Finding coverage.** Every catalogue ID appears in exactly one item or in "Not planned".

| Catalogue section | Mapping |
| --- | --- |
| Correctness and security | C1 W1 · C2 W14 · C3 W11 · C4 W13 · C5 W6 |
| API compatibility | API-1 W8 · API-2 D1 · API-3 W6 · API-4 W7 · API-5 W8 · API-6 W8 · API-7 W8 · API-8 W8 · API-9 D2 · API-10 W10 · API-11 W38 · API-12 D6 · API-13 W8 |
| Clarity | CL-1 W9 · CL-2 W5 · CL-3 W13 · CL-4 W8 · CL-5 W27 · CL-6 W28 · CL-7 W28 · CL-8 W30 · CL-9 W6 · CL-10 D1 · CL-11 D3 · CL-12 W31 · CL-13 W34 · CL-14 not planned · CL-15 W43 |
| Architecture | ARCH-1 W5 · ARCH-2 D2 · ARCH-3 D3 · ARCH-4 W26 · ARCH-5 D4 · ARCH-6 D5 · ARCH-7 W33 · ARCH-8 W32 · ARCH-9 W14 · ARCH-10 W37 · ARCH-11 D6 · ARCH-12 W28 · ARCH-13 W26 |
| Performance | PERF-1 W3 · PERF-2 W18 · PERF-3 W4 · PERF-4 W19 · PERF-5 W20 · PERF-6 W21 · PERF-7 W22 · PERF-8 W23 · PERF-9 W24 · PERF-10 W2 · PERF-11 W25 · PERF-12 W21 · PERF-13 W26 |
| Interoperability | interop-1 W15 · interop-2 W16 · interop-3 W16 · interop-4 W16 · interop-5 W16 · interop-6 W16 · interop-7 W17 · interop-8 W17 · interop-9 W6 |
| Tests | T1 W3 · T2 W12 · T3 W17 · T4 W29 · T5 W35 · T6 W36 · T7 W43 · T8 W44 |
| Release engineering | REL-1 W8 · REL-2 W10 · REL-3 W10 · REL-4 W39 · REL-5 W39 · REL-6 W10 · REL-7 D1 · REL-8 W41 · REL-9 W40 · REL-10 W34 · REL-11 W10 · REL-12 W1 · REL-13 W42 |

**Not planned.**

- `CL-14`: editorial. CLAUDE.md's ownership table assigns build and test commands to
  CLAUDE.md, and CONTRIBUTING.md and README link there, so there is no ownership gap to fix.

### W1. Fix the permanent session deadlock

- **Status:** done — #147
- **Priority:** blocker
- **Findings:** [C1](#correctness-and-security-findings) (see
  [B1](#b1-permanent-session-deadlock)), [REL-12](#release-engineering-findings)
- **Depends on:** none
- **Problem:** Three paths in an Engine.IO session wait on each other: an in-flight
  websocket frame writer holds `ws.writeLocker`; `setDeadline`
  (`engineio/internal/session/session.go:489-501`) holds the read side of `upgradeLocker`
  while `conn.SetWriteDeadline` waits for `ws.writeLocker`
  (`engineio/transport/websocket/connect.go:101`); `closeMode` (`close.go:46`) requests the
  write side, so `Session.fail` (`close.go:141`), called from `frameWriter.Write` while the
  writer still holds `ws.writeLocker`, cannot take a new read lock. Any websocket client,
  direct or upgraded from polling, that stops reading but keeps sending packets (pings or
  messages) while the server emits triggers it, because every inbound packet calls
  `setDeadline` (`session.go:198`). `OnDisconnect` never runs, goroutines and the TCP
  connection leak, and `engineio.Server.Close` (`engineio/server.go:88-90`) shuts sessions
  down one at a time, so it never reaches later sessions or `dropUnaccepted`.
- **Change:**
  1. `Session.setDeadline` (`engineio/internal/session/session.go`): compute and store the
     deadline, read `s.conn` under `upgradeLocker.RLock`, release the lock, then call
     `SetReadDeadline` and `SetWriteDeadline` on the snapshot, as `nextReader` and
     `nextWriterCompress` already do.
  2. `Session.fail` (`engineio/internal/session/close.go`): stop taking `upgradeLocker`.
     Publish the closed state and the connection at close time in an atomic snapshot (for
     example an `atomic.Pointer` to `{conn transport.Conn}` stored by `closeMode` in the
     same critical section that sets `s.closed = true`), and have `fail` read it. After
     `closed` is set the connection no longer changes: `switchTransport`
     (`upgrade.go:154`) refuses a closed session. `fail` may still reach
     `closeTransport` (`close.go:116-123`), which takes `upgradeLocker.Lock()` briefly to
     set `transportClosed`; that write lock may stay, because after step 1 no holder of the
     read side waits on `ws.writeLocker`. The read-side holders, checked at `0fa1ead`, are
     `Transport`, `URL`, `LocalAddr`, `RemoteAddr`, `RemoteHeader`, `ServeHTTP`,
     `nextReader`, `nextWriterCompress`, `Writable`, `ReadyState`, `Upgraded`, `replaced`
     and `setDeadline`; none of them calls a websocket method that takes `ws.writeLocker`
     except `setDeadline` before this fix. A PR that adds another read-side holder keeps
     this property or makes `transportClosed` atomic instead.
  3. `engineio.Server.Close` (`engineio/server.go:75-94`): call `Shutdown` on every live
     session concurrently (one goroutine per session and a `sync.WaitGroup`), call
     `dropUnaccepted` before waiting, then wait. Keep the documented behavior that repeated
     calls return without waiting and that an Error hook may call `Close`.
  4. `REL-12`: replace the TODO at `engineio/transport/websocket/connect.go:99` with a
     comment saying why the lock is required: gorilla/websocket stores the write deadline
     in a field that its writers read, and supports one concurrent writer, so
     `SetWriteDeadline` must not run concurrently with an open frame writer. Optional, in
     the same PR: store the deadline atomically in the wrapper and apply it inside
     `wrapper.nextWriter` after taking `writeLocker`, then drop the lock from
     `SetWriteDeadline`; if this is done, the comment describes that instead.
- **Tests** (all with `-race`):
  - `engineio/internal/session`, new file `deadlock_test.go`:
    - `TestSetDeadlineDoesNotBlockClose`: a test conn embedding `scriptConn`
      (`lifecycle_test.go:38`) scripts one PING packet; its `SetWriteDeadline` returns nil
      until the test arms it (an `atomic.Bool`), because `New` itself calls `setDeadline`
      (`session.go:91`) inside `newTestSession` (`lifecycle_test.go:128`). Arm it after
      `newTestSession` returns; once armed, `SetWriteDeadline` signals entry and blocks
      until released, and the release is registered with `t.Cleanup` so that a failing run
      on `origin/master` does not leave the `NextReader` goroutine blocked. Call
      `NextReader` in a goroutine, wait for the signal, then call `Abort`. Assert `Abort`
      returns within 1 s; then release. On master `Abort` waits for the release.
    - `TestFailDoesNotWaitForPendingClose`: take `s.upgradeLocker.RLock()` in the test,
      start `Abort` in a goroutine and wait until `s.upgradeLocker.TryRLock()` returns
      false (a writer is pending); whenever it returns true, call
      `s.upgradeLocker.RUnlock()` at once and retry. Then call `s.fail(io.ErrClosedPipe)`
      in a goroutine.
      Assert it returns within 1 s; then `RUnlock`. On master `fail` blocks behind the
      pending writer.
  - `engineio`, new file `server_close_concurrent_test.go` (package `engineio_test`),
    `TestServerCloseShutsDownSessionsConcurrently`: reuse the setup of
    `TestServerCloseFromErrorHookDuringShutdown` (`server_close_hook_test.go`) to put one
    session into a transport-error state whose Error hook blocks on a channel. Dial and
    `Accept` the error session first, as the reused setup does, then dial and `Accept` 16
    healthy websocket sessions and read OPEN on each. Call `server.Close()` in a goroutine; assert that every
    healthy client's next `ReadMessage` fails within 1 s while the hook is still blocked;
    then release the hook and assert `Close` returns. On master the healthy sessions after
    the blocked one in map order stay open; run with `-count=20` on master to show the
    failure.
  - Root package, new file `stalled_reader_test.go` (package `socketio`, reusing
    `recv`/`waitFor` from `lifecycle_test.go:19-21`), `TestStalledReaderIsDisconnected`.
    This turns the B1 probes described in the review (not retained) into a committed test.
    Each subtest starts its own server in this configuration, modelled on the B1 reproductions:
    `NewServer(WithEngineOptions(engineio.WithPingInterval(2*time.Second), engineio.WithPingTimeout(2*time.Second), engineio.WithLogger(<discarding logger>)))`,
    served by `httptest.NewServer` with `go srv.Serve()` and closed in `t.Cleanup`; the
    test keeps the `*Server` for `Server.Close`. Do not use `newTestServer`
    (`lifecycle_errors_test.go:27`): it returns only the `httptest.Server`, and its 700 ms
    heartbeat window (PingInterval 200 ms + PingTimeout 500 ms, which also sets the write
    deadline, `session.go:489-501`) differs from this configuration.
    The server's root `OnConnect` stores the `Conn` and starts a goroutine that waits on a
    per-subtest `start` channel, then emits a 256,000-byte string every 2 ms, at most 400
    times, and returns early when a done channel closed by `OnDisconnect` is closed
    (`Conn.Emit` returns no error). `OnEvent("/", "x", ...)` is registered, and
    `OnError`/`OnDisconnect` report to channels.
    - Direct-websocket subtests: a raw gorilla client reads OPEN and `40`, and the test then
      closes `start`.
    - Upgrade subtests: the client uses text payloads, as `testHandshakeAfterUpgrade`
      (`handshake_test.go:157`, URLs at `:186,191`) does, because without `b64` the
      polling transport answers binary-capable clients with `application/octet-stream`
      framing (`engineio/transport/polling/server.go:38`). It sends the handshake
      `GET /?EIO=3&transport=polling&b64=1`, reads the sid from the OPEN packet, and polls
      `GET /?EIO=3&transport=polling&b64=1&sid=<sid>` until it has read the packet `40`
      (it may arrive in the handshake response or in the next poll). A poll body is a
      sequence of `<length>:<packet>` records (for example `97:0{...}2:40`), so the client
      splits the records and looks for a record whose packet is `40`. It then dials
      websocket at `/?EIO=3&transport=websocket&sid=<sid>`, writes `2probe`, reads
      `3probe`, sends one polling GET that returns the NOOP `1:6`, writes `5`, waits until
      `conn.EngineConn().Upgraded()` is true, and only then closes `start`, as
      `testHandshakeAfterUpgrade` does. Emission must not start during the polling phase:
      there the queue overflows on polling, which is outside the lock cycle (see
      [Review limits](#review-limits)), and the session closes normally. The upgrade
      reproduction delayed emission until after the upgrade.

    After `start` is closed, the client stops reading and, every 100 ms, writes one frame
    with a 1 s write deadline. The write loop runs in its own goroutine and stops on the
    first write error (expected once the fixed server closes the connection; the error is
    ignored) or when a done channel closed in `t.Cleanup` is closed; it never calls
    `t.Fatal`, `t.Error` or `require`, and all assertions stay in the test goroutine.
    Subtests: `ping-websocket` (frame `2`), `message-websocket`
    (frame `42["x"]`), `ping-upgrade` and `message-upgrade` (the same frames after the
    upgrade), and the control `silent-websocket` (sends nothing). In the four sending
    subtests, wait for `OnError` to report `ErrWriteBufferFull` with `recv` (5 s, checked
    with `errors.Is`), then assert that `OnDisconnect` fires within 5 s; in every subtest,
    assert that `OnDisconnect` fires (the silent client is closed by overflow) and that
    `Server.Close` returns within 2 s. On master `ping-websocket`, `message-websocket` and
    `ping-upgrade` get no `OnDisconnect` and `Close` returns only at its 10 s bound. The
    review's probes showed this for those three variants (see the B1 row of the
    [evidence log](#release-relevant-results)), but their exact emit parameters were not
    retained: the size, cadence and count above are this plan's choice, so the PR shows
    the master failure with this test.
    `message-upgrade` is expected to fail on master by the same mechanism (every inbound
    packet calls `setDeadline`) but was not reproduced in the review; if it passes on
    master, say so in the PR and keep it as a regression guard.
- **Benchmarks:** none required; this is a correctness fix. It touches the per-packet
  `setDeadline` path, so if W2 has merged, the Benchmarks workflow comparison of
  `BenchmarkWebsocketEcho/*` on this PR must show benchstat `~` or an improvement; quote
  it. A regression the report flags (above its 20% threshold) needs the owner's
  acceptance in review.
- **Docs:** `engineio.Server.Close` godoc: sessions are shut down concurrently. No user
  document changes; the behavior returns to the documented contract.
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 -run 'TestSetDeadlineDoesNotBlockClose|TestFailDoesNotWaitForPendingClose' ./engineio/internal/session` passes; both fail on master.
  - [ ] `go test -race -count=20 -run TestServerCloseShutsDownSessionsConcurrently ./engineio` passes; fails on master.
  - [ ] `go test -race -count=3 -run TestStalledReaderIsDisconnected .` passes; `ping-websocket`, `message-websocket` and `ping-upgrade` fail on master, and the PR reports the master result of `message-upgrade`.
  - [ ] `make test-stress PKGS='. ./engineio/...'`, `make lint`, `make gates`, `make cover-check` pass.
  - [ ] No TODO remains at `engineio/transport/websocket/connect.go:99`.
- **Definition of done:** standard.
- **Out of scope:** goroutine-leak gating (W12); removing the websocket nag timers (W23).

### W2. Add Socket.IO-level benchmarks

- **Status:** done — #153; adds `BenchmarkEncode`, `BenchmarkEncodePrepared`, `BenchmarkDecode`, `BenchmarkBroadcastWebsocket`, `BenchmarkEmitWebsocket`, `BenchmarkJoinRoom`, `BenchmarkConnectWebsocket`, `BenchmarkMemorySendTo`, `BenchmarkRedisSendTo`, `BenchmarkRedisSendToParallel`, `BenchmarkWebsocketEcho`, `BenchmarkPollingEcho`, `BenchmarkPayloadRoundTrip` and `BenchmarkPollingClientsBroadcast` (26 cases; `BenchmarkPollingClientsBroadcast` runs only with `SOCKETIO_BENCH_POLLING=1` until W20).
- **Priority:** before tag (W3 and W4 need it to show before/after)
- **Findings:** [PERF-10](#performance-findings)
- **Depends on:** none
- **Problem:** The repository's v1 benchmarks cover only Engine.IO codecs and payloads
  (`engineio/parser`, `engineio/parser/internal/payload`). Nothing measures the Socket.IO
  parser, broadcast, emit, adapters, Redis publication or transports, so the CI base-vs-PR
  comparison could not see the #143 encoder regression, and every performance number in
  this review is an ad hoc measurement. The report also shows only time, while the M6
  regression is mostly allocations.
- **Change:** add these benchmarks (all names new; `b.ReportAllocs()` in each; type and
  helper names prefixed `bench` to avoid clashes with existing test helpers):
  - `parser/bench_test.go` (package `parser_test`, public API only):
    - payload types: `benchItem{User string; Text string; Time int64; Tags []string; Meta map[string]string; Online bool}`
      with JSON tags `user`, `text`, `time`, `tags,omitempty`, `meta,omitempty` and
      `online`, and the value
      `{User: "alice", Text: "hello world, this is a chat message", Time: 1700000000, Tags: []string{"a", "b", "c"}, Meta: map[string]string{"k1": "v1", "k2": "v2"}, Online: true}`
      (the original M6 codec payload); `benchItemNoMap`, the same struct and value without
      `Meta` (the payload of the M6 independent re-check, structs with a `[]string` field
      and no map); `benchScalar{N int; S string; B bool; F float64}` (the follow-up
      payload);
    - each packet is encoded as
      `enc.Encode(parser.Header{Type: parser.Event}, []interface{}{"event", value})` (and
      `parser.Prepare(h, []interface{}{"event", value})`). The slice is the single variadic
      argument, because the encoder writes only `args[0]` as the payload
      (`parser/encoder.go:139-141`); passing `"event", value` as two arguments would encode
      only `"event"`. For `structs100`, `structsNoMap100` and `scalars100`, `value` is one
      slice of 100 values (`[]benchItem`, `[]benchItemNoMap`, `[]benchScalar`), as in the
      original harnesses;
    - `BenchmarkEncode` with sub-benchmarks `string` (`"hello"`), `struct` (one
      `benchItem`), `structs100`, `structsNoMap100`, `scalars100`, `binary`
      (`parser.Buffer` of 1 KiB),
      writing to a discarding `FrameWriter`;
    - `BenchmarkEncodePrepared/structs100`: `EncodePrepared` of one packet from
      `parser.Prepare`, the per-recipient broadcast cost;
    - `BenchmarkDecode` with sub-benchmarks `string`, `struct`, `structs100`, `binary`:
      a `FrameReader` that replays frames captured once by encoding the packet
      (`parser.NewEncoder(w).Encode(...)`, or `EncodePrepared` of a `parser.Prepare`
      result) into a capturing `FrameWriter`, because `PreparedPacket` has no exported
      frame accessors; each iteration runs
      `DecodeHeader`, `DecodeArgs` with the matching `reflect.Type`, then `Close`.
  - `broadcast_bench_test.go` (root, package `socketio_test`, public API only):
    `socketio.NewServer(socketio.WithLogger(<discarding logger>))` (it sets the engine
    logger too), served by
    `httptest.NewServer`, `go srv.Serve()`; raw gorilla clients that read OPEN and `40` and
    then signal each received frame on a channel:
    - `BenchmarkBroadcastWebsocket` with sub-benchmarks `string-10` and `structs100-10`:
      10 clients; per iteration `srv.BroadcastToNamespace("/", "data", payload)` and wait
      for 10 frames;
    - `BenchmarkEmitWebsocket/string`: one client; per iteration `Conn.Emit` without ACK
      and wait for the frame;
    - `BenchmarkJoinRoom/sockets-200`: 200 connected clients; per iteration
      `srv.JoinRoom("/", "room", conns[i%200])`;
    - `BenchmarkConnectWebsocket`: per iteration dial, read OPEN and `40`, close, wait for
      `OnDisconnect`.
  - `adapter/memory_bench_test.go` (package `adapter_test`): 1,000 no-op `adapter.Socket`
    values; every socket joins its own ID room, as the server does (`socket.go:398`).
    `BenchmarkMemorySendTo` with sub-benchmarks `one-room` (every socket also in room `r`,
    `Options{Rooms: []string{"r"}}`), `all` (every socket also in room `r`, `Options{}`, so
    each socket is reached through two rooms and must be delivered once), `three-rooms`
    (`Options{Rooms: []string{"a", "b", "c"}}` with overlapping rooms of 500 sockets: `a`
    holds sockets 0–499, `b` 250–749, `c` 500–999).
  - `adapter/redis/publish_bench_test.go` (package `redis`, miniredis):
    `BenchmarkRedisSendTo` (sequential `SendTo(adapter.Options{}, "e", "x")`) and
    `BenchmarkRedisSendToParallel` (`b.SetParallelism(16)`, `b.RunParallel`). Generalize
    `newTestRedisBroadcast` and `waitRedisSubscribers` (`adapter_test.go:52-69`) to
    `testing.TB`. With `SOCKETIO_BENCH_METRICS=1`, report `conns/op` from
    `TotalConnectionCount()`.
  - `engineio/transport_bench_test.go` (package `engineio_test`, public API):
    `BenchmarkWebsocketEcho` with sub-benchmarks `small` (`4hello`) and `4KiB`: the server
    accepts the session, reads each message with `NextReader` and writes it back with
    `NextWriter`; and `BenchmarkPollingEcho`: per iteration one POST of a MESSAGE and one
    GET that returns the echo.
  - `engineio/transport/polling/payload_bench_test.go` (package `polling`):
    `BenchmarkPayloadRoundTrip`: `newPayload(false)` (text mode), read and write deadlines
    45 s ahead. `FeedIn` is a synchronous hand-off over unbuffered channels
    (`payload.go:55-71,110-160`): it blocks until a reader takes the payload and releases
    it, so it never runs in the benchmark goroutine, as in
    `payload_feed_close_test.go:15`. Per iteration, start
    `go func() { done <- p.FeedIn(strings.NewReader("6:4hello"), false) }()`; in the
    benchmark goroutine call `NextReader`, read to EOF and `Close`; then receive from
    `done` and fail on a non-nil error.
  - `client/polling_bench_test.go` (package `client_test`):
    `BenchmarkPollingClientsBroadcast/clients-10`: 10 Go clients restricted to the polling
    transport (`client.WithTransports(polling.Default)`); per iteration one server
    broadcast and wait until all 10 handlers ran. With `SOCKETIO_BENCH_METRICS=1`, report
    `dials/op` counted through the httptest server's `Config.ConnState` (`http.StateNew`).
  - `.github/benchmarks/benchmark-report.json`: set `report.metrics` to
    `["time", "bytes", "allocations"]` and `report.group_by` to `["package", "metric"]`.
    The pinned report action (`sshaplygin/benchmark-report/report@f71129f`, its
    `docs/configuration.md`) rejects more than one selected metric unless `metric` is in
    `group_by` or `columns`, with the error "multiple selected metrics require metric
    grouping or column". Before pushing, check the file against that document; if the
    report step still fails, revert this file and state in the PR that `B/op` and
    `allocs/op` are compared manually with `benchstat` (How to use).
  - `.github/workflows/benchmarks.yml`: raise the `compare` job's `timeout-minutes` from 20
    to 45, because this PR changes `.github/benchmarks/` and therefore benchmarks every
    package on both sides, and every later PR runs more benchmarks than before.
- **Tests:** none beyond the benchmarks. Each benchmark must pass with
  `go test -run '^$' -bench <name> -benchtime=1x` (smoke run) and must not leave goroutines
  or listeners behind (close clients and servers in `b.Cleanup`).
  `BenchmarkPollingClientsBroadcast` and `BenchmarkConnectWebsocket` open TCP connections
  per iteration, and until W20 the polling clients share `http.DefaultTransport`, which
  exhausted ephemeral ports on darwin in the PERF-5 measurement. Both create their server
  and clients per run and call `polling.Default.Client.CloseIdleConnections()` in
  `b.Cleanup`. Run
  `go test -run '^$' -bench 'PollingClientsBroadcast|ConnectWebsocket' -benchmem -count=10 ./client .`
  on darwin and record in the PR that it completes without dial errors
  (`can't assign requested address`); the PR's Benchmarks workflow run is the Linux check.
  The workflow runs with `pipefail`, so a dial error fails the comparison: if either run
  fails, skip the polling benchmark unless `SOCKETIO_BENCH_POLLING=1` is set, say so in
  the PR and in the benchmark comment, and W20 removes the skip.
- **Benchmarks:** this item adds them. Expected direction: none; on this PR the base side
  lacks the new benchmarks and the report lists them as missing on base.
- **Docs:** CLAUDE.md "CI" paragraph on benchmarks: the report shows time, bytes and
  allocations, grouped by metric (only if the report configuration change was kept);
  custom metrics require `SOCKETIO_BENCH_METRICS=1` and are not reported by CI. Apart from
  W2's Status line and the `Fixed by` note on PERF-10 (Marking progress), this review is
  not edited: the Captured by column of
  [Performance measurements](#performance-measurements) already names each benchmark.
- **Acceptance criteria:**
  - [ ] `go test -run '^$' -bench . -benchmem -count=1 ./parser ./adapter/... ./engineio/... ./client .` completes; total time is recorded in the PR.
  - [ ] `go vet ./...` and `make lint` pass.
  - [ ] The Benchmarks workflow on this PR completes within its timeout and its report shows `B/op` and `allocs/op` rows, grouped by metric; or, if the report configuration had to be reverted, the PR says so and quotes a manual `benchstat` comparison of `B/op` and `allocs/op`.
  - [ ] Copying `parser/bench_test.go` and `broadcast_bench_test.go` onto a `3dd094d` worktree (How to use) compiles and runs.
- **Definition of done:** standard; the Status line lists the committed benchmark names.
- **Out of scope:** any change to measured code; failing CI on regressions
  (`fail_on_regression` stays false).

### W3. Restore encoder performance

- **Status:** done — #169; BenchmarkEncode/structs100: 3619 → 411 allocs/op, 349.3 → 79.4 µs (CI, base 38ba465, linux/amd64); against 3dd094d: 1012 → 510 allocs/op, 128.0 → 42.1 µs (−67.1%, p=0.000, darwin/arm64)
- **Priority:** before tag (or deferred in writing with the cost in the release notes)
- **Findings:** [PERF-1](#performance-findings) (see
  [M6](#m6-json-encoder-from-143-regresses-encode-cost)), [T1](#tests-findings)
- **Depends on:** W2
- **Problem:** The JSON encoder introduced in #143 calls `json.Marshal` for every leaf value
  and map key and sorts the entries (`parser/json_encode.go:109-182`), and allocates a
  cycle-detection map for every packet (`parser/encoder.go:138`). Ad hoc measurements
  (no committed benchmark yet) give 2,518 instead of 1,012 allocations for 100 structs and
  +48% to +57% time for an end-to-end broadcast to 10 websocket clients compared with
  `3dd094d`; see W2 for the committed benchmarks. The previous master was already about 2×
  slower than `v1.4.2` (historical ad hoc measurement; not reproducible by a committed
  benchmark; see [Performance measurements](#performance-measurements)). `isJSONEmpty`
  (`json_encode.go:252`), which implements `omitempty`, has no test coverage.
- **Change:**
  1. `T1` first, in the same PR: a table-driven differential test in
     `parser/json_compatibility_test.go` that encodes `omitempty` structs covering every
     kind `isJSONEmpty` handles (bool, numbers, strings, slices, maps, arrays, pointers,
     interfaces, nested structs, fields holding `parser.Buffer`) and compares the JSON
     payload with `json.Marshal` of a mirror struct. The mirror has the same fields and
     tags; each `parser.Buffer` field becomes a `json.RawMessage` holding the attachment
     placeholder `{"_placeholder":true,"num":<N>}` (N is the attachment index in encode
     order, `parser/json_encode.go:34-38`), and each `*parser.Buffer` field becomes a
     `*json.RawMessage` that is nil when the original pointer is nil. A `Buffer` field is
     a struct, so `omitempty` never omits it, as `encoding/json` never omits a struct; a
     nil `*Buffer` is omitted. The test also asserts the number of attachments.
  2. `parser/json_encode.go`: a per-`reflect.Type` cache (`sync.Map`) of whether a type is
     plain: its subtree contains no `parser.Buffer` or `*parser.Buffer`, no interface, no
     pointer, no type with a custom JSON or text marshaling method (use the existing
     `implementsJSONMethod`), and is not recursive. Encode a plain value with a single
     `json.Marshal` call and append the result.
  3. Write strings, numbers and booleans with append-style helpers (`strconv.Append*`, and
     string escaping that matches `encoding/json`, including HTML escaping).
  4. `parser/encoder.go:138`: create `payloadEncoder.active` lazily, on the first pointer,
     map or slice visit that needs cycle detection.
  5. Write directly into the final buffer with header space reserved, avoiding the copy
     into `data` (`Prepare`, `parser/encoder.go:45-55`).
  Visibility and snapshot semantics stay unchanged: `parser/json_visibility_test.go`,
  `parser/prepared_test.go`, `parser/json_omitzero_test.go` and
  `parser/json_methods_v2_test.go` must pass unchanged.
- **Tests:** the T1 differential test above, plus `TestEncodePlainTypesMatchEncodingJSON`
  in `parser/json_compatibility_test.go`: nested slices, maps and arrays of scalars,
  embedded structs, unexported fields, and the tags `-`, `,string` and `omitempty`,
  compared with `json.Marshal`. Run the parser tests also with `GOEXPERIMENT=jsonv2`
  (Go 1.25 or later) because `json_mode_v2*.go` and `json_methods_v2.go` change behavior
  under it.
- **Benchmarks:** `BenchmarkEncode/*`, `BenchmarkEncodePrepared/structs100` (package
  `parser`) and `BenchmarkBroadcastWebsocket/*` (root) from W2. Base revisions: the PR base
  (CI comparison) and `3dd094d`, the previous master before #143 (manual comparison in How
  to use). Expected direction: allocations and time decrease against the PR base; against
  `3dd094d`, `allocs/op` of `BenchmarkEncode/structs100` at or below the `3dd094d` value and
  `sec/op` not significantly worse (benchstat `~` or lower). If that target is not reached,
  the PR reports the remaining difference with numbers and the owner decides whether to
  merge and state the residual cost in the release notes.
- **Docs:** none (godoc unchanged). W3's PR records the before/after in its Status line;
  the M6 table stays as recorded.
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 ./parser/...` passes, and `GOEXPERIMENT=jsonv2 go test -count=1 ./parser/...` passes on Go 1.25 or later.
  - [ ] `go test -coverpkg=./parser -coverprofile=c.out ./parser && go tool cover -func=c.out | grep isJSONEmpty` reports more than 0%.
  - [ ] The PR quotes the CI benchmark comparison and the manual `benchstat` against `3dd094d` for the benchmarks above.
  - [ ] `make lint`, `make test-race`, `make gates`, `make cover-check` pass.
- **Definition of done:** standard, with the benchmark result in the Status line.
- **Out of scope:** decoding (W25); prepare-once broadcast (W21); encoder fuzzing (W35);
  the jsonv2 CI job (W37).

### W4. Bound Redis broadcast publication

- **Status:** done — #151; BenchmarkRedisSendTo: 56796 → 58122 ns/op, BenchmarkRedisSendToParallel: 18227 → 18561 ns/op, allocs/op unchanged
- **Priority:** before tag
- **Findings:** [PERF-3](#performance-findings) (see [M5](#m5-redis-publish-has-no-io-deadline))
- **Depends on:** W2
- **Problem:** Broadcast `PUBLISH` (`adapter/redis/adapter.go:195` → `codec.go:191-198`)
  runs `c.Do` on a pooled connection whose dial options set no read or write timeout
  (`adapter.go:70-104`). With Redis paused, `SendTo` stays blocked until Redis resumes,
  even after adapter `Close` returns (reproductions in M5 and its Follow-up row).
  `WithRequestsTimeout` bounds only cluster requests (`boundedDo`,
  `requests.go:94,120,470`), although `options.go:123` and `cluster.go:23` say it bounds
  Redis commands and all calls.
- **Change:**
  1. `adapter/redis/codec.go` `do`: run the command with
     `redis.DoWithTimeout(c, timeout, cmd, args...)`, where `timeout` is the adapter's
     requests timeout. Do not use `redis.DoContext`: it is missing from redigo
     v2.0.0+incompatible, which `adapter.go:78-81` supports and `_examples/gf` selects.
  2. `adapter/redis/adapter.go`: give the publishing pool (`pub`, `adapter.go:100`) its own
     dial that adds `redis.DialWriteTimeout(timeout)`; the shared Pub/Sub subscriber and
     `boundedDo` keep the existing `dial` without read or write timeouts. Compute `timeout`
     from `opts.RequestsTimeout`, falling back to `redisRequestTimeout`, because
     `initCluster` runs after the pool's first `Get`.
  3. Godoc: `WithRequestsTimeout` (`options.go:123`) bounds each cluster request and each
     broadcast publication; keep `cluster.go:23` true.
- **Tests:** new file `adapter/redis/publish_deadline_test.go` (package `redis`), turning
  the M5 probe (not retained) into committed tests with `-race`:
  - helper `stallProxy(t, addr) (proxyAddr string, stall func())`: a TCP proxy to
    miniredis that forwards both directions until `stall` is called, after which it keeps
    every connection open but stops forwarding;
  - `TestRedisPublishBoundedByRequestsTimeout`: adapter via `newBroadcast` on the proxy
    with `RequestsTimeout: 100 * time.Millisecond`; a first `SendTo` succeeds; after
    `stall()`, `SendTo` returns a non-nil error within 1 s. On master it does not return
    (the test fails at its 2 s guard; the cleanup unstalls the proxy so the goroutine ends);
  - `TestRedisSubscriberIdleLongerThanRequestsTimeout`: two adapters on one miniredis with
    `RequestsTimeout: 50 * time.Millisecond`; after 300 ms idle, a broadcast from the first
    reaches a local socket of the second, so no read timeout reached the subscriber.
- **Benchmarks:** `BenchmarkRedisSendTo` and `BenchmarkRedisSendToParallel` (package
  `adapter/redis`, W2) against the PR base. Expected direction: unchanged or slightly
  slower (one deadline per command); a slowdown above the report's 20% threshold needs the
  owner's acceptance in review.
- **Docs:** godoc above; V1.md "Adapters and Redis": broadcast publication is bounded by
  `WithRequestsTimeout` and `SendTo` returns the timeout error.
- **Acceptance criteria:**
  - [ ] `go test -race -count=3 -run 'TestRedisPublishBoundedByRequestsTimeout|TestRedisSubscriberIdleLongerThanRequestsTimeout' ./adapter/redis` passes; the first fails on master.
  - [ ] `make examples` passes (builds `_examples/gf` with redigo v2.0.0+incompatible).
  - [ ] `make test-stress PKGS=./adapter/...`, `make lint`, `make cover-check` pass.
  - [ ] The PR quotes the benchmark comparison.
- **Definition of done:** standard.
- **Out of scope:** pool sizing (W19); an asynchronous publish queue.

### W5. Report Redis broadcast failures

- **Status:** done — #152
- **Priority:** before tag
- **Findings:** [ARCH-1](#architecture-findings), [CL-2](#clarity-findings) (see
  [M4](#m4-redis-send-and-sendall-drop-broadcasts-silently))
- **Depends on:** W4 (same file)
- **Problem:** `Send` and `SendAll` (`adapter/redis/adapter.go:154-161`) discard the error
  of `SendTo`. A trailing callback is rejected before any delivery, so nothing is sent, not
  even locally; a publication failure follows a successful local delivery and is lost
  without a log. `BroadcastToRoom`/`BroadcastToNamespace` return true whenever the namespace
  is registered and its adapter was constructed, also after `Server.Close`. The logger
  contract says WARN marks a failure no caller receives (`logger/logger.go:31-32`), and
  godoc promises per-socket callbacks (`server.go:275-290`, `errors.go:21-22`,
  `adapter/adapter.go:61-62`).
- **Change:**
  1. `adapter/redis/adapter.go` `Send`/`SendAll`: when `SendTo` returns an error, log
     `logger.Log.Warn("socketio: redis broadcast failed", "nsp", bc.nsp, "err", err)`.
  2. Godoc: `Server.BroadcastToRoom` and `BroadcastToNamespace` (`server.go:275-290`): the
     result reports only that the namespace is registered and its adapter was constructed,
     not delivery and not that the adapter is open; a trailing callback is registered per
     socket by the in-memory adapter, while the Redis adapter rejects it, sends nothing and
     logs a WARN record. `ErrBroadcastAck` (`errors.go:21-22`) and the `Send`/`SendAll`
     comments in `adapter/adapter.go:61-62` and `adapter/redis/adapter.go:153,158` say the
     same.
- **Tests:** root `redis_integration_test.go`, with `captureLogs` (`log_test.go:37`) and a
  server peer from `start(t, 'S', hooks{setup: ...})` (`backpressure_test.go:250`) whose
  setup calls `useRedis` (`redis_integration_test.go:22`); the root socket joins room `r`:
  - `TestRedisBroadcastCallbackLogsWarn`: `BroadcastToRoom("/", "r", "e", "x", func() {})`
    returns true, no frame arrives on the peer's `fc.out` within 200 ms, and one WARN record
    `socketio: redis broadcast failed` with keys `nsp` and `err` is logged;
  - `TestRedisBroadcastPublishFailureLogsWarn`: after the miniredis server is closed,
    `BroadcastToNamespace("/", "e", "x")` still delivers to the local socket and logs the
    same WARN record.
  Both fail on master (no record). `TestNoBadKeyAttrs` must still pass.
- **Benchmarks:** none (error path only).
- **Docs:** godoc above; V1.md "Adapters and Redis" (`docs/V1.md:120-128`): rejected
  callbacks and publication failures of `BroadcastTo*` are logged at WARN.
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 -run 'TestRedisBroadcast(Callback|PublishFailure)LogsWarn|TestNoBadKeyAttrs' .` passes; the first two fail on master.
  - [ ] `make lint`, `make test-race`, `make cover-check` pass.
- **Definition of done:** standard.
- **Out of scope:** changing the boolean result or the `Adapter` interface (D4); renaming
  the other Redis log keys (W28).

### W6. Document the CORS default

- **Status:** done — #148
- **Priority:** before tag
- **Findings:** [API-3](#api-compatibility-findings), [C5](#correctness-and-security-findings),
  [CL-9](#clarity-findings), [interop-9](#interoperability-findings) (see
  [M2](#m2-permissive-cors-default-is-not-in-the-user-docs))
- **Depends on:** none
- **Problem:** By default `engineio/options.go:174-181` accepts every origin, and
  `getTransport` (`:121-151`) applies that to both default transports: polling reflects the
  request `Origin` with `Access-Control-Allow-Credentials: true`, and websocket skips the
  origin check. This matches socket.io 2.x but is documented only in godoc; README, V1.md
  and PROTOCOL.md never mention it, so cookie-authenticated applications are exposed to
  cross-site WebSocket hijacking unknowingly. `_examples/default-http/main.go:19-43` adds an
  allow-all `CheckOrigin`, suggesting CORS is off by default. PROTOCOL.md also does not say
  that an unknown Socket.IO packet type closes the connection.
- **Change:**
  - `docs/PROTOCOL.md`, Engine.IO section, new "CORS" bullet: the default; preflight
    handling; `engineio.WithCORS(false)` (polling responses, including error responses,
    carry no `Access-Control-Allow-Origin`/`-Credentials`, an `OPTIONS` request on an open
    session still answers 200 with `Access-Control-Allow-Headers: Content-Type`,
    cross-origin polling requests are still served, a foreign-origin websocket gets 403);
    and transport `CheckOrigin` for an allow-list.
  - `docs/PROTOCOL.md`, Socket.IO section: an unknown or invalid packet type closes the
    connection (interop-9).
  - `docs/PROTOCOL.md`, Engine.IO section, related to W15: websocket ignores `b64`, so a
    `forceBase64` client is disconnected when it sends binary data (state it as a
    limitation; W15 removes it).
  - `docs/V1.md`: a short security note linking the PROTOCOL.md bullet.
  - `engineio.WithCORS` godoc: one sentence on the security effect of the default.
  - `_examples/default-http/main.go`: remove `allowOriginFunc` and the explicit transport
    list, and add a comment that the default accepts every origin and how to restrict it.
- **Tests:** none new; the existing `TestCORSDefault` and `TestCORSDisabled`
  (`engineio/cors_test.go`) pin the documented behavior. Examples are exercised by CI.
- **Benchmarks:** none.
- **Docs:** listed under Change (PROTOCOL.md owns wire behavior; V1.md usage).
- **Acceptance criteria:**
  - [ ] `grep -n "WithCORS(false)" docs/PROTOCOL.md docs/V1.md` finds the new text.
  - [ ] `make examples` passes; `make examples-node` passes with both pinned clients (CI conformance job).
  - [ ] `go test -race -count=1 -run 'TestCORS' ./engineio` passes.
- **Definition of done:** standard.
- **Out of scope:** changing the default (decided: matches socket.io 2.x); the migration
  table entry (W8 links here).

### W7. Document the Redis wire break and upgrade procedure

- **Status:** done — #149
- **Priority:** before tag
- **Findings:** [API-4](#api-compatibility-findings) (see
  [M3](#m3-redis-adapter-is-wire-incompatible-with-upstream-go-peers))
- **Depends on:** none
- **Problem:** The fork's Redis adapter uses different channel names and MessagePack instead
  of upstream's JSON (`adapter/redis/adapter.go:121-124`, `codec.go:28-33`). In a mixed
  fleet, a fork namespace-wide broadcast, or a room broadcast whose room name or last
  `#`-separated segment equals the namespace name, stops the dispatcher of every upstream
  `v1.7.0` peer in that namespace; those peers then receive no broadcasts and answer no
  cluster requests, and their `RoomLen`/`Rooms` block without a timeout. Root traffic is
  also incompatible: fork root channels (`socket.io#/#…`) do not match the upstream root
  pattern (`socket.io##*`), so upstream peers never receive them, and the fork discards the
  upstream root messages it cannot decode. Root-namespace broadcasts are therefore lost in
  both directions, but they do not stop dispatchers. The deleted
  changelog carried the warning; no current
  document does.
- **Change:** `docs/PROTOCOL.md`, "Redis cluster interoperability": state that upstream Go
  Redis peers are wire-incompatible (channel and codec table from M3), the mixed-fleet
  effect above, and the upgrade procedure: give the new fleet a distinct `WithPrefix`, or
  upgrade every instance at once.
- **Tests:** none (documentation).
- **Benchmarks:** none.
- **Docs:** `docs/PROTOCOL.md` (owner of wire behavior); W8 links to it.
- **Acceptance criteria:**
  - [ ] `grep -n "WithPrefix" docs/PROTOCOL.md` finds the upgrade procedure.
  - [ ] The text matches M3 and its Follow-up row: fork namespace-wide broadcasts, and room broadcasts whose room name or last `#`-separated segment equals the namespace, stop upstream dispatchers in that namespace; root-namespace traffic is not delivered between fork and upstream peers in either direction, but does not stop dispatchers.
- **Definition of done:** standard.
- **Out of scope:** any compatibility codec for upstream peers.

### W8. Write the migration guide

- **Status:** done — #191
- **Priority:** before tag
- **Findings:** [API-1](#api-compatibility-findings), [CL-4](#clarity-findings),
  [REL-1](#release-engineering-findings), [API-5](#api-compatibility-findings),
  [API-6](#api-compatibility-findings), [API-7](#api-compatibility-findings),
  [API-8](#api-compatibility-findings), [API-13](#api-compatibility-findings) (see
  [M1](#m1-no-migration-guide-or-release-notes))
- **Depends on:** W5, W6, W7 (links); D1 (version wording of the release notes); D2–D6
  decided and any resulting API PR merged, because the identifier table and the apidiff
  lists must describe the tagged API
- **Problem:** Every adopter arrives from upstream, because the fork's own tags declare the
  upstream module path. `b2c1cd2` deleted the changelog and `docs/V1_MIGRATION.md`; no
  document lists the API changes (apidiff: root 24 incompatible changes against `v1.4.2`
  and 20 against upstream `v1.7.0`; `parser` 4 and 3; `engineio` 8, or 17 including `transport`,
  `transport/polling` and `transport/websocket`, with five public packages removed), the
  changed defaults, or calls that compile but mean something different. The non-root
  `Conn.ID` format `<namespace>#<engine ID>` is not in the public godoc of either role.
- **Change:**
  - `docs/V1.md`, new section "Migrating from googollee/go-socket.io":
    - import path rewrite (a `replace` drop-in does not work);
    - old-to-new identifier table, for example `NewServer(*engineio.Options)` →
      `NewServer(...Option)` and `Server.Adapter(*RedisAdapterOptions)` →
      `WithAdapter(redis.New(...))`; removed `engineio` packages `frame`, `packet`,
      `payload`, `session`, `transport/utils`; removed `Options`, `Dialer`, `Opener`,
      `Pauser`; methods added to `engineio.Conn`. Use the deleted
      `docs/V1_MIGRATION.md` "Engine.IO codecs and polling" table (`git show b2c1cd2^:docs/V1_MIGRATION.md`)
      as a starting point, with intermediate-master names replaced by upstream `v1.7.0`
      names; regenerate the lists with `apidiff` against upstream `v1.7.0` and `v1.4.2`
      (procedure below);
    - changed-defaults table with the old default, the new default and the opt-out option
      for every row of the [M1](#m1-no-migration-guide-or-release-notes) changed-defaults
      table (CORS links W6). M1 owns the values; verify each against the sources before
      quoting it. Obtain the old sources with
      `GOFLAGS=-mod=mod go mod download -json github.com/googollee/go-socket.io@v1.7.0 github.com/googollee/go-socket.io@v1.4.2 github.com/googollee/go-engine.io@v1.4.1`
      and read the printed `Dir` paths: ping defaults in upstream `v1.7.0`
      `engineio/server_options.go:39-51` and go-engine.io `v1.4.1` `server.go:45-56`;
      disconnect reasons in upstream `v1.7.0` `connection.go:58-68` and
      `namespace_handler.go:75-78`. New values come from the `engineio/options.go` godoc
      and, for disconnect reasons, `close.go:184-211` and
      `engineio/internal/session/contracts.go:53-61`;
    - "compiles but means something different": the non-root `Conn.ID` format
      (`<namespace>#<engine ID>`), `BroadcastToRoom(room, event, s)` with a string `s`
      (now namespace, room, event), and parser `Encode(h, args)` with nil `args`, which
      now writes a `null` payload (`1/a` becomes `1/a,null`);
    - Redis: link the W7 upgrade procedure;
    - Go 1.22 floor.
  - apidiff procedure: install `golang.org/x/exp/cmd/apidiff` at a pinned pseudo-version
    and name it in the PR (the review used an x/exp build of 2026-10-14; its exact
    pseudo-version was not recorded, so counts from another build may differ and must then
    be explained). Compare packages, not modules, because the module paths differ. This
    repository has no `v1.7.0` tag, so take the old trees from upstream in a scratch
    directory outside this repository:
    `git clone https://github.com/googollee/go-socket.io upstream`, then
    `git -C upstream worktree add ../old-v1.7.0 v1.7.0` and
    `git -C upstream worktree add ../old-v1.4.2 v1.4.2` (writable trees with their own
    `go.mod` and `go.sum`; the read-only module cache is not used here). In each old tree,
    write the export data of each package (`apidiff -w old.<pkg>.x <old import path>`); in
    the release branch, write the new export data the same way; then run
    `apidiff -incompatible old.<pkg>.x new.<pkg>.x`. Packages: root, `engineio`,
    `engineio/transport`, `engineio/transport/polling`, `engineio/transport/websocket` and
    `parser`. apidiff also reports changes caused only by the module path (for example
    `(*Server).Accept: changed from func() (Conn, error) to func() (Conn, error)`); the
    counts below include them. Quote the commands and counts in the PR; they must match
    M1 (root 24 vs `v1.4.2` and 20 vs `v1.7.0`; `parser` 4 vs `v1.4.2` and 3 vs `v1.7.0`;
    `engineio` 8 vs `v1.7.0`, 17 with `transport` (5), `transport/polling` (2) and
    `transport/websocket` (2)), or the PR explains the difference, for example API changes
    from D2–D6.
  - `Conn.ID` godoc in `types.go:71-72` and `client/types.go:28-29`: the root namespace
    returns the engine ID, other namespaces `<namespace>#<engine ID>`.
  - `CONTRIBUTING.md`: keep the no-changelog policy (the paragraph that starts "V1 has no
    standalone changelog"); add to "Before tagging v1" a step to publish GitHub release
    notes containing the migration summary, the version-numbering statement from D1 and
    every major deferred in writing.
- **Tests:** none new; `internal/parity` gates must pass after the godoc change
  (`TestPublicRoleDocumentation`).
- **Benchmarks:** none.
- **Docs:** `docs/V1.md` (owner of v1 usage), godoc, `CONTRIBUTING.md` (release procedure).
- **Acceptance criteria:**
  - [ ] `docs/V1.md` has the section with the six parts above (import path, identifier table, changed-defaults table with old and new values, compiles-but-changes-meaning list, Redis upgrade link, Go 1.22 floor), and its links resolve on GitHub.
  - [ ] `go doc . Conn.ID` and `go doc ./client Conn.ID` show the format.
  - [ ] `make gates` passes.
- **Definition of done:** standard.
- **Out of scope:** a standalone changelog (against the documented policy); compatibility
  stubs.

### W9. Add a lifecycle sketch and runnable examples

- **Status:** done — #150
- **Priority:** before tag
- **Findings:** [CL-1](#clarity-findings) (see
  [M7](#m7-no-inline-quick-start-docs-do-not-state-that-serve-must-run))
- **Depends on:** none
- **Problem:** README links runnable server and Go client examples (by decision in
  `b557f2a`), but no inline code in README, V1.md or godoc mounts the handler or calls
  `Serve`, `Close` or `Connect`, and there are no `Example` functions. A user who forgets
  `go server.Serve()` gets no error: `Connect` returns nil, but `OnConnect` never fires and
  acknowledgements never arrive.
- **Change:**
  - New root `doc.go`: package comment with a short server lifecycle sketch: `NewServer`,
    `OnConnect`/`OnEvent`, `http.Handle("/socket.io/", server)`, `go server.Serve()`,
    `defer server.Close()`.
  - New `client/doc.go`: package comment with `client.New`, `Connect`, `Emit` with an ACK
    callback, `Close`.
  - New `example_test.go` (package `socketio_test`): `ExampleNewServer` (compile-only, no
    `// Output:`, no fixed port).
  - New `client/example_test.go` (package `client_test`): `ExampleNew` that starts a server
    on `httptest.NewServer`, connects a client, emits with an ACK and prints the ACK value,
    with `// Output:`.
  - `Server` type and `Serve` godoc (`server.go:184`): connections are not dispatched, and
    no error is reported, while `Serve` is not running.
  - README keeps linking to the runnable examples.
- **Tests:** `ExampleNew` runs under `go test` and checks its output; `ExampleNewServer`
  is compiled only.
- **Benchmarks:** none.
- **Docs:** root and client package docs, godoc.
- **Acceptance criteria:**
  - [ ] `go test -count=1 -run 'Example' . ./client` passes.
  - [ ] `go doc .` and `go doc ./client` print the sketches.
  - [ ] `make lint`, `make gates`, `make cover-check` pass.
- **Definition of done:** standard.
- **Out of scope:** package docs of the other packages (W27).

### W10. Prepare the release commit

- **Status:** done — #193
- **Priority:** before tag
- **Findings:** [API-10](#api-compatibility-findings), [REL-6](#release-engineering-findings),
  [REL-2](#release-engineering-findings), [REL-3](#release-engineering-findings),
  [REL-11](#release-engineering-findings)
- **Depends on:** W1 merged (B1 cannot be deferred); W2 merged (its committed benchmarks
  are the only source of a performance figure in the release notes, including the M6 cost
  if W3 is deferred); W3–W9 merged or, for the items of M1–M7, deferred in writing (Status
  `deferred — <reason> (<date>)`, see [How to use this plan](#how-to-use-this-plan));
  D1–D6 decided and any resulting before-tag change merged
- **Problem:** README advertises `@v1.5.0` before the tag exists and lacks the Engine.IO
  pkg.go.dev link that CONTRIBUTING step 4 requires. A previously deleted `v1.5.0` is still
  in the maintainer's local module cache. The repository-local git identity is a
  placeholder (`Probe <probe@example.invalid>`) that already appears in `Co-authored-by`
  trailers on `ea920ae` and `3dd094d` and would become the tagger. 130 remote branches
  remain besides master, including `release/v1.5.0` and the `v1-h/release-notes` draft.
- **Change** (release PR, prepared by the implementer):
  - README: the version chosen in D1 in the install command and the root pkg.go.dev links,
    and a new Engine.IO link `https://pkg.go.dev/github.com/sshaplygin/go-socket.io@<version>/engineio`.
  - `go.mod`, only if D1 chose R1: the retraction, for example
    `retract [v0.9.1, v1.4.2] // upstream tags; go.mod declares github.com/googollee/go-socket.io`.
  - The release notes text in the PR description (W8 summary, D1 statement, deferrals).
    A performance figure in the release notes comes only from a committed benchmark (W2),
    with the benchmark name, base revision and command; for a deferred W3, the M6 cost as
    described in [How to use this plan](#how-to-use-this-plan).
  Owner-only steps, listed in the PR as a checklist:
  - REL-3: replace or remove the placeholder `user.name`/`user.email` in the release
    checkout, and check the squash-merge body for `Co-authored-by` lines before
    confirming. History on master is not rewritten.
  - CONTRIBUTING "Before tagging v1": the full CI matrix; `make test-stress` locally, since
    the stress job does not run on push until W39; `make conformance` and
    `make examples-node` with `socket.io-client` 1.7.4 and 2.5.0.
  - REL-2: before tagging, verify consumption of the merged release commit with a fresh
    cache, in a scratch module:
    `GOMODCACHE=$(mktemp -d) GOFLAGS=-mod=mod go get github.com/sshaplygin/go-socket.io@<release commit SHA>`
    (CONTRIBUTING.md step 4); after tagging, repeat it with `@<version>`.
  - REL-11 (optional; not required for a valid tag): delete merged or superseded remote
    branches, `release/v1.5.0` in particular, after reusing anything useful from
    `v1-h/release-notes` in the release notes; `git fetch --prune` afterwards.
  Progress in this document (Marking progress): the PR sets W10's Status to
  `in progress — #<PR>`, appends `; in progress — #<PR>` to D1's Status line, and appends
  `Fixed by #<PR> (W10).` to API-10 and REL-6 and `Fixed by #<PR> (D1).` to API-2, REL-7 and
  CL-10. It adds no note to REL-2, REL-3 or REL-11; the owner appends
  `Fixed by #<PR> (W10).` to each of them after doing its step.
- **Tests:** none.
- **Benchmarks:** none.
- **Docs:** README (install and links), `go.mod`.
- **Acceptance criteria:**
  - [ ] `go mod tidy && git diff --exit-code` and `go build ./...` pass (with the retraction, if D1 chose R1).
  - [ ] README has the root and Engine.IO pkg.go.dev links with the chosen version.
  - [ ] Before the tag (owner): the fresh-cache `go get` at the release commit SHA succeeds.
  - [ ] After the tag (owner): the fresh-cache `go get` at the version succeeds; if D1 chose R1, `go list -m -versions github.com/sshaplygin/go-socket.io` no longer lists the retracted versions.
- **Definition of done:** standard; in addition, D1's Status line carries
  `; done — #<PR>` and API-2, REL-7 and CL-10 carry `Fixed by #<PR> (D1).`; the owner
  creates the tag on the verified release commit and appends the `Fixed by` notes of the
  owner-only steps done (REL-2, REL-3, and REL-11 if done).
- **Out of scope:** creating the tag or the GitHub release (owner only).

### D1. Version number and inherited tags

- **Status:** decided — option A + R1 (2026-10-24); done — #193
- **Priority:** owner decision
- **Findings:** [API-2](#api-compatibility-findings), [REL-7](#release-engineering-findings),
  [CL-10](#clarity-findings)
- **Depends on:** none
- **Problem:** `v1.5.0` reuses an upstream release number and is lower than upstream
  `v1.7.0`, whose history this module contains. CONTRIBUTING.md "Releases" and the README already
  select `v1.5.0`, and the module proxy and checksum database have no record of the earlier
  deleted tag. The inherited tags `v0.9.1`–`v1.4.2` declare the upstream module path, so
  `go get github.com/sshaplygin/go-socket.io@latest` fails today.
- **Options:**
  - Version A: keep `v1.5.0`; the release notes say that the fork does not continue
    upstream numbering and which upstream state it supersedes.
  - Version B: a number above upstream, such as `v1.9.0`; change CONTRIBUTING.md "Releases" and the
    README links first.
  - Retraction R1: `retract [v0.9.1, v1.4.2]` with a comment in the release commit, so
    `go list -m -versions` and pkg.go.dev stop offering unusable versions.
  - Retraction R2: no retraction.
- **Recommendation:** A and R1. A matches the documented release procedure; R1 removes the
  unusable versions from listings.
- **Change:** none until decided; W10 applies the decision, and W10's PR records itself in
  this Status line and adds the `Fixed by` notes on API-2, REL-7 and CL-10 (Marking progress).
- **Tests:** none. **Benchmarks:** none.
- **Docs:** CONTRIBUTING.md and README only under option B.
- **Acceptance criteria:** the decision is recorded in this Status line with the date.
- **Definition of done:** decision recorded; W10's PR merged under the standard definition
  of done, with this Status line at `; done — #<PR>`.
- **Out of scope:** v2 version numbering.

### D2. Aliases to internal packages

- **Status:** decided — option B (2026-10-24); done — #154
- **Priority:** owner decision
- **Findings:** [ARCH-2](#architecture-findings), [API-9](#api-compatibility-findings)
- **Depends on:** none
- **Problem:** 24 exported type aliases target internal packages: 7 in `engineio`
  (`Conn`, `CompressWriter`, `ReadyState`, `CloseReason`, `CloseError`, `IDGenerator`,
  `DefaultIDGenerator`), 13 in `engineio/parser` (`FrameType`, `PacketType`, `Frame`,
  `Packet`, `FrameWriter`, `FrameReader`, `PacketEncoder`, `PacketDecoder`, `PayloadWriter`,
  `PayloadReader`, `PayloadEncoder`, `PayloadDecoder`, `PayloadEncoderOption`) and 4 in the
  root package (`Endpoint`, `ClientRegistration`, `EventHandler`, `RemoteError`).
  `go doc ./engineio Conn.ID` fails and `go doc . RemoteError` shows only the alias,
  contrary to the rule in `internal/parity/api_docs_test.go:15-16`. The types remain usable.
  Internal packages cannot alias public types back, because the public packages import them.
- **Options:**
  - A: keep all aliases; add doc comments on each alias listing its methods or fields, and
    record the exemption in `TestPublicRoleDocumentation`.
  - B: declare the interface aliases as public interfaces with the same method sets in
    their public package (`engineio.Conn`, `CompressWriter`, `IDGenerator`;
    `engineio/parser.FrameWriter`, `FrameReader`, `PayloadWriter`, `PayloadReader`; root
    `Endpoint` (method `Emit(string, ...interface{})`) and `ClientRegistration` (method
    `RegisterEvent(string, EventHandler) error`)), keep the struct and named-type aliases
    with doc comments as in A, and extend `TestPublicRoleDocumentation` to the converted
    interfaces.
  - C: move every aliased type into a public package and alias inward; requires new leaf
    packages to avoid import cycles, an ARCHITECTURE.md and `.github/architecture` change,
    and a large refactor.
- **Recommendation:** B before the tag: it fixes `go doc` for the interfaces users implement
  or call, without moving implementation types.
- **Change (option B, before tag):** the declarations above; internal code keeps using its
  own types, which satisfy the public interfaces; update `internal/parity` fixtures.
- **Tests:** under B, extend `TestPublicRoleDocumentation`
  (`internal/parity/api_docs_test.go`) with `engineio`, `engineio/parser` and root entries
  for the converted interfaces; it fails before the change. Under A, record the alias
  exemption in the same test.
- **Benchmarks:** none.
- **Docs:** godoc; docs/ARCHITECTURE.md only under option C.
- **Acceptance criteria:** decision recorded; under B, `go doc ./engineio Conn.ID` prints
  the method and `make gates` passes.
- **Definition of done:** decision recorded; if B or C, the PR merged under the standard
  definition before the tag.
- **Out of scope:** v2 API.

### D3. Typed events and client bridge methods

- **Status:** decided — option A (2026-10-24); done — #186
- **Priority:** owner decision
- **Findings:** [ARCH-3](#architecture-findings), [CL-11](#clarity-findings)
- **Depends on:** W9 (it creates the client package doc that option A edits)
- **Problem:** Typed event descriptors (`Event`, `AckEvent`, `Endpoint`, `RemoteError` and
  the sentinel errors) live in the root server package, so a client program that uses typed
  events must import the server; the client runtime itself does not. `client.Client` and
  `client.Socket` export `RegisterEvent`, `SendEvent` and `RequestAck`
  (`client/event.go:10-22`, `client/typed.go:152-221`), whose parameters are
  `internal/event` types.
- **Options:**
  - A: keep the layout; document in the client package doc and V1.md "Typed events" that
    typed descriptors come from the root package, and label the bridge methods as used by
    `socketio.Event` only, with a doc comment on `Socket.RegisterEvent` (it has none) and a
    rewritten comment on `Client.RegisterEvent` (`client/event.go:9`).
  - B: a public role-neutral package (for example `event`) owning the descriptors and
    errors, with root aliases kept; requires ARCHITECTURE.md, `.github/architecture` and
    parity fixture changes.
  - C: move the bridge behind an unexported interface satisfied through `internal/event`,
    removing the exported bridge methods.
- **Recommendation:** A for v1; B and C are API redesigns better suited to v2.
- **Change:** under A, documentation only (before tag); under B or C, a before-tag PR with
  the changes listed.
- **Tests:** under B or C, parity compile fixtures for a client-only typed program.
- **Benchmarks:** none.
- **Docs:** client package doc (W9 creates it), V1.md "Typed events".
- **Acceptance criteria:** decision recorded; under A, `go doc ./client Client.RegisterEvent`
  and `go doc ./client Socket.RegisterEvent` state that they are bridges for
  `socketio.Event` only.
- **Definition of done:** decision recorded; the resulting PR merged under the standard
  definition.
- **Out of scope:** v2 typed API.

### D4. Adapter contract

- **Status:** decided — option B (2026-10-24); done — #155
- **Priority:** owner decision
- **Findings:** [ARCH-5](#architecture-findings)
- **Depends on:** W5 for the implementation PR (both rewrite the `Send`/`SendAll` comments
  in `adapter/adapter.go`)
- **Problem:** `adapter.Adapter` (`adapter/adapter.go:45-67`) has `Send` and `SendAll`
  duplicating `SendTo`, and `Rooms(nil)` overloads `AllRooms`. The optional capabilities
  `io.Closer` (asserted in `Server.Close`, `server.go:106`) and
  `BindSocketLookup(func(string) adapter.Socket)` (an anonymous interface assertion,
  `namespace_handlers.go:200`) are not named in package `adapter`. The v1 tag freezes this interface for
  third-party adapters.
- **Options:**
  - A: freeze as is and document: `Send`/`SendAll` are wrappers over `SendTo`,
    `Rooms(nil)` equals `AllRooms`, the optional capabilities are named in the package doc,
    and new capabilities arrive as optional interfaces.
  - B: A plus named optional interfaces in package `adapter` (for example
    `type SocketLookupBinder interface { BindSocketLookup(func(string) Socket) }`) used by
    `namespace_handlers.go` instead of the anonymous assertion.
  - C: remove `Send`, `SendAll` and the `Rooms(nil)` overload from the interface; breaks
    upstream-style adapters.
- **Recommendation:** B (additive).
- **Change (B):** `adapter/adapter.go` declarations and docs; `namespace_handlers.go:200`
  uses the named interface; `adapter/redis` unchanged except doc references and a
  test-file assertion.
- **Tests:** under B, existing adapter and Redis tests and a compile-time assertion
  `var _ adapter.SocketLookupBinder = (*broadcast)(nil)` in an `adapter/redis` `_test.go`
  file; under A,
  existing tests; under C, existing tests updated for the removed methods.
- **Benchmarks:** none.
- **Docs:** `adapter` package doc; V1.md "Adapters and Redis".
- **Acceptance criteria:** decision recorded; under B, `make gates` and `make test-race`
  pass.
- **Definition of done:** decision recorded; the resulting PR merged before the tag.
- **Out of scope:** the `AllRooms` comment fix (W28).

### D5. Engine connection access

- **Status:** decided — option A (2026-10-24); done — #189
- **Priority:** owner decision
- **Findings:** [ARCH-6](#architecture-findings)
- **Depends on:** none
- **Problem:** `Conn.EngineConn()` (`types.go:60-61`) returns the full `engineio.Conn`,
  including `NextReader` and `NextWriter`, which the library's own reader and writer own;
  an application call corrupts the packet stream.
- **Options:**
  - A: document `EngineConn` as metadata access (ID, Transport, Upgraded, ReadyState,
    Close) and state that frame I/O belongs to the library.
  - B: return a narrower public interface before the tag.
- **Recommendation:** A; B breaks callers for a misuse that documentation prevents.
- **Change (A):** godoc of `Conn.EngineConn`; V1.md "Connections and namespaces".
- **Tests:** none. **Benchmarks:** none.
- **Docs:** godoc, V1.md.
- **Acceptance criteria:** decision recorded; under A, `go doc . Conn.EngineConn` states
  the restriction.
- **Definition of done:** decision recorded; the resulting PR merged before the tag.
- **Out of scope:** the client role.

### D6. Exported test helpers

- **Status:** decided — option A (2026-10-24); done — #156
- **Priority:** owner decision
- **Findings:** [ARCH-11](#architecture-findings), [API-12](#api-compatibility-findings)
- **Depends on:** none
- **Problem:** `engineio/parser/fake.go` exports `FakeDiscardWriter`, `NewFakeConnReader`,
  `NewFakeConstReader` and `NewFakeConnWriter`, the last three returning unexported types;
  `engineio/transport/clock.go:22` exports `TimestampFromClock` with an unexported
  parameter type. Comments refer to a nonexistent `base.Conn`
  (`engineio/transport/manager.go:8`, `engineio/transport/websocket/connect.go:18`).
- **Options:**
  - A: before the tag, move the fakes into `_test.go` files of the packages that use them
    and unexport `TimestampFromClock`.
  - B: keep them and document them as test support.
- **Recommendation:** A: the fakes are test support, and removing them is free only before
  the tag. The public-role fixture `internal/parity/testdata/roles/engine_codecs.go` uses
  `NewFakeConnWriter` and `NewFakeConnReader` as a `FrameWriter`/`FrameReader`, and under A
  it must use a small fixture-local `FrameWriter`/`FrameReader` instead.
- **Change (A):** move or unexport as above; find users with
  `git grep -n "NewFakeConn\|NewFakeConst\|FakeDiscardWriter\|TimestampFromClock"`; update
  `internal/parity/testdata/roles/engine_codecs.go` to define its own `FrameWriter` and
  `FrameReader` (it is compiled by `internal/parity/roles_test.go` under `make gates`).
  Under either option, fix the two `base.Conn` comments.
- **Tests:** under A, existing tests compile against the moved helpers and the
  `engine_codecs` fixture compiles with its own fakes; under B, none.
- **Benchmarks:** none.
- **Docs:** godoc; under option A, the ARCHITECTURE.md row of `engineio/parser`
  ("frame/packet test fakes") and the `internal/parity` fixture.
- **Acceptance criteria:** decision recorded; under A, `go doc ./engineio/parser` no longer
  lists the fakes and `make gates` passes.
- **Definition of done:** decision recorded; the resulting PR merged before the tag.
- **Out of scope:** other API oddities.

### W11. Reject duplicate session IDs

- **Status:** done — #159
- **Priority:** after release
- **Findings:** [C3](#correctness-and-security-findings)
- **Depends on:** none
- **Problem:** `newSession` (`engineio/server.go:271-339`) registers the generated ID in
  `s.sessions` and `s.live` without checking for an existing entry. A duplicate ID from a
  custom `IDGenerator` replaces a live session in both registries, and `Server.Close` then
  leaves the first session open.
- **Change:** under `s.lifecycle` in `newSession`, before `s.sessions.Add`, reject the
  handshake when the ID is already in `s.live` or `s.sessions`: close the new transport and
  return `fmt.Errorf("generate session id: duplicate identifier")`, which follows the
  existing empty-ID error path. Document in the `IDGenerator` godoc
  (`engineio/internal/session/id.go:8-13`, `engineio/id.go:7-8`) and
  `WithSessionIDGenerator` that IDs must be unique and a duplicate fails the handshake.
- **Tests** (`-race`; from the C3 probe, not retained):
  - `TestNewSessionRejectsDuplicateID` in `engineio/server_session_test.go`: a server with
    a constant-ID generator; the first `newSession` with `newBlockingConn()` succeeds, the
    second returns an error and closes its conn; `s.sessions.Get` returns the first; after
    `s.Close()`, the first session's `Closed()` channel closes within 1 s.
  - `TestDuplicateSessionIDKeepsFirstSession` (package `engineio_test`): two websocket dials
    with a constant generator; the second handshake fails; after `server.Close()` the first
    client's read fails within 1 s.
  Both fail on master.
- **Benchmarks:** none.
- **Docs:** godoc above.
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 -run 'TestNewSessionRejectsDuplicateID|TestDuplicateSessionIDKeepsFirstSession' ./engineio` passes; both fail on master.
  - [ ] `make test-stress PKGS=./engineio/...`, `make lint`, `make cover-check` pass.
- **Definition of done:** standard.
- **Out of scope:** retrying the generator.

### W12. Close frame readers on errors and gate goroutine leaks

- **Status:** in progress — #157
- **Priority:** after release
- **Findings:** [T2](#tests-findings)
- **Depends on:** none
- **Problem:** After a parse error or a mid-frame failure, `Decoder.DecodeHeader` keeps the
  last frame (`parser/decoder.go:83`) and the read loop returns without closing it, so the
  websocket frame reader's nag goroutine and 30 s timer
  (`engineio/transport/websocket/wrapper.go:76-86`) stay alive after disconnect and
  `Server.Close`. Some client test fixtures also leak. No test fails when goroutines leak.
- **Change:**
  - Root `io.go` `serveRead` (`:75`) and `client/io.go` `serveRead` (`:159`): close the
    decoder's last frame (`Decoder.DiscardLast` or `Close`) on the decode-error and
    transport-failure return paths.
  - New `main_test.go` in the root, `client` and `engineio` packages: a `TestMain` that,
    after `m.Run()`, waits up to 5 s until no goroutine other than the one running
    `TestMain` has a stack frame in a non-test file of this module, and otherwise prints
    their stacks and exits with status 1. Generalize `moduleGoroutines`
    (`log_test.go:580`), which matches frames in non-`_test.go` files under the directory
    of its own file, into an unexported helper copied into each package's `main_test.go`
    that matches the module root directory (the nearest parent directory holding
    `go.mod`). Use `runtime.Stack`; add no module dependency. A goroutine started by test
    code is reported when its stack is inside library code, for example a test handler
    called from a library dispatcher.
  - Before writing fixes, run the gate once on master and list the reported stacks in the
    PR; the fixes are limited to that list. Fix the client fixtures the gate reports:
    close clients with `t.Cleanup`, and use buffered or `select`-guarded channels in
    handlers. Until W23, a path that leaves a websocket frame open keeps a 30 s nag
    goroutine, longer than the 5 s wait; close the frame on that path rather than raising
    the wait.
  - The Benchmarks workflow runs the same `TestMain` around the W2 benchmarks in these
    packages, under `pipefail` (`.github/workflows/benchmarks.yml:148,160`). Fix any W2
    benchmark the gate reports; benchmarks close their servers and clients in
    `b.Cleanup`.
- **Tests:** `TestMalformedPacketReleasesFrameReader` (root, `-race`; from the T2 probe,
  not retained): a websocket client connects and sends `49`; after `OnDisconnect` and
  `Server.Close`, no library goroutine remains within 1 s (helper shared with the
  `TestMain` gate). It fails on master (one frame-reader goroutine for 30 s).
- **Benchmarks:** none.
- **Docs:** CLAUDE.md "Conventions": test packages fail on leaked library goroutines.
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 . ./client/... ./engineio/...` passes with the gate.
  - [ ] `go test -run '^$' -bench . -benchtime=1x . ./client ./engineio` passes with the gate (the Benchmarks workflow runs the same `TestMain`).
  - [ ] `go test -race -count=1 -run TestMalformedPacketReleasesFrameReader .` passes; fails on master.
  - [ ] `make test-stress` passes.
- **Definition of done:** standard.
- **Out of scope:** removing the nag timers (W23).

### W13. Document and log the OnError contract

- **Status:** done — #161
- **Priority:** after release
- **Findings:** [CL-3](#clarity-findings), [C4](#correctness-and-security-findings)
- **Depends on:** none
- **Problem:** `OnError` receives a nil `Conn` for root connect and middleware failures;
  neither `Server.OnError` (`server.go:151`) nor `ServerNamespace.OnError`
  (`namespace.go:57`) says so, and V1.md:41 says errors receive an inactive connection
  context. A panic inside `OnError` is recovered and discarded without a log
  (`namespace_handlers.go:223`).
- **Change:** log the recovered value in `namespaceHandler.ErrorHandler` as
  `Warn("socketio: error handler panic", nspAttr(<namespace>), "err", fmt.Errorf("%v", v))`;
  pass the server logger into the namespace handler (or wrap at the call sites, which have
  `c.log`). Godoc of both `OnError` methods: nil `Conn` for root connect/middleware
  failures, an inactive `Conn` for other namespaces, panics recovered and logged at WARN.
  Fix V1.md:41.
- **Tests:** `TestOnErrorPanicIsLogged` in root `log_test.go` with `captureLogs`: root
  `OnConnect` returns an error and the `OnError` handler panics; assert one WARN record with
  keys `nsp` and `err`. It fails on master. `TestNoBadKeyAttrs` passes.
- **Benchmarks:** none.
- **Docs:** godoc, `docs/V1.md:36-42`.
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 -run 'TestOnErrorPanicIsLogged|TestNoBadKeyAttrs' .` passes; the first fails on master.
- **Definition of done:** standard.
- **Out of scope:** client-role error handlers.

### W14. Document dynamic namespace retention

- **Status:** done — #164
- **Priority:** after release
- **Findings:** [C2](#correctness-and-security-findings), [ARCH-9](#architecture-findings)
- **Depends on:** none
- **Problem:** Namespaces created through `OfFunc` (`namespace.go:161`) stay in
  `Server.views`, the registry and the parent's children map for the server's lifetime,
  even when every connection to them is rejected. socket.io 2.x behaves the same way, but
  neither the godoc nor V1.md says so.
- **Change:** `OfFunc` godoc and V1.md "Connections and namespaces": children are retained
  for the server's lifetime; use restrictive match functions and authenticate at the root
  namespace.
- **Tests:** none (documentation). **Benchmarks:** none; state no memory figure in the docs.
- **Docs:** godoc, `docs/V1.md`.
- **Acceptance criteria:** `go doc . Server.OfFunc` states the retention.
- **Definition of done:** standard.
- **Out of scope:** a cleanup option equivalent to socket.io 4.6
  `cleanupEmptyChildNamespaces`; it needs its own item and a benchmark of registry memory.

### W15. Support Engine.IO base64 over websocket

- **Status:** in progress — #162
- **Priority:** after release
- **Findings:** [interop-1](#interoperability-findings)
- **Depends on:** W6 (removes the limitation it documents)
- **Problem:** The websocket transport ignores `b64=1`. A binary packet is sent as a binary
  frame, and an inbound text frame `b4AQI=` is parsed as packet type `'b' − '0'` = 50, so the
  session closes with `invalid Engine.IO packet type 50`. A `forceBase64` client loses its
  session when it sends binary data (0/8 runs on websocket and upgrade); upstream `v1.7.0`
  behaves the same way.
- **Change:** in `engineio/transport/websocket`, read `b64` from the request URL kept in
  `conn.url`; when set, decode text frames starting with `b` as base64 binary packets
  (`b<type><base64 data>`), and write binary packets as `b<type><base64 data>` text frames,
  as engine.io 3.6.2 does. Remove the limitation from PROTOCOL.md and describe the behavior.
- **Tests:** `TestWebsocketBase64Mode` (package `engineio`, in a new file
  `engineio/websocket_base64_test.go`, `-race`; from the interop-1 probe, not retained): over `?EIO=3&transport=websocket&b64=1`, a server binary MESSAGE
  `{1,2}` arrives as text `b4AQI=`, and a client text `b4AQI=` arrives at
  `Conn.NextReader` as a binary frame `{1,2}` with the session still open; subtest for the
  polling-to-websocket upgrade with `b64=1` (`newParityWire`, `server_parity_test.go:49`).
  Fails on master. Conformance: a `forceBase64` scenario for websocket and upgrade in
  `internal/conformance/testdata/node/client.js`.
- **Benchmarks:** `BenchmarkWebsocketEcho` (W2): no regression for non-b64 sessions.
- **Docs:** `docs/PROTOCOL.md`.
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 -run TestWebsocketBase64Mode ./engineio` passes; fails on master.
  - [ ] `make conformance` passes with both pinned clients.
- **Definition of done:** standard.
- **Out of scope:** base64 mode in the Go client.

### W16. Document the remaining wire differences

- **Status:** done — #167
- **Priority:** after release
- **Findings:** [interop-2](#interoperability-findings), [interop-3](#interoperability-findings),
  [interop-4](#interoperability-findings), [interop-5](#interoperability-findings),
  [interop-6](#interoperability-findings)
- **Depends on:** W6
- **Problem:** PROTOCOL.md does not describe five differences from the reference server:
  every JSON payload and the OPEN packet end with `\n` (`parser/encoder.go:144`); polling
  POST accepts only `application/octet-stream` or `text/plain` with UTF-8 charset
  (`engineio/transport/polling/util.go:21`), while the reference treats every
  non-octet-stream body as text; ERROR payloads are always `err.Error()` strings
  (`packets.go:214,278`); an untyped handler whose argument type does not match sends no ACK,
  so the JS callback never fires (`packets.go:103`); a server-initiated websocket close
  sends no close frame (`engineio/transport/websocket/connect.go:117`).
- **Change:** one PROTOCOL.md entry per difference, each stating the behavior and that it
  is intentional or a known limitation; the ACK rule also in V1.md next to the ACK rules.
- **Tests:** none (documentation). **Benchmarks:** none.
- **Docs:** `docs/PROTOCOL.md`, `docs/V1.md`.
- **Acceptance criteria:** each of the five behaviors has a PROTOCOL.md entry.
- **Definition of done:** standard.
- **Out of scope:** code changes (dropping the newline, accepting more media types, an
  `err.data` equivalent, error ACKs, close frames); each would be its own item with a
  conformance test.

### W17. Conformance heartbeat and Redis teardown

- **Status:** done — #168
- **Priority:** after release
- **Findings:** [T3](#tests-findings), [interop-7](#interoperability-findings),
  [interop-8](#interoperability-findings)
- **Depends on:** none
- **Problem:** PARITY.md:20 attributes the Engine.IO heartbeat to `engineio`/session tests
  and raw conformance tests together, but every conformance server uses the 25 s default
  and no JS scenario stays connected long enough to ping; only session tests exercise the
  heartbeat. The mixed-cluster polling scenario
  (`internal/conformance/testdata/node/server/redis-cluster.js:199`) takes about 33 s,
  almost all teardown, against `runNode`'s 45 s timeout.
- **Change:**
  - `internal/conformance/conformance_test.go`: `TestJSHeartbeat`, per transport, against a
    server with `WithPingInterval(200 * time.Millisecond)` and
    `WithPingTimeout(200 * time.Millisecond)`; new `heartbeat` scenario in `client.js`: stay
    connected 1 s, count engine `pong` events (at least 3), then ACK an `echo`; second
    subtest: the client suppresses pings and the Go side asserts `OnDisconnect` with reason
    `ping timeout` within 1 s. Neither pinned client has an option for this: after
    `connect`, replace the engine's ping sender (`socket.io.engine.ping = function () {}`)
    and clear `socket.io.engine.pingIntervalTimer`, then keep the process alive for 1 s.
    Verify that this suppresses pings with both 1.7.4 and 2.5.0 and state the result in
    the PR.
  - `redis-cluster.js`: find what keeps the process alive for about 30 s after the
    `finally` block (`:199-200`), which already closes every client socket and manager
    before `io.close()`; time each cleanup step. Then remove that cause (for example, wait
    for the reference server's `disconnect` event of every client before `io.close()`), or
    end the script with `process.exit(process.exitCode || 0)` after cleanup. The PR
    states which was done and quotes the subtest's elapsed time before and after.
  - `docs/PARITY.md:20`: name the tests that own the heartbeat.
- **Tests:** the scenarios above, with both pinned clients.
- **Benchmarks:** none.
- **Docs:** `docs/PARITY.md`.
- **Acceptance criteria:**
  - [ ] `make conformance` passes with `socket.io-client` 1.7.4 and 2.5.0.
  - [ ] The `TestRedisMixedCluster` polling subtest finishes in under 10 s (`go test -json` elapsed).
- **Definition of done:** standard.
- **Out of scope:** browser clients.

### W18. Stop per-packet polling timers

- **Status:** open
- **Priority:** after release
- **Findings:** [PERF-2](#performance-findings)
- **Depends on:** W2
- **Problem:** `payload.readTimeout` and `writeTimeout`
  (`engineio/transport/polling/payload.go:399-422`) create a `time.After` timer per packet
  and never stop the losing one. When the application is built with pre-1.23 timer
  semantics (main module go line below 1.23, a pre-1.23 toolchain, or
  `asynctimerchan=1`), each timer keeps memory alive for about 45 s: 50,000 packets grew the
  heap 1.0 → 42.8 MiB, an ad hoc measurement (no committed benchmark yet) that this item's
  test and W2's `BenchmarkPayloadRoundTrip` capture. This module's own go directive does
  not decide it.
- **Change:** one reusable timer per direction in `payload` (`time.NewTimer`,
  `Stop`/`Reset`), or stop the timer when the `select` at each call site (`:128`, `:150`,
  `:214`, `:240`, `:438`, `:462`, `:490`, `:526`) returns on another case.
- **Tests:** `TestPollingTimersReleasedUnderAsyncTimerChan` (package `polling`): re-executes
  the test binary with `GODEBUG=asynctimerchan=1` and a marker variable; the child feeds
  50,000 packets through one `newPayload(false)` with 45 s deadlines, each with the
  goroutine structure of W2's `BenchmarkPayloadRoundTrip` (`FeedIn` of `6:4hello` in a
  goroutine, `NextReader`, read to EOF and `Close` in the test goroutine, then wait for
  `FeedIn`), and reports the `HeapInuse` growth after `runtime.GC()`; the parent asserts
  less than 8 MiB. Fails on master.
- **Benchmarks:** `BenchmarkPayloadRoundTrip` (package `engineio/transport/polling`) and
  `BenchmarkPollingEcho` (package `engineio`), against the PR base. Expected: lower `B/op`
  and `allocs/op`.
- **Docs:** none.
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 -run TestPollingTimersReleasedUnderAsyncTimerChan ./engineio/transport/polling` passes; fails on master.
  - [ ] The benchmark comparison is quoted in the PR.
- **Definition of done:** standard, with the benchmark result in the Status line.
- **Out of scope:** websocket timers (W23).

### W19. Size the Redis publishing pool

- **Status:** in progress — #158; BenchmarkRedisSendToParallel: 24085 → 23560 ns/op, conns/op 0.006918 → 0.001983 (manual, darwin/arm64)
- **Priority:** after release
- **Findings:** [PERF-4](#performance-findings)
- **Depends on:** W2, W4
- **Problem:** The publishing pool (`adapter/redis/adapter.go:100`) has `MaxIdle: 4` and no
  `MaxActive`; 64 goroutines × 200 `SendAll` (12,800 publishes) opened 2,259–2,556 new TCP
  connections, an ad hoc measurement (no committed benchmark yet) that W2's
  `BenchmarkRedisSendToParallel` captures; this item's test asserts the bound at 16
  goroutines.
- **Change:** raise `MaxIdle` to 16 (named constant), without `Wait`, so callers never
  block on the pool.
- **Tests:** `TestRedisPublishPoolReusesConnections` (package `redis`): 16 goroutines × 100
  `SendAll` against miniredis, counting the `TotalConnectionCount()` increase from after
  adapter construction (so the subscriber and setup connections are excluded); assert at
  most 20 (16 idle publishing connections plus margin). This workload was not measured in
  the review (only 64 × 200 was): run the test on master before the fix and quote the
  count; if master stays within 20, raise the per-goroutine count to 16 × 1,000 and keep
  16 goroutines. The 20-connection bound holds only while the number of concurrent
  publishers does not exceed the new `MaxIdle`: with more (for example the 64 × 200 of the
  PERF-4 measurement), the fixed pool still closes every connection above 16 on return and
  redials, so do not use this assertion for more than 16 goroutines.
- **Benchmarks:** `BenchmarkRedisSendToParallel` (W2) against the PR base; expected lower
  `ns/op` and `allocs/op`; `conns/op` clearly lower than on the base, though not
  necessarily near 0, because `b.SetParallelism(16)` runs 16 × `GOMAXPROCS` goroutines,
  more than the 16 idle connections; `conns/op` comes from a manual run of
  `SOCKETIO_BENCH_METRICS=1 go test -run '^$' -bench 'RedisSendToParallel' -benchmem -count=10 ./adapter/redis`
  on base and head, compared with `benchstat` (quote both).
- **Docs:** none.
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 -run TestRedisPublishPoolReusesConnections ./adapter/redis` passes; fails on master.
- **Definition of done:** standard, with the benchmark result in the Status line.
- **Out of scope:** a public pool-size option.

### W20. Give the polling client its own HTTP transport

- **Status:** open
- **Priority:** after release
- **Findings:** [PERF-5](#performance-findings)
- **Depends on:** W2
- **Problem:** `polling.Default` (`engineio/transport/polling/transport.go:24`) uses an
  `http.Client` with a nil `Transport`, so every Go polling client shares
  `http.DefaultTransport` with two idle connections per host. With ten clients, 1,000
  broadcasts opened about 3,000 new TCP connections, and sustained broadcasts on darwin
  localhost exhausted ephemeral ports within about 9–13 s. Each figure is an ad hoc
  measurement (no committed benchmark yet) that this item's test and W2's
  `BenchmarkPollingClientsBroadcast/clients-10` capture.
- **Change:** give `polling.Default.Client` its own transport,
  `http.DefaultTransport.(*http.Transport).Clone()` with `MaxIdleConnsPerHost = 64`; godoc of
  `polling.Transport.Client` and V1.md "Client limits": use
  `client.WithTransports(&polling.Transport{Client: ...})` for custom limits.
- **Tests:** `TestPollingClientsReuseConnections` (package `client_test`): 10 polling clients,
  200 broadcasts; the httptest server's `ConnState` counts at most 40 new connections.
  Fails on master.
- **Benchmarks:** `BenchmarkPollingClientsBroadcast/clients-10` (W2) against the PR base;
  expected lower time and allocations; `dials/op` from a manual run of
  `SOCKETIO_BENCH_METRICS=1 go test -run '^$' -bench 'PollingClientsBroadcast' -benchmem -count=10 ./client`
  on base and head, compared with `benchstat`. W2 gated the benchmark behind
  `SOCKETIO_BENCH_POLLING=1`, and this PR removes the gate. Because the PR base still
  has the gate, the CI comparison shows no base side: run the manual comparison with
  `SOCKETIO_BENCH_POLLING=1` set for the base run as well, preferably on Linux (a darwin
  base run can lose a sample to port exhaustion), and grep both outputs for `--- FAIL`
  (see [REL-13](#release-engineering-findings)).
- **Docs:** godoc, `docs/V1.md` "Client limits".
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 -run TestPollingClientsReuseConnections ./client` passes; fails on master.
- **Definition of done:** standard, with the benchmark result in the Status line.
- **Out of scope:** a transport per dialed client.

### W21. Prepare broadcasts once and skip the single-room dedup map

- **Status:** in progress — #160
- **Priority:** after release
- **Findings:** [PERF-6](#performance-findings), [PERF-12](#performance-findings)
- **Depends on:** W2
- **Problem:** `deliverBroadcast` (`adapter/memory.go:144-153`) calls each target's `Emit`,
  which prepares the packet separately, so a broadcast encodes once per recipient.
  `broadcastMembers` (`adapter/memory.go:110-134`) builds a `seen` map even for one room
  without exclusions.
- **Change:** `adapter` cannot import `parser` (docs/ARCHITECTURE.md), so declare in package
  `adapter` an unexported optional interface with exported methods, for example
  `PrepareEmit(opts Options, event string, args ...interface{}) (interface{}, error)` and
  `EmitPrepared(opts Options, packet interface{})`, implemented by the root
  `namespaceConn`; `Memory` (and the local part of Redis `SendTo`) prepares once through the
  first target and delivers the opaque packet to every target, with per-socket ACK headers
  through `PreparedPacket.WithHeader`. `adapter.Socket` does not change. In
  `broadcastMembers`, skip the map when `len(opts.Rooms) == 1 && len(opts.Except) == 0`, and
  pre-size the result to that room's size. The namespace-wide path (`len(opts.Rooms) == 0`)
  keeps the dedup map: every socket is in its own ID room (`socket.go:398`) and possibly in
  named rooms, so without the map a socket in a named room would receive the broadcast more
  than once.
- **Tests:** `TestBroadcastMarshalsPayloadOnce` (root): a value whose `MarshalJSON` counts
  calls, broadcast to 10 sockets, is marshaled once (10 on master). Existing union,
  exclusion and dedup tests in `adapter` pass, and `TestNamespaceBroadcastOnce` (root,
  `namespace_api_test.go`) must keep passing: it guards the namespace-wide dedup.
- **Benchmarks:** against the PR base: `BenchmarkBroadcastWebsocket/*` (root), expected
  lower time and allocations; `BenchmarkMemorySendTo/one-room` (`adapter`), expected lower
  allocations (dedup map skipped; its no-op sockets do not implement the prepare interface,
  so prepare-once is not exercised there); `/all` and `/three-rooms`: no change expected
  from the dedup change.
- **Docs:** none.
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 -run TestBroadcastMarshalsPayloadOnce .` passes; fails on master.
  - [ ] `make architecture-check` passes.
- **Definition of done:** standard, with the benchmark result in the Status line.
- **Out of scope:** the Redis publication envelope encoding.

### W22. Copy packets only when an ACK ID is added

- **Status:** open
- **Priority:** after release
- **Findings:** [PERF-7](#performance-findings)
- **Depends on:** W2
- **Problem:** Normal and volatile sends call `PreparedPacket.WithHeader` (`socket.go:154`,
  `socket.go:369`) even when `registerAck` (`socket.go:164`) added no ACK ID; `WithHeader`
  copies the payload into a new text frame (`parser/encoder.go:58-62`). The typed send path
  (`typed.go:114-120`) already copies only when it adds an ACK ID.
- **Change:** call `WithHeader` only when `registerAck` changed the header.
- **Tests:** none new; behavior is unchanged. Existing emit and ACK tests
  (`packet_lifecycle_test.go`, `event_integration_test.go`, `connection_typed_events_test.go`)
  pass.
- **Benchmarks:** `BenchmarkEmitWebsocket/string` (root) against the PR base; expected
  lower `B/op` and `allocs/op`.
- **Docs:** none.
- **Acceptance criteria:** `make test-race` passes; the benchmark comparison is quoted.
- **Definition of done:** standard, with the benchmark result in the Status line.
- **Out of scope:** the client role (`client/socket.go:138`, `client/typed.go:121`), unless
  measured in the same PR.

### W23. Remove per-frame websocket timers

- **Status:** open
- **Priority:** after release
- **Findings:** [PERF-8](#performance-findings)
- **Depends on:** W2, W12
- **Problem:** Each `rcWrapper` and `wcWrapper` (`engineio/transport/websocket/wrapper.go:76-94,146-164`)
  starts a goroutine and a 30 s timer that only log a DEBUG record when a frame stays open.
- **Change:** remove the nag goroutines and timers and their two DEBUG messages.
- **Tests:** existing websocket tests; W12's leak gate keeps passing.
- **Benchmarks:** `BenchmarkWebsocketEcho/*` (`engineio`) against the PR base; expected
  lower time and `allocs/op`.
- **Docs:** none (the messages are not documented).
- **Acceptance criteria:** `go test -race -count=1 ./engineio/...` passes; benchmark quoted.
- **Definition of done:** standard, with the benchmark result in the Status line.
- **Out of scope:** the write lock (W1).

### W24. Look up room members without a scan

- **Status:** open
- **Priority:** after release
- **Findings:** [PERF-9](#performance-findings)
- **Depends on:** W2
- **Problem:** `roomSocket` (`server.go:252-264`), used by `JoinRoom`, `LeaveRoom` and
  `LeaveAllRooms`, copies and scans every socket of the namespace.
- **Change:** under `nh.connectedMu.RLock()`, check `nh.connected[nc.ID()] == nc`
  (`namespace_handlers.go:12-13`), keeping the existing guards: the `*namespaceConn`
  assertion, `h := s.getNamespace(nsp)` with `h != nil && h.Err() == nil`, membership in
  that handler's `connected` map (which replaces the `h.Sockets()` scan and keeps a socket
  of a replaced namespace handler from matching), `nc.Connected()` and
  `nc.beginRoomOperation()`.
- **Tests:** existing room tests (`lifecycle_test.go`,
  `namespace_generation_regression_test.go`, `adapter_test.go`) pass.
- **Benchmarks:** `BenchmarkJoinRoom/sockets-200` (root) against the PR base; expected
  much lower time.
- **Docs:** none.
- **Acceptance criteria:** `make test-race` and `make test-stress PKGS=.` pass; benchmark
  quoted.
- **Definition of done:** standard, with the benchmark result in the Status line.
- **Out of scope:** adapter room storage.

### W25. Remove extra JSON passes from decoding

- **Status:** open
- **Priority:** after release
- **Findings:** [PERF-11](#performance-findings)
- **Depends on:** W2
- **Problem:** Decoding makes extra JSON passes, unchanged since before #143: `json.Valid`
  and a `RawMessage` split in `validatePayload` (`parser/decoder.go:244-248`), then
  per-argument `Unmarshal` in `DecodeArgs`. The per-packet `bufio.Reader`
  (`parser/decoder.go:87`) is not the cause; `v1.4.2` allocates one too. A comparison put
  decoding 37–42% slower than `v1.4.2` (historical ad hoc measurement; not reproducible by
  a committed benchmark; see PERF-11); W2's `BenchmarkDecode` against the PR base is this
  item's only performance evidence.
- **Change:** drop the separate `json.Valid` pass (the `RawMessage` unmarshal already
  validates) and avoid parsing the payload more than once; optionally reuse a
  `bufio.Reader` per `Decoder`.
- **Tests:** existing malformed-input tests (`parser/validation_test.go`,
  `parser/limits_external_test.go`, `FuzzDecoderDiscardPacket` seeds) pass with the same
  errors.
- **Benchmarks:** `BenchmarkDecode/*` (`parser`) against the PR base. Expected lower time,
  `allocs/op` not higher. No comparison with `v1.4.2` is made: the W2 benchmark file
  cannot compile at `v1.4.2` (no `Prepare`, and its `FrameReader` uses go-engine.io's
  `engineio.FrameType`), and the committed `BenchmarkDecode` against the PR base is the only
  performance evidence. The `v1.4.2` figure in PERF-11 is a historical ad hoc
  measurement; not reproducible by a committed benchmark.
- **Docs:** none.
- **Acceptance criteria:** `go test -race -count=1 ./parser/...` passes; benchmark quoted.
- **Definition of done:** standard, with the benchmark result in the Status line.
- **Out of scope:** encoding (W3).

### W26. Reduce the idle connection footprint

- **Status:** open
- **Priority:** after release
- **Findings:** [ARCH-4](#architecture-findings), [PERF-13](#performance-findings),
  [ARCH-13](#architecture-findings)
- **Depends on:** W2
- **Problem:** `Start` sets `boundary` before admission (`connection.go:276`), so the
  `writeChan` delivery path is never used at runtime; only its capacity feeds the queue
  budget, but its buffer costs about 6 KiB per connection (88 B × 64 = 5,632 B, allocated as
  6,144 B). An idle websocket connection runs 7 goroutines, 6 of them started by the library.
  Each figure is an ad hoc measurement (no committed benchmark yet) that this item's test
  and W2's `BenchmarkConnectWebsocket` capture; `v1.4.2` ran 4 (historical ad hoc
  measurement; not reproducible by a committed benchmark). docs/ARCHITECTURE.md
  "Runtime" names only the read-dispatch, write and error workers.
- **Change:** remove the `writeChan` path and `boundary`, keeping an integer capacity and a
  single ordered queue (`queue.go`, `connection.go`, `socket.go` budget expressions); update
  tests that build connections without `Start`. `boundary` is also read by the namespace
  connect/disconnect records and the `EngineConn()`/`Request()` results
  (`connection.go:177,259-269`), the non-root `Conn.ID()` format (`socket.go:260`; the
  format W8 documents), `Disconnect` and connection-level `Use` (`socket.go:269,317`), the
  emitter (`emitter.go:20`), the close and drain records (`close.go:19,126,134,145`) and
  queue admission (`queue.go:65`); find them with
  `git grep -n '\.boundary' -- '*.go' ':!*_test.go' ':!v2'`. Since `Start` always sets it
  at runtime, make every one of these behave as if it were true, and list in the PR the
  tests that relied on the `boundary == false` behavior of connections built without
  `Start`. Merge the engine close watcher with
  `observeEngineClose` (`typed.go:134`) and start the error loop (`io.go:9`) lazily.
  Update docs/ARCHITECTURE.md "Runtime" with the resulting workers and that a blocking
  handler also blocks the header worker handoff.
- **Tests:** `TestIdleWebsocketConnectionGoroutines` (root): 50 idle websocket connections;
  the goroutine increase per connection is at most 5 (7 on master).
- **Benchmarks:** `BenchmarkConnectWebsocket` (root) against the PR base; expected lower
  `B/op` (at least the 6 KiB channel buffer) and `allocs/op`.
- **Docs:** docs/ARCHITECTURE.md.
- **Acceptance criteria:**
  - [ ] `go test -race -count=1 -run TestIdleWebsocketConnectionGoroutines .` passes; fails on master.
  - [ ] `make test-stress PKGS=.` and `make gates` pass.
- **Definition of done:** standard, with the benchmark result in the Status line.
- **Out of scope:** client-role workers.

### W27. Package documentation and lint checks

- **Status:** open
- **Priority:** after release
- **Findings:** [CL-5](#clarity-findings)
- **Depends on:** W9 (root and client package docs)
- **Problem:** 9 of 12 public packages have no package comment, and `.golangci.yml` disables
  `ST1000` and `ST1020`–`ST1022`.
- **Change:** package comments for `engineio`, `engineio/client`, `engineio/parser`,
  `engineio/transport`, `engineio/transport/polling`, `engineio/transport/websocket` and
  `parser`; remove `-ST1000`, `-ST1020`, `-ST1021`, `-ST1022` from `.golangci.yml` and fix
  what they report.
- **Tests:** none. **Benchmarks:** none.
- **Docs:** godoc.
- **Acceptance criteria:** `go list -f '{{.ImportPath}} {{.Doc}}' ./...` shows a doc for
  all 12 public packages; `make lint` reports 0 issues.
- **Definition of done:** standard.
- **Out of scope:** other staticcheck exclusions.

### W28. Adapter godoc and Redis log keys

- **Status:** open
- **Priority:** after release
- **Findings:** [CL-6](#clarity-findings), [CL-7](#clarity-findings),
  [ARCH-12](#architecture-findings)
- **Depends on:** none
- **Problem:** `redis.Cluster` methods have unnamed parameters (`adapter/redis/cluster.go:24-35`);
  the `adapter.Adapter` `AllRooms` comment refers to a connection, contrary to the
  implementation (the `Rooms` comment is accurate); both Redis log records,
  `cluster.go:104` and `requests.go:274`, use the key `namespace`, which the logger contract
  does not list (`nsp`).
- **Change:** name the `Cluster` parameters (ctx, sid, room, closeEngine) with one-line
  docs including error and timeout semantics; rewrite the `Adapter` method docs as proper
  doc comments (`AllRooms` lists all rooms of the namespace, cluster-wide for Redis; and,
  unless D4 removed the overload, `Rooms(nil)` equals `AllRooms`); change both log keys to
  `nsp`.
- **Tests:** in root `log_test.go`, a Redis variant of `TestNoBadKeyAttrs`: with the
  miniredis server closed, `ClearRoom` triggers `socketio: redis cluster request failed`;
  the key checker reports no bad key. Fails on master (`namespace`).
- **Benchmarks:** none.
- **Docs:** godoc.
- **Acceptance criteria:** the new test passes and fails on master; `make lint` passes.
- **Definition of done:** standard.
- **Out of scope:** the adapter contract (D4).

### W29. Bound Redis registration time in tests and docs

- **Status:** open
- **Priority:** after release
- **Findings:** [T4](#tests-findings)
- **Depends on:** none
- **Problem:** Registering a handler on a new namespace constructs the Redis adapter
  synchronously and waits up to 10 s for subscription acknowledgements
  (`redisDialTimeout`, `adapter/redis/subscription.go:15`). `TestRedisServerCloseStopsBroadcasts`
  (`redis_integration_test.go:167`) spends those 10 s, and V1.md does not state the
  duration.
- **Change:** the wait is the package variable `redisDialTimeout`, which only
  `adapter/redis` tests can override (`readiness_test.go:127-129`, `shutdown_test.go:23-24`).
  Add a public option in `adapter/redis` for it (proposed name `WithDialTimeout`, default
  10 s; the name is confirmed in review) and use it instead of the variable; the root test
  uses 200 ms and asserts that registration returns within 1 s; V1.md "Adapters and
  Redis": registration dials Redis synchronously and can block up to that timeout.
- **Tests:** the updated `TestRedisServerCloseStopsBroadcasts`; `options_test.go` covers the
  option.
- **Benchmarks:** none.
- **Docs:** godoc, `docs/V1.md`.
- **Acceptance criteria:** `go test -json -run TestRedisServerCloseStopsBroadcasts .` shows
  an elapsed time under 2 s; `make gates` passes.
- **Definition of done:** standard.
- **Out of scope:** asynchronous adapter construction.

### W30. Document which server API to use

- **Status:** open
- **Priority:** after release
- **Findings:** [CL-8](#clarity-findings)
- **Depends on:** none
- **Problem:** The `Server.*(namespace, ...)` methods and `Of(...)`/`Emitter` overlap with
  different ACK semantics, and nothing says which to use; the two `Use` variants
  (`namespace.go:40,65`, connection-level `socket.go:313`) are not cross-referenced.
- **Change:** V1.md: the `Server.*(namespace, ...)` methods are retained wrappers in the
  upstream `v1.7.0` call shape (with the API-8 caveat for `v1.4.2` code), `Of(...)`/`Emitter`
  is the primary API, and the ACK difference; godoc cross-references between the `Use`
  variants.
- **Tests:** none. **Benchmarks:** none.
- **Docs:** `docs/V1.md`, godoc.
- **Acceptance criteria:** V1.md has the guidance; `go doc . Server.Use` refers to
  `ServerNamespace.Use` and `Namespace.Use` (the connection-level middleware every `Conn`
  has; `go doc` cannot resolve `Conn.Use`).
- **Definition of done:** standard.
- **Out of scope:** removing wrappers.

### W31. Document the close, drain and upgrade state machines

- **Status:** open
- **Priority:** after release
- **Findings:** [CL-12](#clarity-findings)
- **Depends on:** none
- **Problem:** Connection close phases and client probe/upgrade have no written state
  description; `probeTransport` (`engineio/client/client.go:489`) is 219 lines with
  cyclomatic complexity 45.
- **Change:** a docs/ARCHITECTURE.md "Runtime" subsection on the connection close phases
  (closing, draining, seal, discard) and on client probe/upgrade; comments naming which
  mutex guards each field in `connection.go` and `engineio/internal/session/session.go`.
- **Tests:** none. **Benchmarks:** none.
- **Docs:** docs/ARCHITECTURE.md.
- **Acceptance criteria:** the subsection exists and matches the code; `make gates` passes.
- **Definition of done:** standard.
- **Out of scope:** refactoring (W32).

### W32. Cancellable pause and explicit close state

- **Status:** open
- **Priority:** after release
- **Findings:** [ARCH-8](#architecture-findings)
- **Depends on:** W31
- **Problem:** When a probe fails after the old transport's pause has started,
  `probeTransport` calls `Resume` every 1 ms until the pause goroutine returns, because a
  `Resume` issued before that goroutine enters `Pause` does not cancel it
  (`engineio/transport/polling/connect.go:124-150`). The connection close condition is a
  compound expression rather than an explicit state.
- **Change:** may land as two PRs, both recorded in this Status line: (1) give `Pause` a
  cancellation path (a stop channel or context) and remove the retry loop; split
  `probeTransport` into dial, probe exchange, pause and commit steps; (2) model the
  connection close as an explicit state enum.
- **Tests:** a polling test in which `Resume` is called before `Pause` starts; assert that
  `Pause` returns within 100 ms and that `probeTransport` calls `Resume` exactly once
  (count calls through a test-only wrapper conn passed to the client); existing upgrade
  tests (`engineio/client/upgrade_test.go`) pass; `-race` and
  `make test-stress PKGS='./engineio/...'`.
- **Benchmarks:** none (no hot path).
- **Docs:** docs/ARCHITECTURE.md subsection from W31.
- **Acceptance criteria:** `go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 29 engineio/client`
  reports none of the functions split from `probeTransport`; `make test-stress` passes.
- **Definition of done:** standard.
- **Out of scope:** upgrade protocol changes.

### W33. Pair or share role-neutral runtime pieces

- **Status:** open
- **Priority:** after release
- **Findings:** [ARCH-7](#architecture-findings)
- **Depends on:** none
- **Problem:** The header worker and listener/dispatch logic exist in both roles
  (for example `client/namespace_handlers.go:100`); `dupl` found 15 clone groups pairing
  `client/` with the root package.
- **Change:** add paired behavioral tests that run the same scenario against both roles for
  the listener registry, frame-failure tracking and header worker. Scenarios, one test
  each, table-driven over both roles: listener registration and removal; a frame-reader
  failure counted once and reported to `OnError`; a header-worker handoff while a handler
  blocks. Regenerate the clone list with
  `go run github.com/mibk/dupl@v1.0.0 -t 100 -plumbing . client` (the original output was
  not retained; if a different threshold or path set reproduces the recorded 16 groups,
  use it and state it) and
  quote the command and output in the PR; the count is informational and not an
  acceptance criterion. Extraction into an
  internal package allowed for both roles is optional and requires docs/ARCHITECTURE.md and
  `.github/architecture` policy updates.
- **Tests:** the paired tests.
- **Benchmarks:** none, unless code is extracted (then the W2 broadcast and emit benchmarks
  must show no regression).
- **Docs:** docs/ARCHITECTURE.md only if code moves.
- **Acceptance criteria:** the paired tests pass for both roles; `make gates` passes.
- **Definition of done:** standard.
- **Out of scope:** merging the roles.

### W34. Example module hygiene

- **Status:** open
- **Priority:** after release
- **Findings:** [CL-13](#clarity-findings), [REL-10](#release-engineering-findings)
- **Depends on:** none
- **Problem:** Example module paths have typos (`_examples/default-http` declares
  `.../_examples/deault-http`, `_examples/dockerize-default-http` declares
  `.../docker-with-deault-http`, `_examples/redis-adapter-unix-socket` declares
  `.../_example/...`); example modules pin old `golang.org/x/net` versions; the error
  strings `write buffer full` (`errors.go:19`, `client/errors.go:16`) and `empty addr`
  (`client/client.go:87`) carry no package prefix, unlike `socketio: ...` and
  `client: ...` errors; `WithPingTimeout` and `WithPingInterval`
  (`engineio/options.go:208,213`) say "zero means" although every nonpositive value
  selects the default (`:93-104`); the README tagline omits the Go client.
- **Change:** fix the module paths and every reference to them; bump framework and
  `golang.org/x/net` versions; add the package prefix to the unprefixed error strings;
  "nonpositive" where the code means it; mention the Go client in the README tagline.
  Find further cases with
  `git grep -n 'errors.New("\|fmt.Errorf("' -- '*.go' ':!*_test.go' ':!v2'` and
  `git grep -n "zero means" -- '*.go' ':!v2'`, compare each with the code, and list what
  was changed in the PR. Keep `_examples/gf` on a gf
  release that selects redigo v2.0.0+incompatible, because it is the only build that guards
  the redigo compatibility noted in `adapter/redis/adapter.go:78-81`.
- **Tests:** `make examples`, `make examples-node`.
- **Benchmarks:** none.
- **Docs:** README, `_examples/README.md`.
- **Acceptance criteria:** `make examples` and `make vuln` pass; `grep -rn "deault\|_example/" _examples/*/go.mod` finds nothing; `_examples/gf/go.mod` still lists redigo v2.0.0+incompatible.
- **Definition of done:** standard.
- **Out of scope:** exported test fakes (D6).

### W35. Encoder and round-trip fuzz targets

- **Status:** open
- **Priority:** after release
- **Findings:** [T5](#tests-findings)
- **Depends on:** W3
- **Problem:** No fuzz target covers encoding or an encode/decode round trip. The decoder
  package `engineio/parser/internal/packet` has no tests of its own, but `engineio/parser`
  tests reach it (89.5% cross-package coverage), so that is not a material gap.
- **Change:** in `parser`, `FuzzDecodeArgs` (decode into a fixed set of typed targets) and
  `FuzzRoundTrip` (`Prepare` → `Decoder` → compare) for binary-free values, with seeds.
- **Tests:** the fuzz targets; seeds run under `go test`.
- **Benchmarks:** none.
- **Docs:** none.
- **Acceptance criteria:** `go test -run '^$' -fuzz FuzzRoundTrip -fuzztime 60s ./parser`
  and the same for `FuzzDecodeArgs` find no failure; `go test ./parser` passes.
- **Definition of done:** standard.
- **Out of scope:** CI fuzz campaigns.

### W36. Replace sleeps in pauser tests

- **Status:** open
- **Priority:** after release
- **Findings:** [T6](#tests-findings)
- **Depends on:** none
- **Problem:** Inherited pauser tests synchronize with sleeps
  (`engineio/transport/polling/pauser_test.go:44,124,128,206`), which weakens what they
  check.
- **Change:** replace the sleeps in `TestPauserTrigger` and
  `TestPauserOnlyOnePauseAfterWaiting` with readiness signals (a hook or channel signalled
  when the goroutine enters the blocking wait); keep the randomized delays of
  `TestPauserConcurrencyWorkingDone` only if they are the point of the test.
- **Tests:** the rewritten tests with `-race -count=50`.
- **Benchmarks:** none.
- **Docs:** none.
- **Acceptance criteria:** `go test -race -count=50 ./engineio/transport/polling` passes;
  `grep -n "time.Sleep(time.Second / 10)" engineio/transport/polling/pauser_test.go` finds
  nothing.
- **Definition of done:** standard.
- **Out of scope:** Pause semantics (W32).

### W37. CI job with GOEXPERIMENT=jsonv2

- **Status:** open
- **Priority:** after release
- **Findings:** [ARCH-10](#architecture-findings)
- **Depends on:** none
- **Problem:** `parser` has build variants for `GOEXPERIMENT=jsonv2`
  (`json_mode_v2.go` for go1.27, `json_mode_v2_pre127.go` for go1.25–1.26,
  `json_methods_v2.go`), but no CI job builds or tests them.
- **Change:** `.github/workflows/ci.yaml`: a job running
  `GOEXPERIMENT=jsonv2 go test -race -count=1 ./parser/... . ./client/...` on Go `stable`
  and `oldstable`, selected when v1 packages are; a `make test-jsonv2` target.
- **Tests:** the job. **Benchmarks:** none.
- **Docs:** CLAUDE.md "Commands" and "CI".
- **Acceptance criteria:** the job runs and passes on the PR; `go test ./.github/changes`
  passes.
- **Definition of done:** standard.
- **Out of scope:** making jsonv2 the default.

### W38. API compatibility gate

- **Status:** open
- **Priority:** after release (needs the v1 tag)
- **Findings:** [API-11](#api-compatibility-findings)
- **Depends on:** the release tag
- **Problem:** After the release, nothing in the Makefile or CI fails on an incompatible
  v1 API change.
- **Change:** a `make apidiff` target running `gorelease -base=<latest v1 tag>`
  (`golang.org/x/exp/cmd/gorelease` at a pinned pseudo-version) in the root module, failing
  when it reports incompatible changes; a CI step in `lint-v1` with full fetch depth.
- **Tests:** demonstrate the failure in the PR with a scratch commit that removes an
  exported function.
- **Benchmarks:** none.
- **Docs:** CLAUDE.md "Commands" and "CI"; CONTRIBUTING.md (what a v1 PR must keep).
- **Acceptance criteria:** the step passes on the PR and failed on the scratch commit.
- **Definition of done:** standard.
- **Out of scope:** v2.

### W39. Stress on push and an aggregate required check

- **Status:** open
- **Priority:** after release
- **Findings:** [REL-4](#release-engineering-findings), [REL-5](#release-engineering-findings)
- **Depends on:** none
- **Problem:** `stress-v1` runs only on `pull_request` (`ci.yaml:378`), so push and schedule
  runs never execute it, although the `ci.yaml` header comment says they run everything and
  CONTRIBUTING lists stress in "the full CI matrix". Branch protection requires only the
  shared-file drift and two conformance checks; a skipped matrix job reports one check
  under the unexpanded name (`ci.yaml:91-93`), so the Test v1 matrix cannot be required
  directly.
- **Change:** run `stress-v1` on push and schedule as well (drop the event condition) and
  raise its timeout to 30 minutes; add a `ci-ok` job that `needs` every job, runs with
  `if: always()`, and fails when any needed job failed or was cancelled. Owner action:
  require `ci-ok` in branch protection.
- **Tests:** the workflow on the PR; validate selection with `go test ./.github/changes`.
- **Benchmarks:** none.
- **Docs:** `ci.yaml` header comment; CLAUDE.md "CI".
- **Acceptance criteria:** `ci-ok` passes on the PR; the owner confirms with
  `gh api repos/sshaplygin/go-socket.io/branches/master/protection` that it is required.
- **Definition of done:** standard, plus the owner's branch protection change.
- **Out of scope:** the Benchmarks workflow.

### W40. Pin CI tools and move actions off Node 20

- **Status:** open
- **Priority:** after release
- **Findings:** [REL-9](#release-engineering-findings)
- **Depends on:** none
- **Problem:** golangci-lint is unpinned (`version: latest` at `ci.yaml:244` and `:298`,
  `@latest` at `:505`), and so is govulncheck (`@latest` at `:258` and `:311`). Every
  JavaScript action in use (`checkout@v4`, `setup-go@v5`, `setup-node@v4`,
  `golangci-lint-action@v8`, and the pinned upload/download-artifact v4 SHAs in
  `benchmarks.yml`) declares `using: node20`, which GitHub marks deprecated.
- **Change:** pin golangci-lint to the v2 release used locally and govulncheck to a fixed
  version; move each action to its newest major whose `action.yml` declares `node24`
  (read it at the chosen tag), keeping SHA pins where they exist.
- **Tests:** the workflows on the PR.
- **Benchmarks:** none.
- **Docs:** CLAUDE.md "Commands" (golangci-lint version).
- **Acceptance criteria:**
  `grep -nE "version: latest|@latest" .github/workflows/ci.yaml .github/workflows/benchmarks.yml`
  finds no golangci-lint or govulncheck entry (on master it finds `ci.yaml:244`, `:258`,
  `:298`, `:311` and `:505`); the PR run has no Node 20 deprecation annotation.
- **Definition of done:** standard.
- **Out of scope:** the `sshaplygin/benchmark-report` actions, which are maintained in that
  repository. `report/` (`benchmarks.yml:200`) is composite with no nested action; the root
  action (`benchmarks.yml:241,247`) is composite and wraps
  `marocchino/sticky-pull-request-comment` v3.0.5, whose `action.yml` declares `node24`.
  If a later pin of either action pulls in a `node20` action, the annotation it produces in
  the Benchmarks workflow (`compare` and `report` jobs) is excluded from the acceptance
  criterion.

### W41. Compile tests on 32-bit platforms

- **Status:** open
- **Priority:** after release
- **Findings:** [REL-8](#release-engineering-findings)
- **Depends on:** none
- **Problem:** `client/connection_typed_test.go:66` compares a decoded `event.Value[int]`
  with the untyped constant 9007199254740993, which overflows `int` on 386, so the client
  test package does not compile there; the request schema has the same limit.
- **Change:** use `int64` consistently in the request schema, the decoded value and the
  expected value; add `GOOS=linux GOARCH=386 go vet ./...` to the Makefile `vet` target.
- **Tests:** the existing test, now portable.
- **Benchmarks:** none.
- **Docs:** none.
- **Acceptance criteria:** `GOOS=linux GOARCH=386 go test -exec=true -run '^$' ./...`
  succeeds; `make vet` passes.
- **Definition of done:** standard.
- **Out of scope:** running tests on 32-bit hardware.

### W42. Fail the benchmark comparison on lost samples

- **Status:** open
- **Priority:** after release
- **Findings:** [REL-13](#release-engineering-findings)
- **Depends on:** none
- **Problem:** with Go 1.25.5, when a sub-benchmark's `b.N=1` probe fails in a later
  `-count` repetition, `go test` prints `--- FAIL` but still prints `PASS` and exits 0. A
  failure in a measured run exits 1. The Benchmarks workflow relies on the exit status
  (`pipefail`), so a lost sample only shrinks the sample count in the report.
- **Change:** in `.github/workflows/benchmarks.yml`, after each side's benchmark run, fail
  the step when the raw output contains a line starting with `--- FAIL`, and print the
  matching lines. Mention the guard in the CLAUDE.md "CI" paragraph on benchmarks.
- **Tests:** a scratch run of the changed step on a benchmark that calls `b.Fatal` when
  `b.N == 1` on its second repetition, quoted in the PR; `go test ./.github/changes` if the
  classifier is touched.
- **Benchmarks:** none.
- **Docs:** CLAUDE.md "CI".
- **Acceptance criteria:** the step fails on the scratch failing benchmark and passes on the
  PR's own run; the PR quotes both outputs.
- **Definition of done:** standard.
- **Out of scope:** changing benchmark thresholds.

### W43. Harden the W1 locking documentation and regression test

- **Status:** open
- **Priority:** after release
- **Findings:** [T7](#tests-findings), [CL-15](#clarity-findings)
- **Depends on:** none
- **Problem:** two notes from the review of #147 (W1). The locking-rule comment at
  `upgradeLocker` (`engineio/internal/session/session.go`) names only read-lock holders and
  failing writers, but write-lock holders are bound by the same rule, and
  `frameReader.Read`/`Close` also reach `fail` → `closeTransport` while the websocket read
  lock is held. `TestServerCloseShutsDownSessionsConcurrently`
  (`engineio/server_close_concurrent_test.go`) blocks one session's Error hook; when map
  iteration shuts that session down last, the old serial `Server.Close` passes too. A
  mutation run with the serial `Close` failed 39 of 40 iterations.
- **Change:** extend the comment to "Holders of upgradeLocker, read or write, must not call
  transport methods that can wait for an open frame reader or writer", with the matching
  change at `closeTransport`. In the test, make two sessions fail and close `entered`
  through `sync.Once`, so the escape rate against a serial `Close` drops to about 0.7%.
- **Tests:** the changed test, plus a mutation run (the serial `Close` from `6dd3b47` with
  the test from this PR) at `-count=40`, quoted in the PR.
- **Benchmarks:** none.
- **Docs:** code comments only.
- **Acceptance criteria:**
  `go test -race -count=20 -run TestServerCloseShutsDownSessionsConcurrently ./engineio`
  passes; the mutation run fails at least 39 of 40 iterations (quoted).
- **Definition of done:** standard.
- **Out of scope:** changing session locking.

### W44. Isolate the TestPollingBinary nil-reader crash

- **Status:** open
- **Priority:** after release
- **Findings:** [T8](#tests-findings)
- **Depends on:** none
- **Problem:** during a local `go test -race -count=5 -shuffle=on -cpu=1,4
  ./engineio/transport/...` run on darwin under ephemeral-port exhaustion (TIME_WAIT above
  16,000), `TestPollingBinary` crashed with a nil-pointer dereference on a nil reader at
  `engineio/transport/polling/polling_test.go:82` (`io.ReadAll(r)`). A rerun after the
  ports drained passed. The `NextReader` error is checked at line 78 with the non-fatal
  `should.Nil(err)`, so a returned error followed by a nil reader would crash at line 82;
  whether `NextReader` returned an error, or a nil reader with a nil error, is not
  established.
- **Change:** reproduce by forcing the dial or request to fail, for example with a
  `RoundTripper` that returns an error. If a library function returns a nil reader with a
  nil error, fix it and add a regression test; otherwise make the test stop on the error
  (`require.NoError` instead of `should.Nil` at line 78).
- **Tests:** the reproduction above, failing before the change.
- **Benchmarks:** none.
- **Docs:** none.
- **Acceptance criteria:** the reproduction passes with `-race -count=20`; the PR quotes the
  original crash and states which side was at fault.
- **Definition of done:** standard.
- **Out of scope:** the port exhaustion itself (W20).

## Release blockers

### B1. Permanent session deadlock

`C1`, confirmed as a blocker by three independent reproductions. Work item: [W1](#w1-fix-the-permanent-session-deadlock).

**Lock cycle.** Three paths in the Engine.IO session wait on each other's locks.

| Lock | Holder | Waiter |
| --- | --- | --- |
| `ws.writeLocker` | `serveWrite`, during an in-flight websocket write | `setDeadline` (`engineio/internal/session/session.go:489-501`), which calls `conn.SetWriteDeadline`; that setter takes the lock in `engineio/transport/websocket/connect.go:101` |
| `upgradeLocker`, read side | `setDeadline`, while it waits for `ws.writeLocker` | `closeMode` (`engineio/internal/session/close.go:46`), which requests the write side |
| `upgradeLocker`, read side (new reader) | — | `Session.fail` (`close.go:141`), called by `serveWrite` while it still holds `ws.writeLocker` |

`fail` cannot take the read lock because `closeMode` already has a write lock request
pending, and a pending writer on a Go `RWMutex` blocks new readers. None of the three paths
can make progress.

**Trigger.** A websocket client, including one upgraded from polling, stops reading but
keeps sending packets while the server emits. Pings and plain event messages both reproduce
it, because every inbound packet calls `setDeadline` (`session.go:198`). The 64-packet
outbound queue overflows, and the resulting `Abort` enters the cycle. A client that stops
sending as well as reading is disconnected normally: in that control run `OnDisconnect`
fired with reason `forced close` and `Server.Close` returned in 8 µs.

**Evidence.** Each reproduction logs `write buffer full`, then no `OnDisconnect` within
30 s, then `close timed out err="1 connections still open"`. `Server.Close` returns after
its 10 s bound. Every run shows the same four blocked stacks. At the Engine.IO level,
`Server.Close` was still blocked when the test timed out at 2m30s.

**Impact.**

- `OnDisconnect` never runs, and the socket never leaves its rooms.
- The connection's goroutines and its TCP connection leak permanently.
- `engineio.Server.Close` (`engineio/server.go:88-90`) shuts sessions down one at a time,
  so it never reaches the sessions after the stuck one, nor `dropUnaccepted`.

**Fix.**

1. In `setDeadline`, read `s.conn` under the read lock, release the lock, and only then
   call the deadline setters. `nextReader` and `nextWriterCompress` already work this way.
2. Make `Session.fail` independent of `upgradeLocker`, for example by keeping `closed` and
   `conn` in atomic snapshots.
3. Optional: store the websocket write deadline atomically and apply it inside
   `nextWriter`, so `SetWriteDeadline` never waits for an in-flight write.
4. Shut sessions down concurrently in `engineio.Server.Close`, or bound each shutdown in
   time.
5. Add a `-race` regression test in which a client that does not read but keeps sending
   packets receives large emits. Run it once with a client that sends only pings and once
   with a client that sends only messages, each over direct websocket and over the
   polling-to-websocket upgrade. It must assert that `OnDisconnect` fires and that `Abort`
   and `Server.Close` return promptly.

## Should fix before release

### M1. No migration guide or release notes

`API-1`, downgraded from blocker to major; `CL-4` major; `REL-1`. Work item: [W8](#w8-write-the-migration-guide).

**Where the guide went.** `b2c1cd2` deleted `CHANGELOG.md` (602 lines) and
`docs/V1_MIGRATION.md` (453 lines). CONTRIBUTING.md now states that v1 needs no changelog: the paragraph that starts "V1 has no
standalone changelog" and the closing paragraph of "Releases".

**Why it matters.** Every adopter of `v1.5.0` arrives from upstream, because the fork's own
`v0.9.1`–`v1.4.2` tags declare `module github.com/googollee/go-socket.io` and cannot be
used under this path. `apidiff` reports these incompatible changes:

| Baseline | Package | Incompatible changes |
| --- | --- | --- |
| `v1.4.2` | root | 24 |
| upstream `v1.7.0` | root | 20 |
| `v1.4.2` | `parser` | 4 |
| upstream `v1.7.0` | `parser` | 3 |
| upstream `v1.7.0` | `engineio` and subpackages | 8 in `engineio`; 17 including `transport` (5), `transport/polling` (2) and `transport/websocket` (2); 5 public packages removed (`frame`, `packet`, `payload`, `session`, `transport/utils`) |

**Changes that compile but change meaning:**

- A non-root `Conn.ID()` now returns `<namespace>#<engine ID>` (`socket.go:257-263`). The public godoc
  in `types.go:71` does not say so.
- `BroadcastToRoom(room, event, arg)`, written in the `v1.4.2` style, still compiles
  when `arg` is a string. It is interpreted as `(namespace, room, event)`, dropping
  the intended delivery or targeting a different namespace/room (`API-8`).
- Parser `Encode(h, args)` with nil `args` now writes a `null` payload (`1/a` becomes
  `1/a,null`) (`API-8`).

**Fix.** Add a "Migrating from googollee/go-socket.io" section to [V1.md](V1.md), and
publish the same text as the GitHub release notes. It should cover:

- the import path;
- an old-to-new identifier table, for example `NewServer(*engineio.Options)` →
  `NewServer(...Option)` and `Server.Adapter(*RedisAdapterOptions)` →
  `WithAdapter(redis.New(...))`;
- a changed-defaults table, see the table below;
- the changes that compile but change meaning, listed above;
- the Redis upgrade procedure from [M3](#m3-redis-adapter-is-wire-incompatible-with-upstream-go-peers);
- the Go 1.22 floor (`API-13`).

Also state the ID format in the `Conn.ID` godoc of both roles.

| Changed default | Covered by |
| --- | --- |
| CORS: every origin accepted, see M2 | [M2](#m2-permissive-cors-default-is-not-in-the-user-docs) |
| Cookie: upstream sets none; the polling handshake now sets an `io` cookie (`Path=/`, `HttpOnly`, `SameSite=Strict`) holding the session ID (`engineio/options.go:160-171`); opt out with `engineio.WithCookieEnabled(false)`, change it with `WithCookie` | migration notes |
| Compression: upstream compresses nothing; polling responses are now compressed from 1,024 bytes (`WithHTTPCompression`, `WithCompressionThreshold`); websocket permessage-deflate stays off (`WithPerMessageDeflate`) | migration notes |
| Ping interval: 20 s → 25 s; timeout: 60 s → 20 s. The server idle deadline was PingTimeout alone (60 s) upstream and is now PingInterval + PingTimeout (45 s), so an explicitly configured PingTimeout now adds to the interval. Clients (JS, Go) already allowed interval + timeout, so their detection window shrinks from 80 s to 45 s. | migration notes |
| Message size limit: upstream none; now 1e6 bytes per polling POST body or websocket message (`WithMaxHTTPBufferSize`) | migration notes |
| Disconnect reasons: upstream `v1.7.0` passes `client namespace disconnect` for every connection close and the DISCONNECT packet's argument (normally empty) for a namespace disconnect; `OnDisconnect` now receives the cause (`client namespace disconnect`, `server namespace disconnect`, `transport close`, `ping timeout`, `transport error`, `forced close`) | migration notes |
| JSONP: removed | migration notes |

The "upstream" values in this table were read from the `v1.7.0` and go-engine.io `v1.4.1`
sources (no cookie, compression or size-limit code in either); W8 verifies them before
quoting.

This guide documents the current contract plus a one-time migration, so it does not
conflict with the no-changelog policy.

### M2. Permissive CORS default is not in the user docs

`API-3` confirmed major; duplicates `CL-9`, `C5` and `interop-9`. Work item: [W6](#w6-document-the-cors-default).

**Behavior.** `engineio/options.go:174-181` accepts every origin by default, and
`getTransport` (`:121-151`) applies that check to both default transports:

- Polling reflects the request `Origin` and sets `Access-Control-Allow-Credentials: true`.
- Websocket skips the origin check entirely.

**Probe.** A default server answered a request with `Origin: https://evil.example` as
follows:

| Server | Polling | Websocket |
| --- | --- | --- |
| Default | `Access-Control-Allow-Origin: https://evil.example`, credentials allowed | `101` |
| `WithCORS(false)` | no CORS headers; handshake still served (`200`) | `403` |

For comparison, upstream `v1.7.0` embeds its own Engine.IO and keeps Gorilla's
same-origin websocket check, so a foreign-origin websocket gets `403` there by default.
Its polling responses carry no CORS headers by default, but the request is still served.
CORS headers control what a browser may read; they do not reject the HTTP request.

**Documentation.** The default matches socket.io 2.x and was a deliberate decision. It is
documented only in godoc: README, V1.md and PROTOCOL.md never mention it. Applications that
authenticate with cookies are exposed to cross-site WebSocket hijacking without knowing it.

**Fix.**

- Add a CORS section to [PROTOCOL.md](PROTOCOL.md) covering the default, preflight handling,
  `WithCORS(false)` and `CheckOrigin`.
- Add a security note to V1.md and to the migration notes.
- Remove the allow-all `CheckOrigin` from `_examples/default-http/main.go:19-43`, or replace
  it with an origin allow-list.

### M3. Redis adapter is wire-incompatible with upstream Go peers

`API-4`, confirmed major. Work item: [W7](#w7-document-the-redis-wire-break-and-upgrade-procedure).

**What changed.** The default prefix is still `socket.io` on both sides, but the channel
names and the payload codec are different:

| Item | Upstream | Fork |
| --- | --- | --- |
| Subscription pattern | `%s#%s#*` | `%s#*` |
| Key | `%s#%s#%s` | `%s#%s#` |
| Request channel | `%s-request#%s` | `%s-request#%s#` |
| Codec | JSON | MessagePack (`codec.go:28-33`) |

The patterns are in `adapter/redis/adapter.go:121-124`.

**Effect in a mixed fleet.** For an identically named non-root namespace, the old
and new patterns overlap. The original real-Redis check used the root-shaped pattern
`socket.io#/#*`, which upstream never subscribes to; a live fleet of two upstream `v1.7.0`
instances and one fork instance on one Redis server later confirmed the non-root overlap:

- A new instance discards decode errors (`subscription.go:208`), so it simply misses old
  traffic.
- An old instance's `dispatch()` returns on the first decode error. A fork
  namespace-wide message passes its namespace filter and stops that dispatcher.
  Single-room channels fail the old namespace filter and are ignored, unless the room
  name, or its last `#`-separated segment, equals the namespace name; such a room
  broadcast also stops the dispatcher. The stopped dispatcher also handles requests and
  responses, so that peer stops receiving broadcasts and answering cluster requests, and
  upstream `RoomLen`/`Rooms` in that namespace then block without a timeout.

The default root namespaces differ too: upstream uses an empty namespace in its
channel names, while this adapter normalizes it to `/`. Root traffic is therefore
also incompatible, but it does not stop upstream dispatchers: fork root channels
(`socket.io#/#…`) do not match the upstream root pattern (`socket.io##*`), so upstream
peers never receive them, and the fork discards the upstream root messages it cannot
decode, as above. Root-namespace broadcasts are lost in both directions.

**Fix (documentation only).** Give the new fleet a distinct `WithPrefix`, or upgrade every
instance at once. Put this procedure in PROTOCOL.md and in the migration notes. The deleted
changelog carried this warning.

### M4. Redis `Send` and `SendAll` drop broadcasts silently

`ARCH-1` confirmed major; `CL-2` (downgraded to minor) resolved as major. Work item: [W5](#w5-report-redis-broadcast-failures).

**Behavior.** `adapter/redis/adapter.go:154-161` discards the `SendTo` error. As a result:

- With a trailing callback, nothing is delivered, not even to local sockets.
- When Redis is down, cross-node delivery is lost and nothing is logged.
- `BroadcastToRoom` and `BroadcastToNamespace` still return `true`.

**Contract violations.**

- The logger contract says WARN "marks a failure no caller receives"
  (`logger/logger.go:31-32`). `Clear` already follows it, but `Send` and `SendAll` do not.
- Godoc promises per-socket callbacks in `server.go:275-290`, `errors.go:21-22`,
  `adapter/adapter.go:61-62` and the Redis `Send` comments.

**Why major despite the downgrade.** The `CL-2` re-check downgraded the finding because
V1.md:125-126 mentions that callbacks are rejected. It did not consider the silent loss
when Redis is down.

**Fix.**

- Log WARN in `Send` and `SendAll` when `SendTo` fails.
- Correct the godoc: the boolean result reports only that the namespace is registered and
  its adapter was constructed, not that anything was delivered or that the adapter is
  still open.

### M5. Redis `PUBLISH` has no I/O deadline

`PERF-3`, confirmed major. Work item: [W4](#w4-bound-redis-broadcast-publication).

**Behavior.** The pool's dial options (`adapter/redis/adapter.go:70-104`) set no read or
write timeout. `codec.go:191-198` calls `c.Do` synchronously on the caller's goroutine.

**Reproduction.** With `redis-server` stopped by `SIGSTOP`:

- `SendTo` was still blocked after 15 s.
- Closing the adapter did not release it.
- It returned only after `SIGCONT`, at 20.0 s.

There is no application-level bound on that wait. The dialer's five-minute TCP
keepalive setting is not an operation timeout: a stopped Redis process can still
have its TCP traffic acknowledged by the kernel. A handler blocked in publication
also blocks its connection's dispatcher.

`WithRequestsTimeout` bounds only cluster requests, whose commands go through `boundedDo`
(`requests.go:94,120,470`); broadcast `PUBLISH` (`adapter.go:195` → `codec.go:191`)
ignores it. The godocs promise more: `options.go:123` says the option bounds "Redis
commands", and `cluster.go:23` says "All calls use the earlier of ctx and
WithRequestsTimeout", which includes the embedded `SendTo`.

**Fix.** Bound broadcast publication by the requests timeout without `redis.DoContext`,
which is missing from redigo v2.0.0+incompatible (selected by gogf/gf v1 releases before
v1.16.6, see `adapter.go:78-81`). Use `redis.DoWithTimeout` for the read plus
`DialWriteTimeout` on the pooled publishing connections only, or close the connection on
deadline as `boundedDo` does. Do not apply a read timeout to the shared Pub/Sub
subscriber, which uses the same dialer. Until publication is bounded, state in both
godocs that broadcasts are not covered.

### M6. JSON encoder from #143 regresses encode cost

`PERF-1`, confirmed major. Work items: [W2](#w2-add-socketio-level-benchmarks) (benchmarks), then [W3](#w3-restore-encoder-performance).

**Cause.**

- `parser/json_encode.go:109-182` calls `json.Marshal` once for every leaf value and every
  map key, and sorts the entries.
- `parser/encoder.go:138` allocates a map for every packet.

**Measurements.** Every figure below except the `v1.4.2` values is an ad hoc measurement
(no committed benchmark yet); the harnesses were not retained in the repository.
[W2](#w2-add-socketio-level-benchmarks) adds the committed benchmarks that must capture this
regression ([Performance measurements](#performance-measurements) names the one for each
row), and [W3](#w3-restore-encoder-performance) records the before/after.

| Benchmark | Baseline | Result |
| --- | --- | --- |
| Encode a slice of 100 structs: allocations | 1,004 (`v1.4.2`), 1,012 (previous master) | 2,518 |
| Encode a slice of 100 structs: time | about 65 µs (`v1.4.2`), about 140 µs (previous master) | about 205 µs: about 1.5× the previous master and about 3× `v1.4.2` |
| Encode, independent re-check | 13 allocations, about 85 µs | 1,419 allocations, about 150 µs |
| Broadcast 100 structs to 10 websocket clients, end to end | 1.535 ms (previous master) | 2.279 ms: +48.5%, p = 0.000; allocations +145% |
| Encode a small single struct | previous master | about 8% slower |

Rows use different payloads, so allocation counts compare only within a row: the codec rows
encode structs with string, int64, bool, `[]string` and `map[string]string` fields; the
independent re-check uses structs without the map. The previous master was already about
2× slower than `v1.4.2`; that part predates #143. Reruns reproduced the allocation counts
exactly; the end-to-end time increase ranged from +48% to +57% across reruns (see
[Follow-up validation](#follow-up-validation)). Each `v1.4.2` value, including "about 3×
`v1.4.2`", is a historical ad hoc measurement; not reproducible by a committed benchmark,
because the benchmarks use API that `v1.4.2` lacks (see
[W25](#w25-remove-extra-json-passes-from-decoding)). These values are context, not
performance claims.

**Fix.**

- Add a cached fast path per `reflect.Type`: when a subtree contains no `Buffer`, no
  interface and no custom marshaler, encode it with a single `json.Marshal`.
- Write scalar values with append-style writers.
- Create the cycle-detection map lazily.
- Commit parser encode benchmarks (`PERF-10`) before changing the encoder, so the CI
  base-vs-PR benchmark comparison shows the before/after; the fix is done only when it
  does. See [W3](#w3-restore-encoder-performance).

If the fix is deferred, state the cost measured by W2's committed benchmarks (see
[How to use this plan](#how-to-use-this-plan)) in the release notes.

### M7. No inline quick start; docs do not state that `Serve` must run

`CL-1`, confirmed major. Work item: [W9](#w9-add-a-lifecycle-sketch-and-runnable-examples).

**Gap.** The `v1.4.2` README had an inline quick start at lines 33-82. The current
README's only code block is `go get`, but it links directly to runnable server and
Go client examples. V1.md never mounts the handler or calls `Serve`, `Close` or
`Connect`, and the repository has no `Example` functions. The missing inline
quick start is a documentation improvement, not an absence of runnable examples.

**Failure mode.** A user who forgets `go server.Serve()` gets no error: `Connect` returns
nil, but `OnConnect` never fires and acknowledgements never arrive.

**Fix.**

- Add a short server and client lifecycle sketch to the root package doc or docs/V1.md:
  `NewServer`, `OnConnect`/`OnEvent`, `http.Handle("/socket.io/", server)`,
  `go server.Serve()`, `defer server.Close()`, and `client.New`/`Connect`/`Emit` with an
  ACK callback. The README keeps linking to runnable examples, as decided in `b557f2a`.
- Add runnable `Example` functions.
- State in the `Server` and `Serve` godoc that connections are not dispatched, and no
  error is reported, while `Serve` is not running.

## Decisions required before tagging

`v1.5.0` freezes these choices for the life of v1. They are design choices, not confirmed
defects: keeping the current behavior is a valid outcome, but each choice has to be settled
before the tag.

**Version number** (`API-2`, `REL-7`; [D1](#d1-version-number-and-inherited-tags))

- `v1.5.0` reuses an upstream release number and is lower than upstream `v1.7.0`, whose
  history this module contains.
- CONTRIBUTING.md already selects `v1.5.0`, and the module proxy and checksum database have
  no record of the earlier deleted tag. If it stays, say in the release notes that the fork
  does not continue upstream numbering. A number above upstream, such as `v1.9.0`, would
  require changing CONTRIBUTING and the README links first.

**Inherited tags** (`CL-10`, `REL-6`; [D1](#d1-version-number-and-inherited-tags), applied by [W10](#w10-prepare-the-release-commit))

- Today `go get github.com/sshaplygin/go-socket.io@latest` fails with a module path mismatch.
- Options: retract `[v0.9.1, v1.4.2]` in the release commit (recommended, D1 option R1),
  or no retraction (R2). Those tags declare the upstream module path.

**Aliases to internal packages** (`ARCH-2`, `API-9`; [D2](#d2-aliases-to-internal-packages))

- 24 exported type aliases to internal packages, among them `engineio.Conn`, `CloseError`,
  `RemoteError` and `EventHandler`, hide their methods and fields from `go doc`.
- This contradicts the rule in `internal/parity/api_docs_test.go:15-16`.

**Typed events** (`ARCH-3`, `CL-11`; [D3](#d3-typed-events-and-client-bridge-methods))

- Typed client events live in the server package, so a typed Go client must import the
  server.
- `client.RegisterEvent`, `RequestAck` and `SendEvent` expose types from `internal/event`.

**Adapter contract** (`ARCH-5`, `CL-7`; [D4](#d4-adapter-contract); the `AllRooms` comment is fixed in [W28](#w28-adapter-godoc-and-redis-log-keys))

- `Send` and `SendAll` duplicate `SendTo`, and `Rooms(nil)` overloads `AllRooms`.
- The optional hooks `io.Closer` and `BindSocketLookup` are detected by duck typing.
- The `AllRooms` comment contradicts the implementation.

**Engine connection access** (`ARCH-6`; [D5](#d5-engine-connection-access))

- `Conn.EngineConn()` exposes `NextReader` and `NextWriter`, which the library itself owns.

**Exported test helpers** (`ARCH-11`, `API-12`; [D6](#d6-exported-test-helpers))

- `NewFakeConn*`, `NewFakeConstReader`, `FakeDiscardWriter` and `TimestampFromClock` are
  exported.
- Two doc comments refer to a nonexistent `base.Conn` (`engineio/transport/manager.go:8`,
  `engineio/transport/websocket/connect.go:18`).

## Backlog

Each item has an ID in the [finding catalogue](#finding-catalogue) and a work item in the
[work plan](#work-plan), except `CL-14` (not planned).

### Correctness and robustness

- **Dynamic namespaces are never removed** (`C2` downgraded to minor, `ARCH-9`).
  - Namespaces created through `OfFunc` stay in the registry for the server's lifetime,
    even when every connection to them is rejected. The original review's heap
    measurement was ad hoc and is not repeated here, because no work item commits a
    registry-memory benchmark ([W14](#w14-document-dynamic-namespace-retention) documents
    the retention only).
  - socket.io 2.x behaves the same way. Document the retention in the `OfFunc` godoc and
    V1.md, and consider a cleanup option later.
- **Duplicate session IDs** (`C3`). A duplicate SID from a custom `IDGenerator` replaces a
  live session, and `Server.Close` then leaves the first session open.
- **`OnError` contract** (`CL-3` downgraded to minor, `C4`).
  - `OnError` receives a nil `Conn` for root rejections. Neither `OnError` method documents
    this, and V1.md:41 says every such error receives an inactive connection context, which
    is too broad.
  - A panic inside `OnError` is recovered without any log (`namespace_handlers.go:223`).
- **Frame reader not closed on errors** (`T2`). After a parse error or a mid-frame failure,
  the websocket frame reader stays open, together with its goroutine and a 30 s timer.
- **Polling timers** (`PERF-2` downgraded to minor). The polling transport creates a
  `time.After` timer for every packet. With pre-1.23 timer semantics, which the
  application's main module go line, a pre-1.23 toolchain or `asynctimerchan=1` selects,
  each one keeps about 1 KiB alive for about 45 s. This is an ad hoc measurement (no
  committed benchmark yet; see [W18](#w18-stop-per-packet-polling-timers), whose
  `TestPollingTimersReleasedUnderAsyncTimerChan` asserts the retention bound; W2's
  `BenchmarkPayloadRoundTrip` captures `B/op` only). go-engine.io `v1.4.1` had the same code, so this is not a
  regression.

### Interoperability

- **Base64 over websocket** (`interop-1` downgraded to minor). The websocket transport
  ignores `b64`, so a `forceBase64` client loses its session when it sends binary data.
  Document this in PROTOCOL.md before the release.
- **Undocumented wire differences:**
  - every JSON payload and the OPEN packet end with `\n` (`interop-2`);
  - polling `POST` accepts fewer `Content-Type` values than the reference server
    (`interop-3`);
  - ERROR payloads are always strings (`interop-4`);
  - an untyped handler whose argument type does not match sends no ACK, so the JS callback
    never fires (`interop-5`);
  - an unknown packet type closes the connection (`interop-9`);
  - no close frame is sent when the server closes a websocket (`interop-6`).

### Documentation

- 9 of 12 packages have no package doc, and lint disables `ST1000` and `ST1020`–`ST1022`
  (`CL-5`).
- Several server APIs overlap but differ in ACK semantics (`CL-8`).
- Close, drain and upgrade have no written state-machine description (`CL-12`).
- ARCHITECTURE.md undercounts the per-connection workers (`ARCH-13`, `PERF-13`). The
  measured count is 7 goroutines per idle websocket connection, 6 of them started by the
  library; this is an ad hoc measurement (no committed benchmark yet; see
  [W26](#w26-reduce-the-idle-connection-footprint)).
- Registering a handler can block for 10 s when Redis is unreachable (`T4`).
- PARITY.md attributes the Engine.IO heartbeat, together with handshake and upgrade, to
  `engineio`/session tests and raw conformance tests collectively; only the session tests
  exercise it, and no conformance scenario runs a periodic ping/pong with a JS client (the
  upgrade probe is the only ping sent) (`T3`, `interop-7`).
- Redis log records use the key `namespace` instead of `nsp` (`ARCH-12`).
- `redis.Cluster` methods have unnamed parameters (`CL-6`).
- Optional: build and test commands are in CLAUDE.md, which the ownership table assigns
  them to; CONTRIBUTING.md and README link there. Moving them to CONTRIBUTING.md is an
  editorial choice, not a gap (`CL-14`).

### Performance

Each figure in this subsection is an ad hoc measurement (no committed benchmark yet). The work
item of each finding names the committed benchmark that captures it (W2 adds them) and
records its before/after. The `v1.4.2` decode baseline (`PERF-11`) is a historical ad hoc
measurement; not reproducible by a committed benchmark, because no planned benchmark can
run at `v1.4.2`; it is context only, not a performance claim (see
[W25](#w25-remove-extra-json-passes-from-decoding)).

- The Redis pool has `MaxIdle` 4 and no `MaxActive` limit: it opened 2,259–2,556 TCP
  connections for 12,800 publishes across runs (`PERF-4`).
- The Go polling client shares `http.DefaultTransport`, which keeps two idle connections
  per host. With ten clients in one process, 1,000 broadcasts opened about 3,000 new TCP
  connections (none with one client), and sustained broadcasts on darwin localhost
  exhausted ephemeral ports within about 9–13 s (`PERF-5`).
- A broadcast encodes the packet once per recipient (`PERF-6`).
- `WithHeader` copies the packet even when no ACK ID was added (`PERF-7`).
- The websocket wrapper starts a goroutine and a 30 s timer for every frame (`PERF-8`).
- `Server.JoinRoom` scans every socket in the namespace (`PERF-9`).
- Decoding is 37–42% slower than in `v1.4.2` across runs, because of the extra JSON passes.
  It is unchanged from the previous master, so it is not a regression from #143
  (`PERF-11`).
- A single-room broadcast still builds a dedup map (`PERF-12`).

### Tests and CI

- `omitempty` handling (`isJSONEmpty`) has no test coverage (`T1`).
- No test fails when goroutines leak (`T2`).
- No fuzz target covers encoding or an encode/decode round trip (`T5`).
- Some tests still synchronize with sleeps (`T6`).
- No benchmark covers the Socket.IO level (`PERF-10`).
- No CI job builds with `GOEXPERIMENT=jsonv2` (`ARCH-10`).
- No `apidiff` or `gorelease` gate protects v1 after the release (`API-11`).
- The stress job never runs on push or on schedule, although `ci.yaml:17` and CLAUDE.md say
  it does (`REL-4`).
- Branch protection requires only three checks (`REL-5`).
- The test packages do not compile for 386 (`REL-8`).
- golangci-lint and govulncheck are unpinned (`latest`), and every JavaScript action in use
  targets the deprecated Node 20 runtime (`REL-9`).

### Release hygiene

- The README advertises `@v1.5.0` before the tag exists, and the `engineio` pkg.go.dev link
  is missing (`CL-10`, `REL-6`, `API-10`).
- A previously deleted `v1.5.0` is still in the maintainer's local module cache, so the
  consumption check must run with a fresh module cache (see
  [W10](#w10-prepare-the-release-commit)) (`REL-2` downgraded to minor).
- The repository-local git identity is a placeholder (`Probe <probe@example.invalid>`). It
  has already appeared in `Co-authored-by` trailers on two master commits (`ea920ae`, `3dd094d`), and it would become the
  tagger (`REL-3`).
- Example modules pin an old `golang.org/x/net` and contain module path typos such as
  `deault-http` (`REL-10`, `CL-13`).
- 130 remote branches remain, including the superseded `release/v1.5.0` (`REL-11`).
- An open TODO remains at `engineio/transport/websocket/connect.go:99` (`REL-12`).

### Maintainability

- **Unused write path** (`ARCH-4`). The `writeChan` delivery path is never used at runtime
  (only its capacity feeds the queue budget), but its buffer costs about 6 KiB per
  connection (88 B × 64 = 5,632 B, allocated as 6,144 B). This is an ad hoc measurement
  (no committed benchmark yet; see [W26](#w26-reduce-the-idle-connection-footprint)).
- **Duplicated runtimes** (`ARCH-7`). The root and client runtimes contain 15 clone groups.
- **Complexity hotspots** (`ARCH-8`):
  - `probeTransport` has cyclomatic complexity 45 and is 219 lines long; when a probe fails
    after the old transport's pause has started, it calls `Resume` every 1 ms until the
    pause goroutine returns, because a `Resume` issued before that goroutine enters
    `Pause` does not cancel it;
  - the connection close condition is a compound expression rather than an explicit state.

## Downgraded findings

No finding was fully refuted by the original re-checks. The re-checks and the final pass
changed the severities below. The follow-up later corrected several subclaims and found
CL-14's ownership premise refuted; see [Catalogue cross-check](#catalogue-cross-check).

| Finding | Change | Reason |
| --- | --- | --- |
| `API-1` | blocker → major | The no-changelog policy is deliberate, the module path is new, and the compile errors are loud. Merged into M1. |
| `REL-1` | major → minor | Re-checked as minor. The merged finding M1 stays major because of the silent default and wire changes. |
| `CL-2` | major → minor | V1.md:125 documents that callbacks are rejected. Raised back to major in M4 because of the silent loss when Redis is down. |
| `CL-3` | major → minor | The `Conn.Close` and `ErrWriteBufferFull` godoc mention the nil `Conn`, `recover` prevents a crash, and no example dereferences it. |
| `API-2` | major → minor | The number is valid for a new module path, and `@latest` resolves once the tag exists. |
| `API-5` | major → minor | Most defaults are documented in PROTOCOL.md, and the constructor change prevents silent compilation. The `Conn.ID` gap is part of M1. |
| `REL-2` | major → minor | The module proxy and sumdb never saw the deleted tag, so the risk is limited to one local cache. |
| `PERF-2` | major → minor | go-engine.io `v1.4.1` has identical code. The effect is transient and applies only to pre-1.23 timer semantics. |
| `C2` | major → minor | Matches socket.io 2.x. A match function and root authentication can mitigate it, and `OfFunc` is a new opt-in API. |
| `interop-1` | major → minor | Requires an opt-in `forceBase64` client sending binary data, and is not a regression. |

The re-checks confirmed `CL-1`, `CL-4`, `ARCH-1`, `API-3`, `API-4`, `PERF-1` and `PERF-3` as
major, and `C1` as a blocker.

## Validation results

All runs were on darwin/arm64 with Go 1.25.5 unless noted. Figures in this section are
from the original runs on `7311fbe`, except where a row cites the follow-up;
[Follow-up validation](#follow-up-validation) records which were reproduced.

### Tests and coverage

| Command | Result |
| --- | --- |
| `make test` | 16 packages ok, 19.5 s |
| `make test-race` | all packages ok, 35.4 s |
| `make test-stress` (race, `-count=5`, `-shuffle`, `-cpu=1,4`) | ok in two separate runs, 3m09s–3m17s |
| `go test -race -count=4 -shuffle=on` over root, `client/...`, `engineio/...`, `adapter/...` | ok |
| `go test -json` over 13 of the 16 tested packages (omits `internal/event`, `internal/parity`, `logger`) | 601 top-level tests including 3 fuzz seed targets, 0 failed; slowest `TestRedisServerCloseStopsBroadcasts` at 11.32 s |
| `make cover-check` | 93.6% (floor 91.7%), 0 uncovered exported functions; repeated runs give 93.5% or 93.6% |

Statement coverage of each package's own code:

| Package | Coverage |
| --- | --- |
| root | 95.1% |
| `client` | 92.5% |
| `engineio` | 92.9% |
| `adapter/redis` | 91.9% |
| `parser` | 88.0% |

### Fuzzing

No crashers were found.

| Target | Executions |
| --- | --- |
| `FuzzDecoderDiscardPacket` | 883,983 |
| `FuzzMalformedPollingPayload` | 4,208,188 |
| `FuzzRedisBroadcastDecoder` | 1,803,566 |
| Additional Socket.IO decoder probe | about 5.6 million |
| Additional payload decoder probe | 2.41 million |

### Interoperability runs

| Check | Result |
| --- | --- |
| `make conformance`, `socket.io-client` 1.7.4 | 62 pass / 0 fail, 48.5 s |
| `make conformance`, `socket.io-client` 2.5.0 | 62 pass / 0 fail, 47.5 s; 49.2 s on rerun |
| `make examples-node` | 7/7 per client |
| Heartbeat soak, 200 ms interval | 14 pongs, 0 disconnects, 6/6 runs |
| `forceBase64`, reference server | 12/12 |
| `forceBase64`, Go server, polling | 4/4 |
| `forceBase64`, Go server, websocket and upgrade | 0/8 |

### Lint, gates and vulnerabilities

| Check | Result |
| --- | --- |
| `make lint` | 0 issues |
| `make vet`, `make gates`, `make sync-check`, `make examples` | pass; 15 example modules |
| `govulncheck`, local Go 1.25.5 | 26 called standard library vulnerabilities (toolchain only), 0 third-party |
| `govulncheck`, CI Go 1.27.3 | none |
| Go 1.22.12 build and vet | ok |
| Go 1.21.13 build | fails on `reflect.TypeFor`, consistent with the 1.22 floor |
| `GOARCH=386` vet | fails with a test constant overflow (`REL-8`) |

### API

| Check | Result |
| --- | --- |
| `apidiff`, `v1.4.2` → `7311fbe`, root | 24 incompatible, 37 compatible |
| `apidiff`, upstream `v1.7.0` → `7311fbe`, root | 20 incompatible, 33 compatible |
| `apidiff`, `v1.4.2` → `7311fbe`, `parser` | 4 incompatible, 11 compatible |
| `apidiff`, upstream `v1.7.0` → `7311fbe`, `parser` | 3 incompatible, 7 compatible |
| `apidiff`, upstream `v1.7.0` → `7311fbe`, `engineio` package | 8 incompatible, 36 compatible; 17 incompatible with `transport` (5), `transport/polling` (2) and `transport/websocket` (2); 5 public packages removed (`frame`, `packet`, `payload`, `session`, `transport/utils`) |
| `go get ...@latest` | fails: module path mismatch |
| `go get ...@v1.5.0` | fails: unknown revision |

### Performance measurements

Every figure in this table is an ad hoc measurement (no committed benchmark yet), except
the `v1.4.2` values; the harnesses were not retained in the repository. The Captured by
column names the item, and the committed benchmark or test, that captures each figure. A
`v1.4.2` value is a historical ad hoc measurement; not reproducible by a committed
benchmark, because the benchmarks use API that `v1.4.2` lacks (see
[W25](#w25-remove-extra-json-passes-from-decoding)); it is context only, not a performance
claim. The last row is a behavioral result, not a performance figure.

| Measurement | Result | Captured by |
| --- | --- | --- |
| Encode a slice of 100 structs, allocations (`v1.4.2` / previous master / `7311fbe`) | 1,004 / 1,012 / 2,518 | previous master and `7311fbe`: W2 `BenchmarkEncode/structs100` (same payload; absolute values may differ from the ad hoc harness); W3 records the before/after against the PR base and `3dd094d`. `v1.4.2`: none (historical ad hoc measurement; not reproducible by a committed benchmark) |
| Encode a slice of 100 structs, time (`v1.4.2` / previous master / `7311fbe`) | about 65 / 140 / 205 µs | previous master and `7311fbe`: W2 `BenchmarkEncode/structs100` (same payload; absolute values may differ from the ad hoc harness); W3 records the before/after against the PR base and `3dd094d`. `v1.4.2`: none (historical ad hoc measurement; not reproducible by a committed benchmark) |
| Encode 100 structs with a `[]string` field and no map (independent re-check) | 13 → 1,419 allocations (follow-up: 12–13 → 1,418–1,419); about 85 → 150 µs | W2 `BenchmarkEncode/structsNoMap100` (absolute values may differ from the ad hoc harness); W3 records the before/after |
| Encode 100 scalar structs (follow-up) | 51,570 → 74,017 ns/op, about 6,089 → 29,541 B/op, 12 → 817 allocs/op | W2 `BenchmarkEncode/scalars100`; W3 records the before/after |
| Encode one struct | about 8% slower than the previous master | W2 `BenchmarkEncode/struct` (the original payload may differ); W3 records the before/after |
| Broadcast 100 structs to 10 clients, end to end | +48.5% (reruns: +48% to +57%); allocations +145% | W2 `BenchmarkBroadcastWebsocket/structs100-10`; W3 records the before/after |
| Decode | +42% vs `v1.4.2` (follow-up reruns: +37% to +42%), unchanged vs previous master | W2 `BenchmarkDecode`, PR-base comparison only (W25). `v1.4.2`: none (historical ad hoc measurement; not reproducible by a committed benchmark) |
| Polling heap over 200,000 packets, Go 1.22 timer semantics | 3.8 → 217.9 MiB; 4.4 MiB with `asynctimerchan=0` | W18 `TestPollingTimersReleasedUnderAsyncTimerChan` asserts the retention bound; W2 `BenchmarkPayloadRoundTrip` captures `B/op` |
| Polling heap over 50,000 packets, `asynctimerchan=1` (follow-up) | 1.0 → 42.8 MiB | W18 `TestPollingTimersReleasedUnderAsyncTimerChan`; W2 `BenchmarkPayloadRoundTrip` |
| Goroutines per idle websocket connection | 7.00 (`v1.4.2`: 4) | W26 `TestIdleWebsocketConnectionGoroutines` asserts the count; W2 `BenchmarkConnectWebsocket` captures `B/op` and `allocs/op`. `v1.4.2` count: none (historical ad hoc measurement; not reproducible by a committed benchmark) |
| `writeChan` buffer per connection | 88 B × 64 = 5,632 B, allocated as 6,144 B | W2 `BenchmarkConnectWebsocket` `B/op` (W26) |
| Redis publishes, 64 goroutines × 200 `SendAll` (12,800 publishes) | 2,259 new connections (follow-up: 2,350–2,556 in three runs) | W2 `BenchmarkRedisSendToParallel` (`conns/op` with `SOCKETIO_BENCH_METRICS=1`); W19 `TestRedisPublishPoolReusesConnections` asserts the bound at 16 goroutines, not this 64-goroutine workload |
| Go polling clients, 10 clients × 1,000 broadcasts | about 3,000 new connections (follow-up: 2,791–3,418); darwin port exhaustion within about 9–13 s of sustained broadcasts | W20 `TestPollingClientsReuseConnections`; W2 `BenchmarkPollingClientsBroadcast/clients-10` (`dials/op` with `SOCKETIO_BENCH_METRICS=1`) |
| One non-reading client and 10 reading clients over 20,000 broadcasts of 4 KiB | fast clients received 200,000/200,000, the slow client was evicted | none (behavioral result only) |

## Strengths

- **Architecture is enforced, not just described.**
  - ARCHITECTURE.md matches `go list` edge for edge, and `.github/architecture` checks it.
  - Codecs import only `logger`, and `client` never imports the server.
  - `staticcheck -checks U1000` and `deadcode -test` report nothing.
- **Tests check behavior.**
  - 613 `Test` functions in the packages `go test ./...` runs, plus 14 in the tagged
    conformance suite and 9 in the examples, assert wire frames, disconnect reasons and
    errors.
  - Tests that need external services are gated: miniredis is used by default, and the
    conformance tag fails rather than skips when dependencies are missing.
- **Interop is tested against the reference implementation.**
  - The differential suite compares Go and Node socket.io 2.5.0 transcripts across 12
    scenarios on both transports.
  - It runs with both pinned clients, a mixed Go/Node Redis cluster and permessage-deflate.
- **Hostile input is handled.**
  - Length and ID parsing is overflow-safe.
  - Attachments are capped at 10, and inbound messages at 1e6 bytes, including inflated
    websocket data.
  - Session IDs come from `crypto/rand`.
  - MessagePack input is validated before allocation.
- **Outbound queues are bounded.** In the 20,000-broadcast run above, the slow client was
  evicted without affecting the others. Normal teardown releases goroutines and room
  memberships, which `v1.4.2` leaked; error-path teardown can leave a frame-reader
  goroutine for 30 s (`T2`, [W12](#w12-close-frame-readers-on-errors-and-gate-goroutine-leaks)).
- **Reference docs are largely accurate.**
  - Every V1.md snippet compiles, and the v1 user and contributor documents contain 0
    broken links.
  - PROTOCOL.md lists deviations explicitly, and godoc defaults match the code.
  - Exceptions are the godoc and V1.md statements corrected by M4, M5, CL-3, CL-7 and
    CL-13.
- **Release mechanics are in place.**
  - `go.mod` is tidy and verified for the root module and 15 examples.
  - The library cross-builds for linux, windows, darwin, freebsd, wasm and 386.
  - CI run 39589689698 on `master` is green, including `govulncheck` on Go 1.27.3.

## Review limits

- **Fixes:** no fix for B1 or for any major finding was written or tested.
- **B1 variants:** direct websocket sessions were reproduced with clients sending pings or
  Socket.IO messages. A polling session upgraded to websocket was reproduced with a pinging
  client. All runs showed the same four blocked stacks. By source reading, a pure polling
  session is outside the cycle: its deadline setters are atomic stores
  (`engineio/transport/polling/payload.go:336-361`) and take no websocket write lock.
- **Platforms:**
  - All runtime testing ran on darwin/arm64.
  - Go 1.22 tests were not run locally; they run in CI only.
  - No Linux, Windows or 32-bit runtime testing was done.
- **Clients:**
  - All JavaScript runs used Node with `forceNode`; no browser was tested.
  - No third-party EIO3 clients (Java, Swift, Python) were tested.
- **Redis:**
  - A live mixed fleet (two upstream `v1.7.0` instances and one fork instance, one Redis)
    was run only for broadcast-triggered dispatcher termination. Failover and mixed-fleet
    `Clear` were not exercised.
  - A hostile Redis peer was not fuzzed.
  - Failover and reconnect under load were not tested.
- **Go client:** reconnect races, ACK-after-disconnect races and sustained websocket load
  were exercised only by the existing suites.
- **Baselines:**
  - There is no end-to-end performance comparison with `v1.4.2`, because its websocket
    broadcast deadlocked in the benchmark harness.
  - `apidiff` was not run against upstream `v1.8.0-rc.1` or upstream `master`.
  - Absolute timings are noisy: the reviews ran concurrently, with load averages up to
    about 130.
- **Performance evidence:** every timing, allocation and heap figure in this review comes
  from ad hoc benchmarks and probes that are not in the repository, so each is an ad hoc
  measurement (no committed benchmark yet), or, for `v1.4.2` values, a historical ad hoc
  measurement; not reproducible by a committed benchmark. When a performance claim is
  settled or a fix is done is defined by the performance rule in
  [How to use this plan](#how-to-use-this-plan).
- **Security:** only CORS, limits, fuzzing and session IDs were checked. Polling request-rate
  abuse, cookie and proxy deployments, and TLS or proxy header handling were not reviewed.
- **Docs:** PROTOCOL.md wire claims were spot-checked, not verified end to end. How
  pkg.go.dev renders the alias types could not be checked before publication.

## Finding catalogue

Every finding is listed under the dimension that reported it.

- **Severity** shows the reported value and, after the arrow, the value after re-checking.
- **Re-check** shows the outcome for blocker and major findings. Minor and nit findings were
  not re-checked in the original review.
- Rows were corrected in place where the follow-up and the fact-check required it. A
  `follow-up:` value in the Re-check column marks the rows whose verdict changed (CL-14,
  T5, REL-8). The follow-up status of every row is in
  [Catalogue cross-check](#catalogue-cross-check).
- Line numbers refer to `7311fbe`.
- Recommendations record the review's advice, corrected where noted. Where one differs from its work
  item, the work item governs. Tags, releases, branch deletion, cache purges on the
  maintainer's machine and git-identity changes are owner-only (see Conventions in
  [How to use this plan](#how-to-use-this-plan)).
- Every timing, allocation, heap and goroutine figure in the catalogue is an ad hoc
  measurement (no committed benchmark yet); W2 adds the committed benchmarks, and
  [Performance measurements](#performance-measurements) names the one that captures each
  figure. A `v1.4.2` value is a historical ad hoc measurement; not reproducible by a
  committed benchmark.
- CL-15, T7, T8 and REL-13 were found on 2026-10-24 while reviewing the W1 and W2 pull
  requests (#147, #153). They were not part of the original review or the follow-up, have
  no row in [Catalogue cross-check](#catalogue-cross-check) and are not among its 85
  entries. Their locations refer to `origin/master` at `d98e5d5`, not `7311fbe`.
- The [work plan](#work-plan) maps every ID to exactly one work item or owner decision, or
  lists it under "Not planned"; the finding coverage table is in
  [How to use this plan](#how-to-use-this-plan). When a fix merges, the row's
  Recommendation ends with `Fixed by #<PR> (W<n>).` (or `(D<n>)`), as described in
  Marking progress there.

### Correctness and security findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| C1 | blocker | confirmed | `engineio/internal/session/session.go:489` | Permanent three-way deadlock between a session's read path, an in-flight websocket write and Close/Abort/Shutdown | Do not hold upgradeLocker while calling into transport deadline setters. In setDeadline, read s.conn under RLock, release the lock, then call SetReadDeadline and SetWriteDeadline, as nextReader and nextWriterCompress already do. Also make Session.fail lock-free: keep closed and conn in atomics or snapshot them without the RWMutex, so a failing writer can never wait on a pending Close. Optionally store the websocket write deadline atomically and apply it inside nextWriter under writeLocker, so SetWriteDeadline does not wait for an in-flight write. Make engineio.Server.Close shut sessions down concurrently or with a bound, so one stuck session cannot block the others. Add a -race regression test: a non-reading websocket client keeps sending packets (pings or messages) while the server emits large payloads, and Close/Abort/Server.Close must return promptly with OnDisconnect fired. Fixed by #147 (W1). |
| C2 | major → minor | partially-confirmed | `server.go:406` | Dynamic namespaces created by clients through OfFunc are never removed (unbounded memory growth) | Remove a dynamic child, and close its adapter, when its last socket leaves (the equivalent of socket.io 4.6 cleanupEmptyChildNamespaces), or at least when no socket ever connected. Alternatively, offer a cap or opt-in cleanup option. State the retention in the OfFunc godoc and docs/V1.md. Fixed by #164 (W14). |
| C3 | minor | — | `engineio/server.go:303` | Duplicate session ID from a custom IDGenerator silently replaces a live session; Server.Close leaves the first one open | Under s.lifecycle in newSession, reject the handshake (or ask the generator again) when the sid is already registered or in s.live, and document that IDGenerator must return unique IDs. Fixed by #159 (W11). |
| C4 | minor | — | `namespace_handlers.go:223` | Panics in a user OnError handler are swallowed without any log | Log the recovered value through the connection or server logger, e.g. c.log.Warn("socketio: OnError panic", ...), as is done for other unhandled errors. Fixed by #161 (W13). |
| C5 | minor | — | `engineio/options.go:178` | Permissive CORS default (origin reflected with credentials, websocket origin check disabled) is documented only in godoc | Document the default and how to restrict it (WithCORS(false), transport CheckOrigin) in docs/V1.md in a security section. Consider an example that uses an origin allow-list. Fixed by #148 (W6). |

### API compatibility findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| API-1 | blocker → major | partially-confirmed | `CONTRIBUTING.md:24` | No v1 release notes or migration guide for upstream users; both were deleted just before release | Before tagging, restore a concise migration document owned in one place (for example a 'Migrating from googollee/go-socket.io v1.4–v1.7' section in docs/V1.md, or docs/MIGRATION.md linked from README). It should cover: the import path rewrite (a `replace` drop-in does not work, see API-2); the old-to-new identifier table (API-6/7/8); a default-behavior change table (API-3/4/5); and the Redis rolling-upgrade procedure. Publish the same summary as the GitHub release notes for v1.5.0, and update CONTRIBUTING.md:24-27,56 accordingly. Fixed by #191 (W8). |
| API-2 | major → minor | partially-confirmed | `CONTRIBUTING.md:32` | "v1.5.0" sits on a descendant of upstream v1.7.0 and clashes with upstream's own v1.5.0; the fork path's existing tags are unusable | Either choose a version above every upstream v1 tag (e.g. v1.8.0, or v1.9.0 to avoid confusion with upstream v1.8.0-rc.1), or keep v1.5.0 and state in README and the release notes that fork versions do not continue upstream numbering and that this is a new module with breaking changes. Either way, add `retract [v0.9.1, v1.4.2] // upstream tags; go.mod declares github.com/googollee/go-socket.io` to go.mod in the release commit so `go list -m -versions` and pkg.go.dev stop offering the unusable versions. Fixed by #193 (D1). |
| API-3 | major | confirmed | `engineio/options.go:39` | CORS default silently changed from same-origin to permissive-with-credentials and is undocumented outside godoc | Keep the decision if intended, but document it in docs/PROTOCOL.md (which owns wire behavior) and put it at the top of the migration and release notes, with `engineio.WithCORS(false)` and explicit `CheckOrigin` examples. Consider a short security note in the WithCORS godoc. Fixed by #148 (W6). |
| API-4 | major | confirmed | `adapter/redis/adapter.go:121` | Redis adapter wire format and channel names changed: mixed old/new Go instances lose cross-instance broadcasts during a rolling upgrade, and this is undocumented | Document in PROTOCOL.md or the migration guide that upstream Go Redis peers are wire-incompatible, and give the procedure: use a new `WithPrefix` for the new fleet, or do a stop-the-world upgrade. Fixed by #149 (W7). |
| API-5 | major → minor | partially-confirmed | `socket.go:257` | Other default and behavior changes vs upstream v1.7.0 have no release note; the non-root Conn.ID format is not in public godoc | Add a 'Changed defaults' table (old value, new value, opt-out) to the migration and release notes. State the namespace ID format in the public `Conn.ID` godoc (root types.go/connection contract), not only on the private method. Fixed by #191 (W8). |
| API-6 | minor | — | `server.go:276` | Root package: incompatible API changes vs v1.4.2 and v1.7.0, with no deprecation path | Include this old-to-new table in the migration guide. No compatibility stubs needed. Fixed by #191 (W8). |
| API-7 | minor | — | `engineio/conn.go:8` | engineio: five packages removed; Options/Dialer/Opener/Pauser removed; engineio.Conn gained methods | Cover it in the migration guide. The deleted V1_MIGRATION.md 'Engine.IO codecs and polling' table is a usable starting point once intermediate-master names are replaced with upstream v1.7.0 names. Fixed by #191 (W8). |
| API-8 | minor | — | `server.go:276` | Some v1.4.2-era calls still compile but mean something different: `BroadcastToRoom(room, event, s)` when `s` is a string (now read as namespace, room, event), and parser `Encode(h, args)` with nil `args` (untyped nil or a nil `[]interface{}`), which now writes a `null` payload where v1.4.2 wrote none | Call out both in the migration guide as 'compiles but changes meaning'. Code changes are optional, e.g. a vet-able note in the BroadcastToRoom godoc that the first argument is the namespace. Fixed by #191 (W8). |
| API-9 | minor | — | `engineio/conn.go:8` | Public aliases to internal packages hide method sets and fields from go doc, contrary to the project's own rule | Before the API freeze, define at least engineio.Conn, CloseError and RemoteError in their public packages (internal code can alias the public type instead), or extend TestPublicRoleDocumentation to these types. Fixed by #154 (D2). |
| API-10 | minor | — | `README.md:29` | README on master advertises an install command and pkg.go.dev links for a v1.5.0 that does not exist yet | Tag promptly after merge, or keep version-less instructions on master until the tag exists. Add the engineio pkg.go.dev link as CONTRIBUTING requires. Fixed by #193 (W10). |
| API-11 | minor | — | `Makefile` | No API-compatibility gate for v1 after release | After tagging, add a CI step such as `apidiff -m -incompatible` (or `gorelease -base=<latest v1 tag>`) against the latest v1 tag for the root module, failing on incompatible changes. |
| API-12 | nit | — | `engineio/parser/fake.go:61` | Test fakes returning unexported types are part of the public engineio/parser API | Move them to an internal test-support package or _test.go files before tagging, or accept and document them. Fixed by #156 (D6). |
| API-13 | nit | — | `go.mod:3` | Go version floor raised to 1.22 (v1.4.2: no directive; v1.7.0: go 1.16) | Already documented in README.md:32; also list it in the release notes. Fixed by #191 (W8). |

### Clarity findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| CL-1 | major | confirmed | `README.md:41` | No inline quick start: README links runnable server and Go client examples, but README, V1.md and godoc contain no code that mounts the handler or calls Serve, Close or client Connect, and there are no Example functions | Add a short server+client lifecycle sketch to the root package doc or docs/V1.md (README keeps linking to runnable examples, per b557f2a): NewServer, OnConnect/OnEvent, `go server.Serve()`, `defer server.Close()`, `http.Handle("/socket.io/", server)`, client.New/Connect/Emit with ack. Add runnable `Example` functions in the root and client packages. State in the Server type doc and Serve doc that Serve must run for connections to be dispatched. Fixed by #150 (W9). |
| CL-2 | major → minor → major (M4) | partially-confirmed | `adapter/redis/adapter.go:153` | Redis adapter silently drops room/namespace broadcasts with a trailing callback, while godoc says callbacks are supported | Either make Send/SendAll log or return the rejection (and have BroadcastToRoom/BroadcastToNamespace return false), or correct the redis Send/SendAll comments, the ErrBroadcastAck doc and the BroadcastToRoom/BroadcastToNamespace docs to say that clustered adapters reject callbacks and send nothing. Fixed by #152 (W5). |
| CL-3 | major → minor | confirmed | `server.go:150` | OnError contract undocumented: nil Conn for root rejections and silently recovered panics; V1.md's inactive-context wording is too broad | Document on both OnError methods that Conn is nil for root connect/middleware failures and an inactive Conn for other namespaces, and that panics in OnError are recovered and discarded. Fix the wording at V1.md:41. Consider logging recovered OnError panics at WARN. Fixed by #161 (W13). |
| CL-4 | major | confirmed | `README.md:7` | No migration guide from googollee/go-socket.io despite pervasive API changes; Conn.ID format change is undocumented | Add a short 'Migrating from googollee/go-socket.io' section, in V1.md or a linked file: import path, the constructor/option mapping, Redis adapter setup, client package, ID format, and removed APIs. State the ID format in the Conn.ID godoc of both roles. Fixed by #191 (W8). |
| CL-5 | minor | — | `.golangci.yml:17` | 9 of 12 public packages lack a package comment; lint explicitly disables the checks | Add package docs (root and client with a usage sketch). Document the remaining exported identifiers. Re-enable ST1000/ST1020-1022 or revive's exported rule, at least for the root, client, engineio and adapter packages. |
| CL-6 | minor | — | `adapter/redis/cluster.go:26` | redis.Cluster methods have unnamed parameters; argument order and the bool meaning are invisible | Name the parameters in the interface (ctx, sid, room, closeEngine) and add one-line docs per method, including error and timeout semantics. |
| CL-7 | minor | — | `adapter/adapter.go:66` | adapter.Adapter AllRooms comment contradicts the implementation (it refers to a connection); the Rooms comment is accurate | Rewrite the interface method docs to state that AllRooms lists all rooms in the namespace (cluster-wide for Redis) and that Rooms(nil) equals AllRooms, and convert the trailing comments into proper doc comments. |
| CL-8 | minor | — | `server.go:276` | Overlapping server APIs with different semantics and no guidance on which to use | In V1.md, state that the Server.*(namespace, ...) methods are retained wrappers in the upstream `v1.7.0` call shape (corrected: not `v1.4.2`-compatible, see API-8) and that Of(...) / Emitter is the primary API, and document the ack difference. In godoc, cross-reference the two Use variants. |
| CL-9 | minor | — | `_examples/default-http/main.go:19` | Starter example adds an explicit allow-all CheckOrigin that suggests CORS is off by default; the permissive CORS default is not in user docs | State the CORS default and how to restrict it (WithCORS(false) or a transport CheckOrigin) in V1.md. Drop the redundant CheckOrigin from default-http or replace it with a restrictive example. Fixed by #148 (W6). |
| CL-10 | minor | — | `README.md:29` | README advertises an unpublished v1.5.0 on master; inherited googollee tags make @latest fail | Tag v1.5.0 promptly after merge. Consider adding `retract [v0.9.1, v1.4.2] // upstream tags with module path github.com/googollee/go-socket.io` to go.mod in the release commit. Align the CONTRIBUTING step with the actual README links. Fixed by #193 (D1). |
| CL-11 | minor | — | `client/typed.go:152` | Exported 'internal bridge' methods with internal-package parameter types on client.Client and client.Socket | If they must stay exported for the cross-package bridge, group and label them consistently, for example 'Deprecated-style: for socketio.Event use only', and document Socket.RegisterEvent. Otherwise move the bridge behind an unexported interface satisfied via internal/event. Fixed by #186 (D3). |
| CL-12 | minor | — | `engineio/client/client.go:489` | Core concurrency code is hard to follow: 100-219 line functions and no written close/drain state machine | Add a short state-machine comment (or an ARCHITECTURE.md subsection) for connection close phases and client probe/upgrade. Annotate which mutex guards each field. Split probeTransport into dial, probe exchange, pause and commit steps. |
| CL-13 | nit | — | `_examples/default-http/go.mod:1` | Cosmetic inconsistencies in example module paths, error prefixes and option wording | Fix the module path typos, use a consistent package prefix in error strings, say 'nonpositive' where the code means it, mention the Go client in the README tagline, and document or unexport the test fakes. |
| CL-14 | nit | follow-up: editorial | `CONTRIBUTING.md:3` | Contributor build/test instructions live in CLAUDE.md and CONTRIBUTING links to them; the ownership table explicitly assigns development commands to CLAUDE.md | Moving commands is an optional editorial choice, not a correction of an ownership violation. |
| CL-15 | nit | — | `engineio/internal/session/session.go` | The W1 locking-rule comment at `upgradeLocker` names only read-lock holders and failing writers; write-lock holders and frame-reader failure paths are bound by the same rule | Extend the comment and the matching `closeTransport` note (W43). Found in the review of #147. |

### Architecture findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| ARCH-1 | major | confirmed | `adapter/redis/adapter.go:154` | Error-less adapter Send/SendAll silently drop Redis broadcasts; Server.BroadcastToRoom returns true and godoc promises per-socket callbacks | Before release, log WARN in redis Send/SendAll when SendTo fails, and fix the BroadcastToRoom/BroadcastToNamespace godoc: the bool only means the namespace is registered and its adapter was constructed, and callbacks depend on the adapter. Consider documenting Send/SendAll as legacy wrappers over SendTo, with a helper third-party adapters can reuse, before freezing the adapter.Adapter interface. Fixed by #152 (W5). |
| ARCH-2 | minor | — | `engineio/server.go:1` | Public types are aliases to internal packages, hiding their methods from go doc and breaking the project's own rule | Declare engineio.Conn (and the other session-facing types) in the public engineio package with session implementing them. For internal/event, wrap Handler in an opaque public struct without exported methods, or move the types to a public role-neutral package. Extend TestPublicRoleDocumentation to engineio and the root non-role types. Fixed by #154 (D2). |
| ARCH-3 | minor | — | `client/event.go:10` | Typed client events live in the server package, so a client program that uses typed events must import the server | Before v1.5.0, consider a public role-neutral package (for example `event`) that owns Event/AckEvent/Endpoint/RemoteError and the sentinel errors, with root aliases kept for convenience, so client.RegisterEvent no longer exposes an internal type. Fixed by #186 (D3). |
| ARCH-4 | minor | — | `queue.go:88` | Vestigial non-server mode in the server connection: Start sets boundary before admission, so the writeChan delivery path is never used at runtime; only its capacity feeds the queue budget | Remove `boundary` and the writeChan path. Keep an integer capacity and a single ordered queue, and simplify the budget expressions accordingly. |
| ARCH-5 | minor | — | `adapter/adapter.go:46` | Adapter extension contract is redundant and partly duck-typed | Before tagging, fix the AllRooms comment and declare the optional capabilities as named interfaces in package adapter (e.g. `SocketLookupBinder`). Document that the Adapter method set is frozen for v1 and new capabilities arrive as optional interfaces. Fixed by #155 (D4). |
| ARCH-6 | minor | — | `types.go:61` | Conn.EngineConn exposes full engine frame I/O alongside the library's own reader and writer | Document EngineConn as metadata-only (ID, Transport, Upgraded, ReadyState, Close), or return a narrower public interface before v1.5.0. Fixed by #189 (D5). |
| ARCH-7 | minor | — | `client/namespace_handlers.go:100` | Server and client runtimes are maintained in parallel | Keep role independence, but extract the role-neutral pieces (listener registry, frame failure tracking, header-reader worker) into an internal package the graph allows for both roles, or add paired behavioral tests that run against both roles. |
| ARCH-8 | minor | — | `engineio/client/client.go:489` | Complexity hotspots in lifecycle code, including a 1 ms Resume retry loop because Pause has no cancellation path and a Resume issued before Pause starts is lost | For v1.5.0 no restructuring is required, but freeze behavior with the existing tests. Afterwards, give Pause a cancellation path (a context or stop channel) and model the connection close as an explicit state enum. |
| ARCH-9 | minor | — | `namespace.go:161` | Dynamic child namespaces are never removed (unbounded registry growth), and OfFunc does not document it | Document the retention in OfFunc and in docs/V1.md, and advise restrictive match functions. Consider a later option to remove empty children (as socket.io 4.6 cleanupEmptyChildNamespaces does). Fixed by #164 (W14). |
| ARCH-10 | minor | — | `parser/encoder.go:138` | Parser reimplements encoding/json traversal for every outbound payload; the GOEXPERIMENT=jsonv2 build variants are not exercised in CI | Add a CI job with GOEXPERIMENT=jsonv2 on the newest Go, and keep the json_compatibility tests as a differential check against encoding/json. Consider a fast path that uses json.Marshal directly when the value contains no parser.Buffer. |
| ARCH-11 | nit | — | `engineio/parser/fake.go:61` | engineio packages export test fakes and other API oddities | Move the fakes to an internal test helper package, unexport TimestampFromClock, and fix the two stale `base.Conn` doc comments before tagging. Fixed by #156 (D6). |
| ARCH-12 | nit | — | `adapter/redis/cluster.go:104` | Redis log records use the attribute key 'namespace', which the logger contract does not list | Use nspAttr-style `nsp` with the '/' convention. |
| ARCH-13 | nit | — | `docs/ARCHITECTURE.md:36` | ARCHITECTURE.md runtime section undercounts per-connection goroutines | List all five Socket.IO workers, and say that blocking a handler also blocks the header worker handoff. |

### Performance findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| PERF-1 | major | confirmed | `parser/json_encode.go:114` | Branch's reflective JSON encoder regresses encode cost vs master and v1.4.2 (per-leaf json.Marshal, per-packet map, extra copies) | Keep the visibility and snapshot semantics but add a cached per-reflect.Type fast path. When a type's subtree cannot contain a Buffer, an interface or a custom marshaler, encode the whole subtree with one json.Marshal or Encoder call. Write string and number leaves with append-style helpers instead of json.Marshal per leaf. Create the cycle-detection map lazily, only past a depth threshold as encoding/json does. Write directly into the final buffer with header space reserved, avoiding the copy into `data`. Commit a parser encode benchmark before changing the encoder, so the CI base-vs-PR comparison shows the before/after. Fixed by #169 (W3). |
| PERF-2 | major → minor | partially-confirmed | `engineio/transport/polling/payload.go:412` | Polling server creates a long time.After timer per packet and never stops the losing timer; when the application is built with pre-1.23 timer semantics (main module go line below 1.23, a pre-1.23 toolchain, or asynctimerchan=1), each timer keeps heap memory alive for ~45s. This library's own go directive does not decide this | Use one per-payload timer for each direction and Reset/Stop it, or an AfterFunc/context set in SetRead/WriteDeadline, and stop the timer when select returns on another case. Add a test that asserts bounded heap after N polling packets with asynctimerchan=1. |
| PERF-3 | major | confirmed | `adapter/redis/codec.go:191` | Redis PUBLISH has no I/O deadline; a stalled Redis blocks every broadcasting caller indefinitely | Bound publishing I/O by the requests timeout: add DialWriteTimeout on publishing connections only (a read timeout must not reach the shared Pub/Sub subscriber, which uses the same dialer) and run each publishing command with redis.DoWithTimeout, which bounds the read (corrected: DoWithTimeout sets the read timeout, DialWriteTimeout the write timeout), or close the connection on deadline as boundedDo does; DoContext is unavailable in redigo v2.0.0+incompatible. Alternatively, publish through a bounded asynchronous queue and report errors through the adapter error path. Fixed by #151 (W4). |
| PERF-4 | minor | — | `adapter/redis/adapter.go:100` | Redis publishing pool (MaxIdle 4, unbounded MaxActive) churns TCP connections under concurrent broadcasts | Size MaxIdle to the expected publish concurrency (or expose an option). Alternatively set MaxActive with Wait: true so callers queue on a bounded set of connections. Fixed by #158 (W19). |
| PERF-5 | minor | — | `engineio/transport/polling/transport.go:24` | Go polling client: polling.Default shares http.DefaultTransport (2 idle conns/host), so several clients per process churn TCP under sustained traffic and can exhaust ephemeral ports | Give polling.Default its own http.Transport with a larger MaxIdleConnsPerHost (for example 64), or create a transport per dialed client. Document WithTransports(&polling.Transport{Client: ...}) for high client counts. |
| PERF-6 | minor | — | `adapter/memory.go:144` | Broadcast encodes the packet once per recipient; the shareable PreparedPacket is not used for fan-out | Add an internal fast path: prepare once in the broadcast path and have namespaceConn accept a prepared packet, for example through an unexported optional interface checked by Memory and Redis, without changing adapter.Socket. Per-socket ACK variants already use WithHeader. Fixed by #160 (W21). |
| PERF-7 | minor | — | `socket.go:154` | Every send copies the full packet via WithHeader even when no ACK ID was added | Call WithHeader only when registerAck changed the header (ack != nil). |
| PERF-8 | minor | — | `engineio/transport/websocket/wrapper.go:146` | Websocket wrapper spawns a goroutine and a 30s timer for every frame read and written | Drop the nag, or make it debug-only. If kept, use time.AfterFunc with no goroutine and stop it on Close. |
| PERF-9 | minor | — | `server.go:252` | Server.JoinRoom/LeaveRoom/LeaveAllRooms copy and scan all namespace sockets on each call | Check membership with a map lookup under connectedMu (nh.connected[nc.ID()] == nc) instead of copying the slice. |
| PERF-10 | minor | — | `.github/workflows/benchmarks.yml:148` | No Socket.IO-level benchmarks; CI benchmark comparison cannot see parser/server regressions | Add benchmarks for parser Prepare/Encode/Decode (string, struct, struct slice, binary), memory-adapter broadcast to N fake sockets, and an emit+ACK round trip over httptest websocket. Then let the existing base-vs-PR workflow pick them up. Fixed by #153 (W2). |
| PERF-11 | minor | — | `parser/decoder.go:87` | Inbound decode 37–42% slower than v1.4.2 across runs, because it parses JSON three times; it also allocates a 4 KiB bufio.Reader per non-byteReader frame, as v1.4.2 does | Fold json.Valid into the RawMessage unmarshal, which already validates, and avoid parsing the payload more than once; reusing a bufio.Reader per Decoder (Reset), or reading directly when the frame reader is already a byteReader, is optional (corrected: `v1.4.2` also allocates one per frame, so it is not the cause). |
| PERF-12 | nit | — | `adapter/memory.go:110` | Broadcast target selection builds a dedup map of all members even for one room without Except | Skip the seen map when len(opts.Rooms) == 1 and len(opts.Except) == 0, and pre-size the result slice (corrected: not `<= 1`; namespace-wide delivery with no rooms walks every room, and each socket is in its own ID room and possibly named rooms, so it needs the map, see W21). Fixed by #160 (W21). |
| PERF-13 | nit | — | `connection.go:281` | 7 server goroutines per idle websocket connection (v1.4.2: 4) | Optional: merge the engine close watcher with observeEngineClose, and start the error loop lazily. |

### Interoperability findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| interop-1 | major → minor | confirmed | `engineio/transport/websocket/wrapper.go:39` | WebSocket transport ignores Engine.IO base64 mode; a forceBase64 client is disconnected when it sends binary | In the websocket transport, decode text frames that start with 'b' as base64 binary packets. When the request carries `b64=1`, write binary packets as `b<base64>` text frames, as engine.io 3.6.2 does. Add a forceBase64 case (websocket and upgrade) to testdata/node/client.js. If this is not implemented for v1.5.0, document it in PROTOCOL.md as unsupported. Fixed by #162 (W15). |
| interop-2 | minor | — | `parser/encoder.go:144` | Every JSON payload and the OPEN packet carry an undocumented trailing newline | Either drop the newline (both the Socket.IO payload and the OPEN packet), or record it in PROTOCOL.md as an intentional difference kept for compatibility with older go-socket.io releases. Fixed by #167 (W16). |
| interop-3 | minor | — | `engineio/transport/polling/util.go:21` | Polling POST rejects Content-Types that the reference server accepts | Match the reference: treat any non-octet-stream body as text. Otherwise, document the exact accepted media types as a Go-specific restriction in PROTOCOL.md. Fixed by #167 (W16). |
| interop-4 | minor | — | `packets.go:211` | Connect and event middleware rejections are always sent as a string; no counterpart to Node's err.data | Allow an error type (for example one with `Data() any`) whose value becomes the ERROR payload, or document in PROTOCOL.md that ERROR payloads are always `err.Error()` strings. Fixed by #167 (W16). |
| interop-5 | minor | — | `packets.go:103` | Untyped handler with a mismatched argument type sends no ACK, so the JS callback never fires (undocumented) | Document the behavior next to the ACK rules in V1.md or PROTOCOL.md. Consider sending an error ACK or an ERROR packet so JS callers can detect the failure. Fixed by #167 (W16). |
| interop-6 | nit | — | `engineio/transport/websocket/connect.go:117` | Server-initiated websocket close sends no close frame | Write a CloseMessage (1000) with a short deadline before closing the underlying connection on orderly server closes. Fixed by #167 (W16). |
| interop-7 | nit | — | `docs/PARITY.md:20` | PARITY.md groups heartbeat with raw conformance coverage, but no conformance scenario asserts a periodic ping | Add a short-interval heartbeat scenario (for example `WithPingInterval(200ms)`, then idle for about 1 s and send an ACK) to client.js for both transports, or make the PARITY.md row say which tests own the heartbeat. Fixed by #168 (W17). |
| interop-8 | nit | — | `internal/conformance/testdata/node/server/redis-cluster.js:199` | Redis mixed-cluster polling scenario idles 30 s after finishing, leaving little margin in runNode's 45 s timeout | Find and remove what keeps the process alive after cleanup, or end the scenario with an explicit `process.exit(process.exitCode \|\| 0)` after cleanup (corrected: the clients are already closed before `io.close()`, `redis-cluster.js:199-200`; see W17). Fixed by #168 (W17). |
| interop-9 | nit | — | `docs/PROTOCOL.md:60` | Unknown Socket.IO packet type and missing CORS wire description are undocumented differences | Add a CORS bullet (default, WithCORS(false), preflight) and a note on how invalid packets are handled to PROTOCOL.md. Fixed by #148 (W6). |

### Tests findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| T1 | minor | — | `parser/json_encode.go:252` | omitempty handling in the rewritten Socket.IO JSON encoder has zero test coverage | Add a table-driven differential test in parser/json_compatibility_test.go that encodes omitempty structs (all kinds handled by isJSONEmpty, including nested and Buffer-bearing fields) and compares against json.Marshal. Consider adding an encoder fuzz target that compares against encoding/json for binary-free values. Fixed by #169 (W3). |
| T2 | minor | — | `engineio/transport/websocket/wrapper.go:76` | No goroutine-leak detection; a probe finds library frame-reader goroutines left for 30s after parse/mid-frame errors, plus leaking client test fixtures | Close the last frame reader on the decode-error and transport-failure paths in the server and client read loops. Fix the leaking fixtures by closing clients via t.Cleanup and using buffered or select-guarded channels in handlers. Add a goroutine-leak check (go.uber.org/goleak VerifyTestMain or an equivalent NumGoroutine wait) to the root, client and engineio packages so close and shutdown regressions fail CI. Fixed by #157 (W12). |
| T3 | minor | — | `docs/PARITY.md:20` | No conformance scenario asserts the Engine.IO periodic heartbeat against a real JS client; heartbeat is covered only by engineio/session tests | Add a conformance scenario with WithPingInterval(200ms) and WithPingTimeout(200ms) that keeps a JS client connected across several intervals on each transport. Also have the client suppress pings and assert 'ping timeout'. Alternatively, correct the PARITY.md row. Fixed by #168 (W17). |
| T4 | minor | — | `redis_integration_test.go:174` | TestRedisServerCloseStopsBroadcasts spends 10s inside a blocking OnConnect and neither bounds nor documents that behavior | Expose a test hook or redis option (dial timeout) so the root test can use a short timeout and assert that registration returns within it. Document in docs/V1.md that registering a handler on a new namespace dials Redis synchronously and can block up to the dial timeout. |
| T5 | nit | follow-up: partial | `parser/prepared_test.go:64` | No encoder or round-trip fuzz target. The decoder package `engineio/parser/internal/packet` has no tests of its own, but `TestPacketDecoder` and other `engineio/parser` contract tests exercise it through `NewPacketDecoder` (89.5% cross-package coverage), so that subclaim is not a material gap | Add FuzzDecodeArgs (decode into a fixed set of typed targets) and a round-trip fuzz (Prepare -> Decoder -> compare) for binary-free values. |
| T6 | nit | — | `engineio/transport/polling/pauser_test.go:206` | Residual sleep-based synchronization in inherited polling tests weakens rather than flakes | Replace the sleeps with explicit readiness signals, for example a hook or channel signalled when the goroutine enters the blocking wait. |
| T7 | minor | — | `engineio/server_close_concurrent_test.go` | `TestServerCloseShutsDownSessionsConcurrently` depends on map iteration order and passes against a serial `Server.Close` in about 1 of 17 runs (a mutation run failed 39 of 40) | Fail two sessions and close `entered` through `sync.Once` (W43). Found in the review of #147. |
| T8 | minor | — | `engineio/transport/polling/polling_test.go:82` | `TestPollingBinary` crashed with a nil-reader dereference during a stress run under ephemeral-port exhaustion; the cause is not isolated | Reproduce with a forced dial failure and fix the side at fault (W44). Found while validating #147. |

### Release engineering findings

| ID | Severity | Re-check | Location | Finding | Recommendation |
| --- | --- | --- | --- | --- | --- |
| REL-1 | major → minor | partially-confirmed | `CONTRIBUTING.md:53` | No v1.5.0 release notes or migration guide, although this release breaks the upstream API | Before tagging, write release notes for the GitHub release covering the module path change, the API mapping from upstream v1.7.0/v1.8.0-rc.1, changed defaults and the support matrix. Either add a short 'Migrating from googollee/go-socket.io' section to docs/V1.md or link one from the release. Add a 'publish GitHub release notes' step to CONTRIBUTING 'Before tagging v1'. Fixed by #191 (W8). |
| REL-2 | major → minor | partially-confirmed | `CONTRIBUTING.md:49` | A v1.5.0 tag was published and deleted before; the stale module is still cached and will undermine release verification | Before tagging, delete the cached v1.5.0 entries (cache/download/.../@v/v1.5.0.* and the extracted @v1.5.0 directory, or `go clean -modcache`). Run the consumption check with a fresh `GOMODCACHE=$(mktemp -d) GOFLAGS=-mod=mod go get github.com/sshaplygin/go-socket.io@v1.5.0`. Alternatively, choose a version that was never tagged. |
| REL-3 | minor | — | `.git/config` | The repo-local git identity is a placeholder and has leaked into master history; an annotated tag would carry it | Remove or replace the local user.name/user.email before creating the tag or merging more PRs. Check the squash-merge body for Co-authored-by lines before confirming. |
| REL-4 | minor | — | `.github/workflows/ci.yaml:378` | The full CI on push and schedule never runs the stress job, although the docs say it runs everything | Run stress-v1 on push and schedule with PKGS=./..., or add a manual `make test-stress` step to the release checklist. Make CLAUDE.md and the ci.yaml comment match whichever you choose. |
| REL-5 | minor | — | `.github/workflows/ci.yaml:17` | Branch protection requires only the drift and conformance checks; the v1 test, lint and gate jobs are not required | Add Lint v1, Gates v1 and the Go 1.22 job to the required checks; they report as skipped under their own names when their job-level if is false. The Test v1 matrix cannot be required directly: when skipped it reports one check under the unexpanded name (ci.yaml:91-93). Require it through an aggregating 'CI ok' job that runs with `if: always()` and fails when any job it needs failed or was cancelled, or make it always run and skip its steps, as the conformance job does. |
| REL-6 | minor | — | `README.md:29` | README on master advertises v1.5.0 before it exists; the release checklist's Engine.IO link is missing; old upstream tags make @latest fail today | Push the tag promptly or keep the versioned links in the release PR, as CONTRIBUTING intends. Add the engineio pkg.go.dev link. Consider adding `retract [v0.9.1, v1.4.2]` to go.mod in the release commit to hide the inherited upstream tags. Fixed by #193 (W10). |
| REL-7 | minor | — | `CONTRIBUTING.md:32` | Version v1.5.0 reuses an upstream release number and is lower than the upstream versions it descends from | CONTRIBUTING.md:32 already selects v1.5.0. Say in the release notes that fork versions do not continue upstream numbering, and which upstream state this release supersedes. Using a number above upstream (for example v1.9.0) would require the owner to change CONTRIBUTING and README first. Fixed by #193 (D1). |
| REL-8 | nit | follow-up: reproduced | `client/connection_typed_test.go:66` | The test package fails to compile on 32-bit platforms | Use `int64` consistently in the request schema, decoded value and expected assertion value, or constrain this exact-integer test to 64-bit builds. Changing only the final `event.Value` type is insufficient. |
| REL-9 | nit | — | `.github/workflows/ci.yaml:244` | CI tool versions are unpinned and some actions target the deprecated Node 20 | Pin the golangci-lint version, matching the local v2 toolchain, and update to Node-24 action versions. Keep govulncheck@latest only if that is intended. |
| REL-10 | nit | — | `_examples/gf/go.mod:24` | Example modules pin old golang.org/x/net and have typos in their module paths | Bump the framework and x/net versions in the example modules and fix the module path typos. |
| REL-11 | nit | — | — | Stale remote branches, including a superseded release branch and a changelog draft | Delete merged or superseded branches, release/v1.5.0 in particular, before tagging. Reuse the useful parts of the v1-h/release-notes draft for REL-1. |
| REL-12 | nit | — | `engineio/transport/websocket/connect.go:99` | An open TODO remains in library code | Replace the TODO with a comment that explains why the lock is needed. Fixed by #147 (W1). |
| REL-13 | minor | — | `.github/workflows/benchmarks.yml` | A sub-benchmark whose `b.N=1` probe fails in a later `-count` repetition prints `--- FAIL` but `go test` exits 0, so the Benchmarks workflow can lose samples without failing | Fail the step on `--- FAIL` in the raw output (W42). Found in the review of #153. |

## Follow-up validation

This section is an evidence log: it records a status and the evidence or limits for
each finding. Conclusions live in the sections above, which were corrected where this
evidence required it. Work status is owned by the [work plan](#work-plan).

Validated on 2026-10-21 against `ebc9f5e`, using Go 1.25.5 on darwin/arm64, and
fact-checked on 2026-10-22 against `0fa1ead` (identical tree); see
[Fact-check on 2026-10-22](#fact-check-on-2026-10-22).
The working tree was clean before validation. Runtime probes and benchmarks ran
in disposable `git archive` copies; no library code or permanent tests were changed.
All 85 catalogue entries were assessed below. **Reproduced** means a new probe or
check demonstrated the claim; **source-confirmed** means the relevant implementation
or configuration was inspected. **Partial** identifies an overstatement, a missing
measurement, or a recommendation whose necessity is a judgment call. **Confirmed** means a
command reproduced the stated state of the repository or CI. **Confirmed snapshot** means
the state of an external system (GitHub, the module proxy, the local configuration) was
observed on the stated date and may change. **Editorial** marks a finding about wording or
ownership rather than behavior. **Measured** means a value was obtained by an ad hoc
probe (no committed benchmark yet). **Snapshot** is short for Confirmed snapshot. A
qualifier after a status (for example "timing, cause not isolated", "coverage gap" or "at
reduced scale") narrows what was reproduced. Compound labels apply these terms to
different parts of one row.

### Release-relevant results

| Finding | Follow-up result | Evidence and limits |
| --- | --- | --- |
| B1 / C1 | Reproduced | Follow-up: a real websocket session held an open frame writer (`ws.writeLocker`). A deadline update then blocked on that lock while holding the read side of `upgradeLocker`, and Abort waited for its write side. Closing the peer and writing 8 MiB entered `Session.fail` while the frame writer still held `ws.writeLocker`. All three operations were still blocked two seconds later, with the reported stacks. Fact-check: the queue-overflow workload deadlocked with the same four stacks for a non-reading client sending only pings, one sending only Socket.IO messages (`42["x"]`), and a pinging client upgraded from polling to websocket. Each time no `OnDisconnect` fired within 30 s and `Server.Close` returned only at its 10 s bound. A client that sends nothing was disconnected normally. Any inbound packet enters `setDeadline` (`session.go:198`), not only a ping. |
| M1 | Source-confirmed, with corrections | No migration guide exists; the deleted guide and upstream sources confirm API changes. The Engine.IO counts measure different scopes: 8 for the `engineio` package, 17 including `transport`, `transport/polling` and `transport/websocket`; the fact-check reproduced both with apidiff against `0fa1ead`. The heartbeat defaults changed from interval 20 s / timeout 60 s to 25 s / 20 s; 80 → 45 s is the client-side sum, and the server idle deadline changed from PingTimeout alone (60 s) to PingInterval + PingTimeout (45 s). |
| M2 | Source-confirmed; current behavior tested | Existing CORS tests passed under `-race`, including foreign-origin polling and direct/upgraded websockets. README, V1.md and PROTOCOL.md omit the default. `WithCORS(false)` stops polling responses, including error responses, from carrying `Access-Control-Allow-Origin`/`-Credentials`; an `OPTIONS` request on an open session still answers 200 with `Access-Control-Allow-Headers: Content-Type`. It is not an HTTP access-control rule: cross-origin polling requests are still served. Upstream v1.7.0 embeds Engine.IO rather than depending on go-engine.io v1.4.1. |
| M3 | Source-confirmed and reproduced | In a fork/upstream `v1.7.0` fleet on real Redis (fact-check), one fork namespace-wide broadcast stopped the dispatcher of every upstream peer in that namespace (request responders 2 → 0). Those peers then receive no broadcasts and answer no cluster requests, and their `RoomLen` and `Rooms` calls, which have no timeout, blocked indefinitely, because the stopped peers stay subscribed and Redis `PUBSUB NUMSUB`, which upstream uses to count expected responders, still counts them. A fork room broadcast is ignored unless the room name, or its last `#`-separated segment, equals the namespace. Fork root traffic does not match the upstream root pattern (upstream root pattern `socket.io##*`; fork root channels `socket.io#/#…`), so it reaches no upstream root dispatcher. |
| M4 | Reproduced for ACK rejection; error loss source-confirmed | Both `Send` and `SendAll` returned without local delivery for a trailing callback; `SendTo` reported the rejection. Their wrappers discard that error and publication errors. `BroadcastToRoom`/`BroadcastToNamespace` report only that the namespace is registered and its adapter was built without error. They still return true after `Server.Close` has closed the adapter, and they never report delivery. Publication failure can follow successful local delivery. |
| M5 | Reproduced | A dedicated Redis process was paused with SIGSTOP. With `RequestsTimeout=50ms`, `SendTo` was still blocked when the 5 s observation window ended (100 times the requests timeout). It was still blocked 2 s after adapter Close returned, which took under 1 ms. Only SIGCONT released it, and `SendTo` then returned a nil error. TCP keepalive is not a five-minute upper bound on this failure. |
| M6 | Reproduced | Follow-up: identical encode benchmark against `3dd094d` and `ebc9f5e` (event plus 100 structs of int, string, bool and float64; three one-second samples per revision): median 51,570 → 74,017 ns/op (+43.5%), about 6,089 → 29,541 B/op, 12 → 817 allocs/op. Fact-check: rerunning the original harnesses against `v1.4.2`, `3dd094d` and `ebc9f5e` gives exactly the original 1,004 / 1,012 / 2,518 allocs/op and about 137 → 203 µs (+48%); end to end to 10 websocket clients, +48% to +57% time and +145% allocations across reruns. Allocation counts depend on the payload: scalar-only structs 12 → 817; structs with a `[]string` field 12–13 → 1,418–1,419; the original structs with `[]string` and `map[string]string` fields 1,012 → 2,518. Absolute times vary between runs; allocation counts and relative increases are stable. Each figure is an ad hoc measurement (no committed benchmark yet); see [W2](#w2-add-socketio-level-benchmarks) and [W3](#w3-restore-encoder-performance). Each `v1.4.2` value is a historical ad hoc measurement; not reproducible by a committed benchmark. |
| M7 | Partial | No inline server/client quick start or Go `Example` functions. Runnable server and Go client examples are linked from README, intentionally since `b557f2a`. No `Serve` lifecycle guidance exists. The original wording "no examples anywhere" was an overstatement and is corrected in M7. |

Severity and verdict are owned by [Verdict](#verdict-ready-after-fixes),
[Release blockers](#release-blockers), [Should fix before release](#should-fix-before-release)
and [Decisions required before tagging](#decisions-required-before-tagging).

### Catalogue cross-check

The grouped rows cover every catalogue ID, including duplicates of B1 and M1–M7.
Counts and timings from the original review remain historical unless explicitly
reproduced here.

| IDs | Status | Follow-up evidence or qualification |
| --- | --- | --- |
| C1 | Reproduced | B1 above. |
| C2, ARCH-9 | Source-confirmed | Dynamic children remain in `Server.views`, the namespace registry and the parent's children map; disconnect does not evict them. The 20,000-namespace heap measurement was not repeated. |
| C3 | Reproduced | Two real websocket sessions with a constant custom SID: Server.Close closed the second and left the first in `open` state. Manual close of the first was needed for cleanup. |
| C4, CL-3 | Source-confirmed | `ErrorHandler` discards recover's result. Root rejection can supply nil; V1.md's unconditional inactive-context wording is too broad. Existing tests passed. |
| C5, API-3, CL-9 | Source-confirmed | M2 above; the starter example also explicitly allows every origin. |
| API-1, CL-4, REL-1 | Source-confirmed | M1 above. A missing migration guide does not imply a requirement to restore a standalone changelog against the documented policy. |
| API-2, REL-7 | Partial | Fork module path and unusable inherited tags confirmed (cached `v1.4.2` declares the upstream path). Upstream and fork versions are distinct module identities; matching an upstream number is not itself a semver violation. CONTRIBUTING.md "Releases" already names `v1.5.0` (line 32 at `0fa1ead`); the owner decides it in [D1](#d1-version-number-and-inherited-tags), which was open when this log was written. |
| API-4 | Source-confirmed | M3 above, with namespace-wide versus single-room channel qualification. |
| API-5 | Source-confirmed, corrected | Non-root ID format is implemented but absent from public Conn.ID godoc; heartbeat defaults corrected in M1. |
| API-6, API-7 | Reproduced | `apidiff` re-run against `0fa1ead` gives the same output as the original run: root 24 incompatible changes vs `v1.4.2` and 20 vs upstream `v1.7.0`; `parser` 4 vs `v1.4.2` and 3 vs `v1.7.0`; `engineio` 8 vs `v1.7.0`, or 17 counting `transport`, `transport/polling` and `transport/websocket`. Package comparison: upstream `engineio/frame`, `packet`, `payload`, `session` and `transport/utils` have no public counterpart (`packet`, `payload` and `session` now live under `internal/`). |
| API-8 | Partial | The old BroadcastToRoom call compiles only if its third argument is assignable to string. Its arguments shift position; the result need not always be "nothing sent." Parser `Encode` changed from `(Header, []interface{})` to `(Header, ...interface{})`, as in upstream `v1.7.0`, and encodes only the first argument. A `v1.4.2`-style call with a non-nil slice encodes the same bytes; a nil argument now adds a `null` payload (`1/a` becomes `1/a,null`). |
| API-9, ARCH-2 | Reproduced | `go doc ./engineio Conn.ID` fails; `go doc . RemoteError` shows only an alias. Recount: 24 exported type aliases target internal packages (7 in `engineio`, 13 in `engineio/parser`, 4 in the root package). This is a documentation and discoverability problem; the aliased types and their methods remain usable. |
| API-10, CL-10, REL-6 | Confirmed snapshot | README targets v1.5.0 and omits an Engine.IO reference link. The GitHub tag, public proxy and sumdb lookups returned 404 during validation. Cached inherited v1.4.2 declares the upstream path. No tag or retraction was created. |
| API-11 | Source-confirmed | No apidiff/gorelease gate in the v1 Makefile or workflow. This is a future compatibility guard, not proof of a current release failure. |
| API-12, ARCH-11 | Source-confirmed | Public codec fakes and TimestampFromClock are present; `base.Conn` is stale wording. Removing exports is an API design choice. |
| API-13 | Source-confirmed | go.mod declares 1.22 and README documents it; no new Go 1.22/1.21 run was performed. |
| CL-1 | Partial | M7 above; README links to runnable examples. |
| CL-2, ARCH-1 | Reproduced / source-confirmed | M4 above. The bool should not be interpreted as delivery success. |
| CL-5 | Confirmed | `go list` reports missing package docs for 9 of 12 public runtime packages; the specified lint checks are disabled. |
| CL-6 | Source-confirmed | Cluster interface parameters are unnamed. Per-method clarification would improve usability; the interface already documents a common timeout contract. |
| CL-7, ARCH-5 | Source-confirmed | AllRooms' interface comment wrongly refers to a connection; Rooms(nil), optional Closer and BindSocketLookup behavior are present. Redundancy and named optional interfaces are design judgments. |
| CL-8 | Partial | Broadcast wrappers and Namespace/Emitter have different ACK behavior. Calling every Server wrapper "upstream-compatible" would contradict API-8 for v1.4.2 users. |
| CL-11, ARCH-3 | Source-confirmed | Typed descriptors are in the root package and client bridge signatures use internal/event types. The client runtime itself does not import the server; application use of the typed descriptors adds that import. |
| CL-12, ARCH-8 | Partial | `probeTransport` (`engineio/client/client.go:489`) is 219 lines with cyclomatic complexity 45 (gocyclo v0.6.0); six other functions are 107–152 lines. Failed probe cleanup calls Resume on a one-millisecond ticker until the pause goroutine returns. It is a timed polling loop, not an unscheduled tight spin. |
| CL-13, REL-10 | Source-confirmed | Example module path typos and old x/net requirements are present. `make vuln` on Go 1.27.3 in CI run 39599568659 (`0fa1ead`) reported no called vulnerabilities in any example module, so the old pins are hygiene, not a demonstrated vulnerability. |
| CL-14 | Editorial; ownership premise refuted | CLAUDE.md's ownership table explicitly assigns development commands to CLAUDE.md. CONTRIBUTING links there. Moving those commands is optional. |
| ARCH-4 | Source-confirmed / measured | Runtime Start sets boundary before admission (`connection.go:276`), so nothing is ever sent on writeChan; only its len() and cap() feed the queue budget. Each connection still allocates the channel buffer: 88 B × 64 = 5,632 B of element storage at the default buffer size on 64-bit platforms, rounded up by the allocator to 6,144 B (about 6 KiB), measured on darwin/arm64 with Go 1.25.5 by a probe that was not retained: an ad hoc measurement (no committed benchmark yet); see [W26](#w26-reduce-the-idle-connection-footprint). |
| ARCH-6 | Source-confirmed | EngineConn exposes frame I/O used by the library. Restricting the return type is a public API choice, not an independently reproduced runtime defect. |
| ARCH-7 | Source-confirmed | Matching header-worker and listener/dispatch logic exists in both roles. The original `dupl` output (16 clone groups, 15 pairing `client/` with the root package) was produced from source copies byte-identical to the current tree. Extraction is optional. |
| ARCH-10 | Source-confirmed | Custom JSON traversal and jsonv2 build variants exist; CI has no GOEXPERIMENT=jsonv2 job. No new jsonv2 run was performed. |
| ARCH-12 | Source-confirmed | Both Redis log records, worker limit reached (`cluster.go:104`) and cluster request failed (`requests.go:274`, which every Redis request/reply error and worker panic goes through), use `namespace`, which is not in the logger's allowed key list (`nsp`). |
| ARCH-13 | Source-confirmed | Runtime docs name only read-dispatch, write and error workers; header-read and engine-close observers are additional Socket.IO workers. |
| PERF-1 | Reproduced | M6 above. |
| PERF-2 | Reproduced at reduced scale | readTimeout/writeTimeout use time.After without stopping losing timers. Over 50,000 polling packets, heap grew 1.0 → 42.8 MiB when the main module declared go 1.22 or set GODEBUG=asynctimerchan=1, and stayed at 1.2 MiB under Go 1.23 timer semantics. Retention depends on the main module's go line, GODEBUG or a pre-1.23 toolchain, not on this module's go directive. The 200,000-packet figure was not repeated. Ad hoc measurement (no committed benchmark yet); see [W18](#w18-stop-per-packet-polling-timers). |
| PERF-3 | Reproduced | M5 above. |
| PERF-4 | Reproduced | Pool MaxIdle=4 and unbounded MaxActive confirmed. Rerunning the original workload (64 goroutines × 200 SendAll, 12,800 publishes) opened 2,350–2,556 new connections in three runs, consistent with the original 2,259. Optimal sizing depends on concurrency and acceptable queueing. Ad hoc measurement (no committed benchmark yet); see [W19](#w19-size-the-redis-publishing-pool). |
| PERF-5 | Reproduced | Polling Default.Client has a nil Transport and shares http.DefaultTransport (MaxIdleConnsPerHost 2). During 1,000 localhost broadcasts, 1 polling client opened no new TCP connections and 10 clients opened 2,791–3,418 across runs. Under sustained broadcasts, 10 clients failed with "can't assign requested address" after 2,784–6,722 broadcasts (about 9–13 s) on darwin in every run; the same load on a transport with MaxIdleConnsPerHost=64 completed (20,000 × 10). Exhaustion depends on the connection rate relative to the ephemeral range and TIME_WAIT (16,384 ports and about 30 s with darwin defaults), not on the client count alone. Ad hoc measurement (no committed benchmark yet); see [W20](#w20-give-the-polling-client-its-own-http-transport). |
| PERF-6 | Source-confirmed | Memory fan-out calls each socket's Emit, which prepares its packet separately; Redis also encodes its publication envelope. |
| PERF-7 | Source-confirmed | Normal and volatile sends call WithHeader even without an ACK; it appends/copies the payload into a new text frame. |
| PERF-8 | Source-confirmed | Each rcWrapper/wcWrapper starts a goroutine and a 30-second timer. |
| PERF-9 | Source-confirmed | roomSocket obtains and scans a Sockets snapshot. A replacement lookup must preserve namespace-generation and room-operation admission checks. |
| PERF-10 | Source-confirmed | Repository benchmark functions cover Engine.IO codecs/payloads, not Socket.IO parser/server/adapters. The follow-up benchmark was temporary. |
| PERF-11 | Reproduced | Reran the review's DecodeStruct benchmark: `v1.4.2` about 3.5–3.8 µs per packet, the previous master (`3dd094d`) and `ebc9f5e` both about 4.9–5.3 µs, i.e. 37–42% slower than `v1.4.2` across runs, with no change from #143 (the decoder is unchanged). All three make 44 allocs/op, and current B/op is lower than `v1.4.2`. `v1.4.2` also allocates a bufio.Reader for non-byteReader frames, so the regression comes from the extra JSON passes (json.Valid, RawMessage split, per-argument Unmarshal), not from the per-packet bufio.Reader. Each `3dd094d` and `ebc9f5e` figure is an ad hoc measurement (no committed benchmark yet); see [W25](#w25-remove-extra-json-passes-from-decoding). Each `v1.4.2` figure is a historical ad hoc measurement; not reproducible by a committed benchmark. |
| PERF-12 | Source-confirmed | broadcastMembers builds/populates a seen map for a single room too. |
| PERF-13 | Reproduced | The original harness measured 7.00 goroutines per idle websocket connection on both `3dd094d` and `ebc9f5e`: 6 started by the library (engineio close watcher, header reader, serveRead dispatcher, serveError, serveWrite, observeEngineClose) plus net/http's connection goroutine. `v1.4.2` measured 4.00 (3 plus net/http). The count did not change in #143. Each `3dd094d` and `ebc9f5e` count is an ad hoc measurement (no committed benchmark yet); see [W26](#w26-reduce-the-idle-connection-footprint). The `v1.4.2` count is a historical ad hoc measurement; not reproducible by a committed benchmark. |
| interop-1 | Reproduced | With b64=1, an outbound binary payload still produced a binary websocket frame. Inbound text `b4AQI=` closed the session with invalid Engine.IO packet type 50. The upgraded variant was not separately probed. |
| interop-2 | Source-confirmed | JSON payload encoding appends newline; OPEN uses JSON encoding with newline. PROTOCOL.md does not describe it. |
| interop-3 | Source-confirmed | Go requires octet-stream or text/plain with UTF-8 charset. Installed reference Engine.IO treats every non-octet-stream request as text. |
| interop-4 | Source-confirmed | Middleware/connect errors are serialized via err.Error(); no Node-style err.data interface exists. |
| interop-5 | Source-confirmed | Argument conversion failure returns before handler invocation/ACK generation; no documented ACK response for that failure. |
| interop-6 | Source-confirmed | Websocket conn.Close closes Gorilla's underlying connection without first writing a close control frame. |
| interop-7, T3 | Partial | No dedicated periodic JS heartbeat scenario or raw-conformance ping assertion was found. PARITY.md:20 names engine/session tests and raw conformance tests together as the heartbeat evidence; it does not promise that both cover it. Every conformance server uses the 25 s default ping interval, and no JS scenario stays connected long enough to send a heartbeat; the Redis polling run lasts about 33 s, but almost all of that is teardown after the clients close. JS heartbeat behavior is supported only by the original review's ad hoc 200 ms soak, which is not part of the repository's tests. |
| interop-8 | Reproduced timing, cause not isolated | The mixed-cluster polling subtest took 33.09 s versus 3.30 s for websocket. It passed. The exact teardown cause and suggested process.exit fix were not validated. The recommendation's other alternative, closing the clients before `io.close()`, is already present: the `finally` block (`redis-cluster.js:199-200`) closes every client socket and manager first. |
| interop-9 | Source-confirmed | Invalid packet closure is implemented/tested; CORS and that Socket.IO wire behavior are absent from PROTOCOL.md. |
| T1 | Reproduced coverage gap | Fresh cross-package coverage reports isJSONEmpty at 0.0%. Merely finding omitempty tags in decoder tests does not cover encoding. |
| T2 | Reproduced in server error path; partial overall | Sending malformed Socket.IO packet `49` over websocket left one frame-reader nag goroutine after disconnect and Server.Close (baseline zero). The source timer bounds that nag to 30 s; this is distinct from B1's permanent deadlock. No leak gate found. The broader fixture/client leak inventory was not reproduced. |
| T4 | Source-confirmed | Registering /three invokes Redis construction synchronously after Redis is stopped. The bound is in construction/subscription code; this is not an executing user OnConnect callback. Redis.New already documents its ten-second construction wait, although V1.md does not state that duration. |
| T5 | Partial | Encoder and round-trip fuzz targets are absent. The decoder package `engineio/parser/internal/packet` has no tests of its own, but `TestPacketDecoder` and other `engineio/parser` contract tests exercise it through `NewPacketDecoder` (89.5% cross-package coverage, `NextReader` 75.0%), so that subclaim is not a material gap. |
| T6 | Source-confirmed | Inherited pauser tests contain sleeps; no flakiness claim was independently established. |
| REL-2 | Confirmed snapshot | Local v1.5.0 cache records commit bb47ddac681e. GitHub tag, proxy and sumdb currently return 404; this does not prove that nobody has ever cached that version. A fresh cache is needed for release consumption checks; no cache was deleted. |
| REL-3 | Confirmed snapshot | Local identity is Probe/probe@example.invalid. Two master commits carry its Co-authored-by trailer: ea920ae (#139) and 3dd094d (#142). Configuration was not changed. |
| REL-4 | Source-confirmed | stress-v1 runs only on pull_request (ci.yaml:378). CONTRIBUTING.md "Before tagging v1" step 1 (lines 43-44 at `0fa1ead`) counts race/stress tests as part of "the full CI matrix" before tagging, but push and schedule runs never execute stress-v1, and the workflow has no manual trigger. Only a pull request that selects a full v1 run covers every stress package. |
| REL-5 | Confirmed snapshot | GitHub protection API returned only shared-file drift and the two conformance checks, strict=false. No protection settings were changed. |
| REL-8 | Reproduced | linux/386 test compilation fails on the untyped large integer in client/connection_typed_test.go:66. SchemaOf[int] also cannot represent that test's value on 386; changing just one assertion is insufficient. |
| REL-9 | Confirmed | golangci-lint is unpinned (`version: latest` at ci.yaml:244 and :298, `@latest` at ci.yaml:505), and so is govulncheck (`@latest` at ci.yaml:258 and :311). Every JavaScript action in use (checkout@v4, setup-go@v5, setup-node@v4, golangci-lint-action@v8 and the pinned upload/download-artifact v4 SHAs) declares `using: node20`; the composite benchmark-report actions do not (the root action wraps `marocchino/sticky-pull-request-comment` v3.0.5, which declares `node24`). Master CI run 39599568659 (0fa1ead) carries GitHub's "Node.js 20 is deprecated" annotation for checkout@v4 and setup-go@v5. Newer major versions of every one of these actions exist. |
| REL-11 | Snapshot / editorial | As of 2026-10-22, GitHub has 131 branches including master, which matches the original's 130 other branches; release/v1.5.0 and v1-h/release-notes remain. The 170 local remote-tracking branches (171 lines from `git branch -r`, including origin/HEAD) include 39 already deleted on GitHub, which `git fetch --prune` removes. Neither count shows which branches are stale. |
| REL-12 | Source-confirmed | The TODO remains; the write lock's concurrency purpose matters to B1 (see W1 step 4). |

### Checks performed in the follow-up

| Check | Result |
| --- | --- |
| `go test -race -count=1 ./...` | Passed. |
| `make gates sync-check` | Passed architecture, public-role/lifecycle fixtures and shared-file checks. |
| `go test -count=1 -coverpkg=./... -coverprofile=<temporary file> ./...` | Passed; combined statement coverage 93.5%, isJSONEmpty 0.0%. The original 93.6% is not a stable exact result. The exported-function coverage gate was not repeated. |
| `go test -race -count=1 -tags conformance -json ./internal/conformance/...` | 62 test/subtest results passed with socket.io-client 2.5.0 and a dedicated Redis server; package elapsed 45.544 s. |
| `GOOS=linux GOARCH=386 go test -exec=true -run '^$' ./...` | Expected build failure in client tests; foreign binaries were not executed. |
| Temporary `TestReview*` probes with `-race` | B1 lock cycle, M4 callback rejection, M5 stalled publication, C3 duplicate SID, interop-1 websocket base64, and T2 server frame-reader retention demonstrated. These probes assert the presence of current defects, not successful fixes. Ad hoc probes, not retained. |
| Temporary `BenchmarkReviewEncode100Structs` | Regression reproduced against 3dd094d; three samples per revision, details above. Ad hoc measurement (no committed benchmark yet), not retained; see [W2](#w2-add-socketio-level-benchmarks) and [W3](#w3-restore-encoder-performance). |

Not repeated: full lint/vet/example matrices, stress, fuzz campaigns, client 1.7.4
conformance, examples-node, vulnerability scans, apidiff and the other historical
performance measurements. The reported historical CI run and toolchain version
were not independently audited. No production fix, tag, release, branch deletion,
git-identity update or repository-settings change was made.

### Fact-check on 2026-10-22

Re-run against `0fa1ead` (code identical to `ebc9f5e` and `7311fbe`) in disposable
`git archive` copies and external probe modules; no tracked file was changed. The probes
and benchmarks of both passes were ad hoc and not retained in the repository.

| Check | Result |
| --- | --- |
| B1 probes with `-race` (ping-only, message-only, silent control, polling-to-websocket upgrade) | Deadlock with four identical blocked stacks in every non-silent variant; silent client disconnected normally |
| Live mixed Redis fleet (two upstream `v1.7.0`, one fork) | Namespace-wide and same-named-room fork broadcasts stop upstream dispatchers; upstream `RoomLen`/`Rooms` then block |
| Redis SIGSTOP, pool churn, `BroadcastTo*` after Close, registration with Redis down | `PUBLISH` unbounded; 2,350–2,556 new connections per 12,800 publishes, an ad hoc measurement (no committed benchmark yet; see [W19](#w19-size-the-redis-publishing-pool)); bool true after Close; registration 10.0 s |
| `apidiff` (root, parser, `engineio` tree), alias AST count, `gocyclo`, `unsafe.Sizeof` | Original counts reproduced byte for byte; 24 aliases; complexity 45; 88 B × 64 |
| Encode, end-to-end broadcast, decode, goroutines-per-connection, polling timer retention, polling churn and sustained load | M6, PERF-2, PERF-5, PERF-11 and PERF-13 rows above; each figure is an ad hoc measurement (no committed benchmark yet), and each `v1.4.2` value a historical ad hoc measurement; not reproducible by a committed benchmark; see [W2](#w2-add-socketio-level-benchmarks) |
| `go test -race ./...`, `make gates sync-check`, `make cover-check`, conformance with `socket.io-client` 2.5.0, `GOARCH=386` build | Pass; pass; 93.5–93.6% with 0 uncovered exported functions; 62/62 in 45.2 s; fails only at `client/connection_typed_test.go:66` |
| CI audit (runs 39589689698 and 39599568659), branch protection, tag/proxy/sumdb lookups | Both runs green, Go 1.27.3, no called vulnerabilities; three required checks; v1.5.0 absent everywhere except the local module cache |

Still not repeated: the slow-consumer eviction run, the 20,000-namespace heap (C2), the
200,000-packet timer figure, full fuzz campaigns, `socket.io-client` 1.7.4 conformance,
`examples-node`, Go 1.22/1.21 builds, stress, the go1.27 jsonv2 variant, and any Linux,
Windows or 32-bit runtime.

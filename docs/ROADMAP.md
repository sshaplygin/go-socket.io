# Roadmap

Scope approved: 2026-09-28. Updated: 2026-10-10. Owner: Sam Shaplygin.

This file owns scope, dependencies, implementation contracts and release gates.
Current implementation: [PROTOCOL.md](PROTOCOL.md). Completed changes:
[CHANGELOG.md](../CHANGELOG.md). Development and plan-review workflow:
[CLAUDE.md](../CLAUDE.md).

## Baseline

The starting fork of `googollee/go-socket.io` supports Socket.IO protocol v4 over
Engine.IO v3. Stage 0, toolchain/CI work and logger tasks 1.2/1.2a have landed;
remaining work starts at stage 1 below. Existing application APIs stay at the
repository root (the v1 module); v2 is a new core/API in `v2/` (*Repository layout*).

The Engine.IO v4 preparation from `codex/eio4-payload` (inspected at
`ee682282997b19309036e9c6fef6e248e06bc230`) has landed in `master` and is in use: the polling
codec is `engineio/payload`, the WebSocket codec and framing are in `engineio/transport/websocket`,
and the standalone `eio4-websocket` and `ws-bench` modules are deleted. Stage 2.1 owns the
remaining integration work below.
The other prepared experiments are listed in Stage 2 *Prepared components*.

## Decisions

| Area | Decision | Contract owner |
| --- | --- | --- |
| Core | Own Engine.IO/Socket.IO core; no dependency on or rebase onto `zishang520/socket.io` | 2.0–2.3 |
| Protocol | v2 supports Engine.IO v4 / Socket.IO protocol v5; old clients (Engine.IO v3 / Socket.IO v4) use the v1 module at the repository root | 2.1, 2.3 |
| API | Generic `Event[T]` / `AckEvent[T, R]` from the first v2 implementation; explicit raw escape hatch; no reflection-based dispatch | 2.0, 2.3 |
| Modules | v1 is the repository root module `github.com/sshaplygin/go-socket.io`, independent of the upstream module; v2 is the module in `v2/`, `github.com/sshaplygin/go-socket.io/v2`; adapters and contrib are separate modules under `v2/` | 2.5, 4b, 5 |
| Go | Go 1.22 minimum for runtime modules; compatible dependencies pinned and minimum tested; build tools may use stable Go | Stage 1 DoD, 2.5 |
| Transport | `gobwas/ws` + `wsutil` on server and client; standard `http.Handler` integration | 2.1 |
| Brokers | Redis `go-redis/v9`, Node non-sharded adapter wire compatibility; NATS core pub/sub, no JetStream | 4b |
| Logging | Application `slog.Handler` through an injected logger; instance routing, process-wide level override | 1.L (v1 records), 2.4 |
| Observability | Nil-able hooks and logging in the root package of the v2 module; OTel bridge in `v2/contrib/otel`; no OTel dependency in the v2 module | 2.4 |
| Admin UI | Required final product stage, separate `v2/contrib/admin`, unchanged official UI; commands disabled by default | 5 |
| Benchmarks | Final comparative campaign after all product features: our v2, existing Go and official JS/TS implementations | 6 |
| Client packages | Go client as its own package on both lines: v1 gets an additive `client` package in the repository-root module (its root `Client` stays as a deprecated wrapper), made inside Stage V1 so that it ships in `v1.5.0` (owner decision of 2026-10-10 replaces "scheduled last"); v2 keeps `client/` in the v2 module, scheduled last, no separate `go.mod` | 2.3C (v2), V1 and 7 (v1) |
| Documentation | English; each contract has one owner; other sections refer to it | CLAUDE.md |
| Layout | Owner decision of 2026-10-10: the repository root is the v1 module (the v1 runtime restored at the root); v2 lives in `v2/` as its own module. Replaces "master root = v2 skeleton, v1 on branch `v1.x`" | Repository layout, Repository restructure |
| v1 completion | Owner decisions of 2026-10-10: the v1 line is finished and released (`v1.5.0`) before the v2 work continues; in-memory parity, examples and the finished Go client first, Redis parity as the next v1 minor; the matrix is [PARITY.md](PARITY.md) | Stage V1 (D1 to D8) |
| Branch `v1.x` | Kept for now; receives no new work after the restructure. Its fate is the owner's later decision: deleting it is outward-facing and is neither done nor scheduled here | Repository layout |
| Tags | No tag of any module until `master` has full support of the v1 protocol line (Socket.IO v4 / Engine.IO v3) with example implementations; `v1.5.0` is then tagged on `master` on the owner's explicit order, v2 tags after it. Replaces the M4 trigger; "full support" is the Stage V1 scope (MV1) | Repository layout, M4 |

### Repository layout

Target layout of the three decisions above. Until step B of *Repository restructure*
merges, `master` still has the v2 skeleton at the root and `v1.x` still holds v1.

| Where | Module | Holds |
| --- | --- | --- |
| repository root | `github.com/sshaplygin/go-socket.io` (v1) | the `v1.x` tip tree: v1 server and client, `engineio/`, `parser/`, `logger/`, `_examples/` (v1 examples, each with its own `go.mod`) |
| `v2/` | `github.com/sshaplygin/go-socket.io/v2` | everything of Stage 2 and later: the API skeleton, `engineio/`, `parser/`, `logger/`, `adapter/`, `client/`, `adapters/*`, `contrib/*`, v2 `_examples/`, `_experiments/`, `testdata/`, `Makefile`, `.golangci.yml`, `CHANGELOG.md`, `README.md` |
| shared, root only | none | `docs/`, `.github/`, `CLAUDE.md`, `CONTRIBUTING.md`, `LICENSE`, `.gitignore` |

The two modules are independent: neither imports nor requires the other, there is no `go.work`, and
step C enforces this with commands.

Owners of the shared files. `docs/` is one tree for both lines (`ROADMAP.md`, `PROTOCOL.md`,
`API.md`, and the later v2 files); a v2-only document says so in its title. `.github/` holds one
workflow set and one `dependabot.yml` that cover every module. `CLAUDE.md` and `CONTRIBUTING.md`
describe both. `README.md` at the root is the v1 README with one link to `v2/README.md`, which is
what pkg.go.dev renders for the v2 module. `CHANGELOG.md` is per module: the root file records v1,
`v2/CHANGELOG.md` records v2, and an entry is never repeated in the other. `LICENSE` stays at the
root only: the extracted module of a `v2.0.0` tag on a scratch repository with a root `LICENSE` and a
`v2/go.mod` held `LICENSE` and `go.mod` (`go mod download -json`). `Makefile` and `.golangci.yml` exist once per module.

**Path convention.** Every path in the Baseline, in Stage 2 to Stage 6, in the Execution rows 2A
to 6C and in the Stage 1b target tree is relative to `v2/` unless it starts with `root:` or names a shared
file above, and "root" there (root package, root `go.mod`, root `v2.1.0`) means the root of the
v2 module. `CHANGELOG.md`, `Makefile` and `README.md` in those stages are the `v2/` files, and a `make`
target there is `make -C v2 <target>`. A path or tag that already starts with `v2/` or `root:`, and
a full module path (`github.com/...`), is written out in full and is not shifted again
(`v2/README.md` in 2.5 is the file, not `v2/v2/README.md`).
A module path written `.../<dir>` is `github.com/sshaplygin/go-socket.io/v2/<dir>` (the
`/vN` suffix it already carries stays). In Stage 2 to Stage 6 text a tag written `<dir>/vX.Y.Z` is
`v2/<dir>/vX.Y.Z`, and the v2 module itself is tagged `vX.Y.Z`. The Tag column of *Milestones* is
the exception: it gives the full git tag names, one notation, and is not shifted. Stage 1, Stage 7
and Execution row 7A paths (`client/`, `_examples/client/`, `client.go`, `connection.go`) are
relative to the repository root (the v1 module), Stage 1 in its pre-1b names. In those
places, in the Decisions table, in *Milestones* and in *Out of scope*, "root" and "the root
module" mean the repository-root module `github.com/sshaplygin/go-socket.io` (v1) unless the text
names `v2/` or the v2 module; a row there that means the v2 root says so in words.

**Tag forms.** Verified on a scratch repository with `go.mod` at the root, in `v2/` and in
`v2/contrib/otel`, resolved through `file://` rewriting of the module's GitHub URL with
`GOPROXY=direct` and an empty module cache (`go1.25.5`):

```sh
T=$(mktemp -d); M=github.com/sshaplygin/go-socket.io
git init -q -b master $T/repo; cd $T/repo
mkdir -p v2/contrib/otel; printf 'module %s\n\ngo 1.22\n' $M >go.mod
printf 'module %s/v2\n\ngo 1.22\n' $M >v2/go.mod
printf 'module %s/v2/contrib/otel/v2\n\ngo 1.22\n' $M >v2/contrib/otel/go.mod
git add -A; git -c user.name=t -c user.email=t@t commit -qm init
git tag v1.5.0; git tag v2.0.0; git tag v2/contrib/otel/v2.0.0
printf '[url "file://%s/repo"]\n\tinsteadOf = https://github.com/sshaplygin/go-socket.io\n' $T >$T/gitconfig
export GIT_CONFIG_GLOBAL=$T/gitconfig GOMODCACHE=$T/mod GOFLAGS=-modcacherw GOPROXY=direct GOPRIVATE=github.com/sshaplygin/\*
cd $T; for q in $M@v1 $M/v2@v2 $M/v2/contrib/otel/v2@v2; do go list -m $q; done
```

```text
github.com/sshaplygin/go-socket.io v1.5.0
github.com/sshaplygin/go-socket.io/v2 v2.0.0
github.com/sshaplygin/go-socket.io/v2/contrib/otel/v2 v2.0.0
```

So the forms are `v1.X.Y` (root), `v2.X.Y` (the v2 module, no prefix) and `v2/<dir>/vN.X.Y`
(a nested module, with the module path ending in `/vN` for N >= 2). The same repository with the tags
`v2/v2.0.0` and `contrib/otel/v2.0.0` instead resolves none of the three queries ("no matching
versions"), so those two spellings are not used.

**Links while no tag exists.** `go get github.com/sshaplygin/go-socket.io@master` for v1 and
`go get github.com/sshaplygin/go-socket.io/v2@master` for v2 (a pseudo-version on the same scratch
repository without tags: `v2.0.0-<date>-<sha>` for v2). pkg.go.dev links carry no version
(`.../go-socket.io[/pkg]`, `.../go-socket.io/v2[/pkg]`). The v1 README sentence containing
`until a release` stays and now names `master`.

**Branch and tag policy.** All work lands on `master` by PR: a v1 change edits root files, a v2
change edits `v2/` (CI path filters follow). `v1.x` stays at its current tip (recorded as
`$V1TIP` in step B), no PR targets it, and the workflows and Dependabot configuration of `master` do
not target it (the forward-port rule of `CONTRIBUTING.md` ends). No tag of any module is created, and none is scheduled, until the owner declares that `master`
has full support of the v1 protocol line (Socket.IO v4 / Engine.IO v3 in the library's own
terminology) with example implementations. The declaration is the owner's; before making it
the owner reads items that already exist: `make examples` passes (the `_examples/*/chat.go`
copies are identical and build); `docs/PROTOCOL.md` is true for its Engine.IO v3 and
Socket.IO v4 sections; the root `CHANGELOG.md` `## Unreleased` section is ready to be renamed.
`v1.5.0` is then tagged on `master` only on the owner's explicit order, by the release commit of
`CONTRIBUTING.md`, followed by the Stage 1 tag-time gates; v2 tags come after it, each on the owner's order
(M3 onward). The Stage V1 MV1 DoD and Acceptance are the checks behind "full support"; v2 work resumes
on the owner's order after MV1 and does not wait for the tag.

## Execution and parallel work

Execution order: **1 → 1b → 2A → R → V1 → V1R → 2B … → 3 → 4b → 5 → 6**, where 2A is the landed
Stage 2.0, row R (the restructure) follows it, Stage V1 completes and releases v1 before the
rest of Stage 2 (owner decision of 2026-10-10), V1R (Redis parity) follows MV1, the PR SYNC
(shared-file drift check, D14) runs beside V1 and merges before 2B, and Stage 7 is
executed inside V1 as PR V1-9. Whether V1R precedes the Stage 2 continuation or runs beside it
(its files are the root module's, the continuation's are under `v2/`) is the owner's order.
Admin UI remains the last product stage and starts after M5; the final benchmark campaign after M6.
Branch `v1.x` was cut between stages 1 and 1b (1b step 0) and is frozen (*Repository
layout*); the restructure below (row R) moves v1 to the repository root and v2 to `v2/`.
Stage 2 merges stop when step A starts, on the owner's order after the PR that records the
restructure has merged, and resume on the owner's order after MV1 (Stage V1), not after step C;
until the stop order they proceed as before and move with the recipe of step A. No tag gates any stage or milestone: tags follow the owner's declaration, and the checks that need
a tag run at tag time (*Milestones*).
Numbers identify scope, not permission to start before a dependency passes.
A prerequisite marked as a gate means its tests and integration must pass, not only
that a draft API exists. Tasks in the same row may run concurrently in separate
worktrees. One integrator owns shared API files, module manifests and CI workflows;
workers submit changes to these files through that integrator.

| Wave | Prerequisites | Independent work / write ownership | Join gate |
| --- | --- | --- | --- |
| 1A | landed infrastructure | 1.R Redis internals (`redis_broadcast.go`); 1.B queue and close internals (`connection.go`, `broadcast.go`, `errors.go`, the socket.io goroutines and close paths in `server.go` (`serveConn`, `serveRead`, `serveWrite`, `serveError`) and `client.go` (`Connect`, `Close`, `clientRead`, `clientWrite`, `clientError`), and the disconnect handlers in `connection_handlers.go`); 1.S session/server fixes (`engineio/session`, `engineio/server.go`, `server.go`; landed) | component regression tests pass |
| 1I | 1A | integrator wires Redis construction errors through `namespace_handler.go` and `server.go`; wires `WriteBufferSize` and the drain deadline (`PingTimeout`) through `engineio/server_options.go`, `server.go` and `client.go`; runs the 1.B slow-client test against the Redis broadcast; also edits `connection.go` (connect-failure path, option wiring in `newConn`, `Conn.Close` godoc), the connect-failure path in `connection_handlers.go`, `namespace_handlers.go`, the session hand-off in `engineio/server.go`, `namespace_conn.go` (godoc only) and `CHANGELOG.md`; contract in the 1I item | integrated bug tests (the 1I item's tests) and root build pass |
| 1B | 1I | 1.L Go files (logging, session close reasons, `logger` godoc; no Markdown except `CHANGELOG.md`); 1.D `README.md`, `engineio/README.md`, `logger/README.md`, `CLAUDE.md`, `CONTRIBUTING.md`; each writes its own `CHANGELOG.md` entries | M1 checks and v1 compatibility |
| 1C | 1B | 1.K known-limitation notes: the godoc of `Server.Adapter`, `RoomLen` and `Rooms` in `server.go` and the `### Known limitations` subsection of `CHANGELOG.md`; contract in the 1.K item | 1.K check, then M1 checks |
| 1b | stage 1 and the 1.D link-form commit merged, `master` green (the cut commit `$CUT`, which 1b records); branch `v1.x` cut from it without a tag (1b step 0) | one refactor owner, who is also the integrator for the CI, Dependabot and `CHANGELOG.md` files of steps 0b to 0d; moves/merges applied sequentially | M1b: the Stage 1b DoD, `v1.x` gates and Acceptance blocks |
| 2A | M1b | 2.0 owner removes the legacy root runtime, v1 broadcast and redigo atomically with the new API skeleton, builds compile fixtures and freezes shared interfaces | G2: fixtures compile, package graph acyclic, no unresolved API signatures; evidence: `make g2` (2.0 *G2 record*) |
| R | 2.0 on `master`; the owner's order | one integrator: steps A to C of *Repository restructure*; the 2.1, 2.2 and test-stress work stopped by the freeze resumes after MV1 (its open PRs are replayed after C and wait) | step C commands pass on the merged `master` |
| V1A | row R step C passed; V1-0 merged | V1-1 (`engineio/payload`, `engineio/transport/polling`, its own new `engineio/polling_*_test.go` files) and V1-2 (`engineio`, `engineio/session`, and the `SetReadLimit` methods of `engineio/transport/polling` and `engineio/transport/websocket`, written after V1-1 has merged) in separate worktrees | per-PR DoD (Stage V1) |
| V1B | V1A | V1-3 to V1-7 in order: one integrator, the root files and `parser` are shared; beside them V1-16 (after V1-4 has merged: V1-4 is the single owner of the close-cause plumbing in `engineio/session` and its aliases in a new file of `engineio`, K24, and of the writability report that V1-6's `Volatile` reads (`Writable` on `*session.Session`, the engine client of `engineio/client.go` and the connections of `engineio/transport/polling` and `engineio/transport/websocket`, K16), the one `engineio/` edit among V1-3 to V1-7; V1-16 and V1-17 rewrite the same file, so they rebase on it, and V1-17's `error` hook reports the cause V1-4 exposes; V1-16 also edits `engineio/session/session_manager.go` and its mirror `v2/engineio/session/manager.go`, `Manager.All`, the one `v2/` file a V1B PR touches, by the rule of *Shared-file drift check*), then V1-17, then V1-18 (`engineio/` and its transports, which V1-3, V1-5, V1-6 and V1-7 do not edit; their root edits, `Server.Engine` and the `CHANGELOG.md` entries, go through the integrator) | per-PR DoD |
| V1C | V1B | V1-8, then V1-20 (`.github/`, the `Makefile`, `CLAUDE.md`, the conformance directory, `internal/parity`; the V1-8 author integrates) | the `conformance` job and the `parity-check` and `cover-check` step green on `master` |
| V1D | V1C | V1-9, then V1-10, then V1-11 (`client/`, root `client.go`, `connection.go`, and for V1-10 `engineio/client.go`, the engine client's close reasons, and `engineio/dialer.go`, `Dialer.Upgrade`, `### V1-10 contract`; for V1-11 the same two `engineio/` files again, `Dialer.OnPing` and `OnPong` and their calls in the engine client, `### V1-11 contract`); beside them V1-19 (new files under `internal/conformance` and its `testdata/node`, no workflow edit) | Stage 7 DoD, then per-PR DoD; the V1-19 block |
| V1E | V1D (V1-12 may start after V1-8) | V1-12 and V1-13, one directory per example; beside them V1-15 (needs V1-2, V1-6 and V1-11: `engineio` options and handshake, `engineio/transport/websocket`, `engineio/transport/polling`, the new `engineio/transport/compress.go`, `engineio/session` (`Session.NextWriterCompress`, the `SetSessionID` call of `session.New`), `engineio/client.go` (the engine client's `NextWriterCompress`), the root emit and broadcast flags and its queue element, `client/`, new files under `internal/conformance`; `parser` is unchanged). The V1-12 author integrates the wave's shared files (`Makefile`, `.github/workflows/ci.yaml`, `CLAUDE.md`): V1-13's `Makefile` edit merges after V1-12, and V1-15 edits none of them, since `make conformance` already runs every tagged test of `internal/conformance` | `make examples`, then `examples-node` once per gate client (the two lines of the per-PR checks of V1-12 and V1-13); the per-PR DoD and the V1-15 block (Stage V1) |
| V1F | V1E | V1-14 release preparation, no tag | MV1 DoD and Acceptance (Stage V1) |
| SY | row R step C passed | the PR SYNC (Stage V1, *Shared-file drift check*): root `Makefile`, `.github/workflows/ci.yaml`, `.github/sync-manifest`, `CLAUDE.md`; it runs beside V1A or V1B, whose PRs are not planned to edit those files, and never beside V1C or V1E, which edit `ci.yaml` and the `Makefile` | the SYNC DoD; the `sync-check` job green on `master` |
| 2B | G2, row R merged (its step C passed), MV1 accepted and SYNC merged | 2.1 Engine.IO (`engineio/`); 2.2 memory adapter (root `adapter.go`, against the frozen `LocalSockets`, no edit of `namespace.go`); 2.3P Socket.IO codec (`parser/`) | all three integrate against frozen contracts |
| 2C | 2B | 2.3S server/namespace runtime (root socket files, including the body of `Namespace.LocalSockets`); 2.3C client (`client/`) | typed Go/Node tests and lifecycle tests (including `TestNamespaceReadiness`, 2.3S) pass; dispatch baseline recorded |
| 2D | 2C | one owner propagates instance loggers across runtime packages | logger precedence/isolation tests pass |
| 2E | 2D | 2.4E Engine.IO hook fire points; 2.4S Socket.IO hook fire points; 2.4O OTel bridge (`contrib/otel`) against frozen hook fixtures | all hook, span, metric and overhead checks pass |
| 2F | 2E | 2.5T conformance/framework tests; 2.5D migration/examples/docs | M3 acceptance; publication verification at tag time |
| 3A | M3 | freeze chat event schema; then server, browser/CLI and load client in separate directories | M4 single-server acceptance |
| 4A | M4 | freeze codec fixtures and adaptertest cases; then Redis and NATS modules independently | each passes shared conformance suite |
| 4B | 4A | cluster chat profile; mixed Go/Node Redis tests in separate test directories | M5 cluster acceptance |
| 5A | M5 | freeze admin snapshot/capability and command-result contracts, and pinned UI fixtures | admin contract gate |
| 5B | 5A | core observers/snapshots; admin wire/auth module; Redis/NATS capability extensions; browser tests against fixtures | integrate and run M6 acceptance |
| 6A | M6 | benchmark owner freezes versions, workload matrix, resource budgets and result schema | comparison contract and correctness checks pass |
| 6B | 6A | our-v2, existing-Go and official-Node runners in separate directories; shared load generator owned by integrator | runners produce equivalent traffic/results |
| 6C | 6B | measurements sequentially on reserved hosts; analysis/report follows complete raw results | M7 reproducibility and report acceptance |
| 7A | V1-8 merged (wave V1D; inside Stage V1) | 7.1 layering and package (`client/`, `_examples/client/`, root `client.go`, `connection.go`, `connection_handlers.go`, `namespace_conn.go`, `errors.go`) and 7.2 tests and docs, as commits of one PR to `master`: the wrapper switch breaks the root tests until they are split, so no PR head may carry one without the other | Stage 7 DoD and Acceptance; the tag is the owner's order at MV1 |

Rows 1A and 1I, and the Stage 1 items, name files by their pre-1b paths (after the
restructure: the root module); the Stage 1b source-to-target map owns the new names (after the
restructure: the `v2/` tree). Rows 2A to 6C follow the path convention of *Repository layout*; row 7A names paths of the repository root (the v1 module).

Mocks permit development against frozen contracts; they do not satisfy integration
or release gates. A contract change updates its owning section and fixtures before
consumers continue. Every join builds/tests the whole v2 module and affected child
modules; re-run affected gates after merges. Each work unit supplies its gate evidence.
Apply the three-agent validation workflow in CLAUDE.md after plan edits; do not
replace unresolved findings with optimistic estimates.

## Repository restructure

Moves `master` from "root = v2 skeleton, v1 on `v1.x`" to *Repository layout*, in the steps
below, in this order. The PR that records this section moves no code, creates no tag and
deletes no branch. Gate blocks run with `bash` and `set -e`, rules as in the Stage 1b DoD.

**A. Freeze.** Starts on the owner's order, after the PR that records this section has merged.
Until step B merges, no PR merges into `master` except step B, and none merges into `v1.x`: the
step B tree is the `v1.x` tip recorded at the start, so a commit that lands on `v1.x` later would
be dropped silently and the branch then declared frozen. Dependabot opens PRs with
`target-branch: v1.x` (two entries in `dependabot.yml`) until B4 removes them; the open ones are
closed, not merged. Open or stopped work is not
merged into the old layout; it is replayed after step C, by its author, on the new `master`, and
merged on the owner's order after MV1:

| Work | State | After step C |
| --- | --- | --- |
| #60 `docs: add 2.3M, the opt-in MessagePack parser` | open, edits `docs/API.md`, `docs/PROTOCOL.md` and `docs/ROADMAP.md`; does not merge during the freeze | the recipe below (shared series only, no module patch); its `docs/ROADMAP.md` hunks conflict with this section's own edits (four regions: the Decisions *Protocol* and *Wire format* rows, the Execution rows 2E and 2F beside its new 2CM, the Milestones rows M2 to M6, the Out of scope `adapters/redis` entry; its 2.3M hunks in Stage 2 apply cleanly) and its `docs/PROTOCOL.md` hunk (line 165 to 168) may conflict with the B5 edit of line 157: both resolved by hand as stated after the recipe; the path convention applies to its text |
| #62 `feat(2.1): Engine.IO v4 handshake gate` | open, edits `engineio/` Go files, `CHANGELOG.md`, `docs/PROTOCOL.md` and `docs/ROADMAP.md`; does not merge during the freeze | the recipe below: Go paths become `v2/<old path>`, imports `.../v2/...` (the B3 command, applied to the patch text); its docs hunks apply cleanly to `master` plus this section's edits, only `docs/PROTOCOL.md` (lines 146 to 153) may conflict with the B5 edit of line 157 |
| 2.1 D1, the 2.2 memory adapter | stopped before a PR | restarted on `v2/` after MV1 |
| forward-port of the `test-stress` target and of PR #52 (`parser` Buffer placeholder numbers) | stopped; `v1.x` already has both | v1 side: restored by B2; v2 side: a new PR on `v2/` |

```sh
git fetch -q origin   # the two SHAs below are read from remote-tracking refs: stale refs record a wrong $OLDBASE
MISSING=$(for n in $(gh pr list --base master --state open --json number,author --jq '.[]|select(.author.is_bot|not)|.number'); do grep -q "^| #$n " docs/ROADMAP.md || echo "#$n"; done); echo "$MISSING"; test -z "$MISSING"   # prints each open PR without a row in the table above (the table is the list: add the row first); before the step B PR is opened
test -z "$(gh pr list --base v1.x --state open --json number --jq '.[].number')"   # nothing waits to merge into v1.x (close Dependabot PRs first)
V1TIP=$(git rev-parse origin/v1.x); echo $V1TIP   # the v1 tree B2 restores; the step B body records it
OLDBASE=$(git rev-parse origin/master); echo $OLDBASE   # the last commit of the old layout; the step B body records it
```

A plain `git rebase <new master>` of a branch cut from `$OLDBASE` is wrong, not only slow: B2
recreates root files with the names B1 moved (`engineio/server.go`, `parser/*`, `logger/*`), so
git does not follow the rename and applies the edit to the restored v1 file at the root, with or
without a conflict (a scratch repository: a branch editing `engineio/server.go` rebased this way
touched the root file only). Rebasing onto the B1 commit with `merge.directoryRenames` is not used
either: git moves no new file in a new subdirectory or a new top-level directory, and the second
rebase stops on an import block that B3 rewrites. The recipe replays the branch as patches, which
carry modified, new, deleted and renamed files alike, on a fresh branch from the post-step-C
`origin/master` (`bash`, not `zsh`):

```sh
BR=${BR:?the branch, e.g. origin/feat/x}; NEW=${NEW:?name of the new branch}; P=$(mktemp -d)
FROM=$(git merge-base ${OLDBASE:?recorded in step A and in the step B body} $BR)
git format-patch -q -o $P/v2 $FROM..$BR -- . ':!docs' ':!.github' ':!CLAUDE.md' ':!CONTRIBUTING.md' ':!LICENSE' ':!.gitignore'   # the module files
git format-patch -q -o $P/shared $FROM..$BR -- docs .github CLAUDE.md CONTRIBUTING.md LICENSE .gitignore   # the root-only files, the list of B1
compgen -G "$P/v2/*.patch" >/dev/null && perl -pi -e '$f=$1 if m{^diff --git a/(\S+)}; s#github\.com/sshaplygin/go-socket\.io(?!/v2)#github.com/sshaplygin/go-socket.io/v2#g if $f =~ /(\.go|go\.mod|\.toml)$/ && /^[ +-]/ && !/^(---|\+\+\+) /; undef $f if eof' $P/v2/*.patch   # the B3 rewrite, on the patch text
git switch -c $NEW origin/master
compgen -G "$P/v2/*.patch" >/dev/null && git am --directory=v2 $P/v2/*.patch   # an empty glob skips the line (#60 has no module patch)
compgen -G "$P/shared/*.patch" >/dev/null && git am -3 $P/shared/*.patch   # no --directory: these files stay at the root
test -z "$(git diff --name-only origin/master...HEAD | grep -vE '^(v2/|docs/|\.github/|CLAUDE\.md|CONTRIBUTING\.md|LICENSE|\.gitignore)')"   # nothing outside v2/ and the root-only files
```

The B3 rewrite is applied to the patches, not after the replay, because a context line with the
old module path never matches the rewritten file: `git am` stopped at an import block without it.
`--directory=v2` puts a new file, wherever it sits, under `v2/`; binary and `testdata` files are
in the patches (`format-patch` writes binary patches); `go.mod` and `go.sum` hunks land in
`v2/go.mod` and `v2/go.sum` (a `go.sum` line needs no rewrite); `README.md`, `CHANGELOG.md`,
`Makefile` and `.golangci.yml` hunks land in the per-module copies. A hunk in a file that this
section or B5 rewrote (`docs/ROADMAP.md`, `docs/PROTOCOL.md`, `v2/README.md`, `v2/CHANGELOG.md`) may
stop `git am`; when a long series stops, re-apply the root-only files as one diff, resolve the
conflict regions by hand, commit, and list the file in the PR that carries it
(`git am --abort; git diff $FROM $BR -- docs .github CLAUDE.md CONTRIBUTING.md LICENSE .gitignore | git apply --3way`;
a single stop: `git am --show-current-patch=diff`, edit, `git add`, `git am --continue`). A file on the root-only list that belongs in `v2/` is moved by hand and listed too. A
commit that touches both kinds of file becomes two commits with the same subject.

Replayed on the real branches in scratch worktrees, nothing pushed, on `origin/master`
(`1392afe`) with B1 to B3 of this section applied (B4 and B5 not written yet): #62 (`f59eff9`,
three commits, 13 files): both `git am` runs completed, the guard printed nothing, the 10 Go files
equal the branch files with the B3 command applied, and `go build`, `go vet` and
`go test -count=1 ./...` in `v2/` passed; the same command without the rewrite stopped at
`v2/engineio/transport/polling/server.go`. #60 (`67ab45e`): no module patch; on that `1392afe` alone the shared series
applied with `-3` and its diff equals the diff of the branch against `1392afe`, but on the head of
the PR that records this section `git am -3` stops in its first patch, and the one-diff form
leaves four conflict regions in `docs/ROADMAP.md` (Decisions, Execution, Milestones, Out of scope)
with `docs/API.md` and `docs/PROTOCOL.md` clean; #62 applies cleanly there. B4 and B5 are not
written yet: the `docs/PROTOCOL.md` conflicts with B5 are untested. A synthetic
branch (an import line added next to the rewritten ones, a new file in a new subdirectory of a
moved directory, a new top-level directory, a deleted file, a rename with an edit, a 2 KiB binary
file, a `go.mod` require, a `go.sum` line, and hunks in `CHANGELOG.md`, `docs/`, `.github/` and
`CONTRIBUTING.md`) landed with every file at its expected path, the binary file byte-identical.

**B. One PR, merged with its commits kept** (`CONTRIBUTING.md` rule 5: B1 must stay a
pure rename, so that `git log --follow` and review of the moves work). Subjects
`refactor(R.<n>): ...`; intermediate commits may not build, the head does.

- B1. Pure `git mv`, no content edit; the root `README.md` and `CHANGELOG.md` move too:

  ```sh
  mkdir v2
  for p in $(ls -A | grep -vxE '\.git|\.github|\.gitignore|docs|CLAUDE\.md|CONTRIBUTING\.md|LICENSE|v2'); do git mv $p v2/; done
  ```

- B2. The v1 tree at the root, from the recorded tip, as new files on top of the history:

  ```sh
  git restore --source=$V1TIP --staged --worktree -- . ':!:v2' ':!:.github' ':!:docs' ':!:CLAUDE.md' ':!:CONTRIBUTING.md' ':!:LICENSE' ':!:.gitignore'
  ```

- B3. The v2 module: `cd v2 && go mod edit -module github.com/sshaplygin/go-socket.io/v2` (this is
  where `v2/go.mod` gets its `/v2` path; 2.5 does not change it), then, from the repository root,
  every use of the module path in the `v2/` files of three kinds, `*.go`, `*go.mod` and `*.toml`,
  gets the `/v2` infix:

  ```sh
  git ls-files -z 'v2/*.go' 'v2/*go.mod' 'v2/*.toml' | xargs -0 perl -pi -e 's#github\.com/sshaplygin/go-socket\.io(?!/v2)#github.com/sshaplygin/go-socket.io/v2#g'
  ```

  The `(?!/v2)` guard keeps a path that already ends in `/v2` (the `module` line just edited) from
  becoming `/v2/v2`. The command covers imports, the `module`, `require` and `replace` lines of
  every `go.mod` (`v2/_examples/*` and `v2/_experiments/*` get `.../v2/_examples/gf` and
  `replace .../v2 => ../../`) and `v2/.deepsource.toml`. It leaves the Markdown files, which carry
  badge, pkg.go.dev and install URLs and history entries that B5 edits by hand. On a scratch tree it
  turned `module .../go-socket.io/_examples/gf` into `.../go-socket.io/v2/_examples/gf`, left
  `module .../go-socket.io/v2` as it was and left a `.md` URL unchanged. The three reads of
  `docs/API.md` in `v2/inventory_test.go` and `v2/frozen_test.go` become `../docs/API.md`.
  `go vet ./...` in `v2/` is clean.
- B4. CI and Dependabot: every `ci.yaml` job runs per module (`working-directory: v2` for the v2
  jobs, a path filter per module), the benchmark workflow covers both modules (below),
  Dependabot lists `/`, `/_examples/*`, `/v2`, `/v2/_examples/*`, `/v2/_experiments/*` (and
  `/v2/adapters/*`, `/v2/contrib/*` when those exist) and loses the two `target-branch: v1.x`
  entries; the branch triggers `branches: [v1.x]` exist only on the frozen branch and stay there.
  The benchmark workflow (`benchmarks.yml`) runs `go test -run '^$' -bench . -benchmem -count=10
  ./...` once in `.` and once in `v2` on each side (base and PR), appending to the same `base.txt`
  and `pr.txt` (`tee -a`), with `base/go.sum`, `base/v2/go.sum`, `pr/go.sum` and `pr/v2/go.sum` in
  `cache-dependency-path` and both module commands in the recorded `command`. Every benchmark on
  `master` today (`engineio/idle_bench_test.go`, `engineio/packet`, `engineio/payload`,
  `engineio/transport`) is in `v2/` afterwards, so a root-only run would silently drop the Stage 2.1
  BEFORE/AFTER comparisons. The `v2` step on the base side runs only when `base/v2/go.mod` exists
  (the step B PR itself has the old layout as base). The change detector (`relevant` in
  `.github/benchmarks/main.go`) already treats `v2/**/*.go` as relevant and `v2/_examples` and
  `v2/_experiments` as not, but it compares `go.mod`, `go.sum`, `go.work` and `go.work.sum` with the
  whole path, so `v2/go.mod` and `v2/go.sum` (a dependency change) are not recognised: B4 compares
  `path.Base(name)` and adds the cases `v2/engineio/x.go`, `v2/go.mod`, `v2/go.sum` (relevant) and
  `v2/_examples/x/main.go` (not) to `.github/benchmarks/main_test.go`. The report groups by the
  import path, and `.github/benchmarks/report/main_test.go` gets a case that
  `github.com/sshaplygin/go-socket.io/v2/engineio` renders under the heading `v2/engineio`.
- B5. Docs, one owner each: `Makefile` per module (`make -C v2 test-race`); `v2/CHANGELOG.md` is the
  moved file without its `## v1.5.0 (unreleased, branch v1.x)` section (those entries are in the
  restored root `CHANGELOG.md`); root `README.md` takes the `@master` link form and one link to
  `v2/README.md`, whose install line is `.../v2@master`; `CLAUDE.md` (intro, layout table with a
  `v2/` row per directory, commands per module, CI jobs, documentation map with `README.md` and
  `CHANGELOG.md` per module); `CONTRIBUTING.md` (rule 2 without the `v1.x` branch, the Releases
  section: tag forms of *Repository layout*, the owner's declaration, `v1.x` frozen, no
  forward-ports, `CHANGELOG.md` per module, `v1.5.0` release commit on `master`);
  the passages below that say v1 lives on `v1.x` or was removed from `master`, which decision 3
  needs true before the owner's declaration (line numbers are those of this revision; the step C
  grep is the source of truth):
  - `docs/PROTOCOL.md` lines 74 to 77, the opening of the Socket.IO v4 section ("Branch `v1.x`
    only ... nothing in this section describes `master`"): the section describes the root module
    (the `socketio` package and `parser`, with `parser.Buffer` and `Header.Query`); the v5 codec of
    `v2/parser` is the next section. Line 97, "Socket.IO v4 runtime, branch `v1.x` only (stage 2.0
    removed the runtime from `master`)": "Socket.IO v4 runtime (v1, repository root)". Line 103,
    the heading "Implemented on master: Socket.IO protocol v5 wire codec": "Implemented in `v2/`:
    ...", and its text names `v2/parser` and the `v2/` root package. Line 157, the table header
    "v4 (branch `v1.x`) | v5 (target; the wire codec is on `master`, see above)": "v4 (v1, repository
    root) | v5 (target; the wire codec is in `v2/parser`, see above)".
  - root `README.md` (the restored `v1.x` file): the table row "v1.x (this branch)", the install line
    `@v1.x` and "use the `v1.x` branch until a release" become the `@master` form and a v2 row with
    a link to `v2/README.md`. `v2/README.md` (the moved file): the "Status of `master`" paragraph,
    the two table rows, the install line and the sentences "The v1 API, on the branch `v1.x`" and
    "on `master` they no longer build" are rewritten for the v2 module (v1 is the repository root);
    the v1 quick start stays in the root `README.md` only.
  - `v2/_examples/README.md` lines 3 to 7 and 33 ("lives on branch `v1.x`", "check out `v1.x`"):
    the v1 server is in the repository root, and the v1 examples are in the root `_examples/`.
  - `v2/CHANGELOG.md` `Removed` entries that say the code "stays on the branch `v1.x`" (the `parser`
    v4 codec, the Engine.IO v3 polling framing, the v1 root runtime): "stays in the v1 module at the
    repository root".
  - `v2/server.go`, the package comment of the v2 root package (shown on pkg.go.dev for the v2
    module; line 9 of this revision: "The v1 server, with the reflection based API, lives on the
    branch v1.x."): v1 is in the repository-root module `github.com/sshaplygin/go-socket.io`. It is
    the only `v1.x` in a Go file of `master` today; the restored root keeps its own `v1.x` mentions in
    comments (`parser/encoder_buffer_test.go`), which stay because the v1 tree is the `v1.x` tip.

**C. Verification**, on the head of the step B PR (`$V1TIP` from its body). The
`api` function is the one of the Stage 1b Acceptance block, applied to the root module:

```sh
V1TIP=${V1TIP:?the v1.x tip recorded in the step B PR}; MOD=$(go list -m); T=$(mktemp -d); BASE=$T/base
git fetch -q origin v1.x
test "$(git rev-parse origin/v1.x)" = "$V1TIP"   # v1.x did not move since step A; run again after the merge
git worktree add -q --detach $BASE $V1TIP; trap 'git worktree remove --force $BASE; rm -rf $T' EXIT
test "$MOD" = github.com/sshaplygin/go-socket.io
test "$(cd v2 && go list -m)" = github.com/sshaplygin/go-socket.io/v2
export GOWORK=off   # a go.work would let one module import the other unnoticed
test -z "$(git ls-files '*go.work' '*go.work.sum')"   # and none is committed
go build ./...
go test -race -count=1 ./...
(cd v2 && go build ./...)
(cd v2 && go test -race -count=1 ./...)
go test -count=1 ./.github/benchmarks ./.github/benchmarks/report   # the detector treats v2/ Go changes as relevant, the report groups v2/<pkg>
test -z "$(go list ./... | grep '/v2')"   # the root module sees v1 only
test -z "$(git ls-files 'v2/*.go' | xargs grep -HnE '"github\.com/sshaplygin/go-socket\.io(/[^"]*)?"' | grep -vE '"github\.com/sshaplygin/go-socket\.io/v2(/[^"]*)?"')"   # v2 imports no root-module package
test -z "$(git ls-files 'v2/*go.mod' | xargs grep -HnE 'github\.com/sshaplygin/go-socket\.io +v')"   # no v2 go.mod requires the root module
test -z "$(git diff --name-only $V1TIP HEAD -- . ':!:v2' ':!:.github' ':!:docs' ':!:CLAUDE.md' ':!:CONTRIBUTING.md' ':!:README.md' ':!:LICENSE' ':!:.gitignore')"   # the v1 tree is the v1.x tip
eval "$(grep '^api() ' docs/ROADMAP.md)"   # an empty match makes the next line fail
test -n "$(api . | head -1)"
test -z "$(diff <(api $BASE) <(api .))"   # the v1 exported API is unchanged
DB=$(grep -oE '"/[^"]*"' .github/dependabot.yml | tr -d '"')
test -z "$(git grep -nE 'v1\.x|[Rr]emoved .*from [`]?master|on [`]?master[`]?' -- docs/PROTOCOL.md docs/API.md)"   # no protocol or API document says v1 lives on v1.x or was removed from master
test -z "$(git grep -nE 'v1\.x|this branch|[Ss]tatus (of|on) [`]master|on [`]master' -- README.md v2/README.md engineio/README.md v2/engineio/README.md CHANGELOG.md v2/CHANGELOG.md _examples/README.md v2/_examples/README.md)"   # the READMEs, changelogs and example notes: no install line @v1.x, no 'this branch', no v1.x branch statement
test -z "$(git grep -nE 'v1\.x' -- 'v2/*.go')"   # no Go comment of the v2 module (the package comment of v2/server.go) says v1 lives on v1.x
test -z "$(git grep -nE 'v1\.x|[Rr]emoved .*from [`]?master' -- CLAUDE.md CONTRIBUTING.md | grep -v frozen)"   # these two may name the branch only on a line that says it is frozen
test -z "$(for d in $(git ls-files 'go.mod' '*/go.mod' | xargs -n1 dirname | sed 's#^\.$##; s#^#/#'); do ok=; for g in $DB; do [[ $d == $g ]] && ok=1; done; test -n "$ok" || echo "no Dependabot entry: $d"; done)"   # every module has an entry
```

The CI run of the head is green in every job (`gh pr checks`), the benchmark report of the PR
lists `v2/` packages (`gh pr view <n> --json comments --jq '.comments[].body' | grep -q 'v2/engineio'`;
the base side has only root packages, so they show as added, not comparable), and `make lint` passes in both
modules. After the merge, on the merged `master`, `consumer master` builds a consumer of the v1
module and the `@master` query of the v2 module resolves, both with `GOPROXY=direct` so that no
proxy cache answers; `$T` is set first because `consumer` creates its directory under it:

```sh
git fetch -q origin v1.x; test "$(git rev-parse origin/v1.x)" = "${V1TIP:?the v1.x tip recorded in the step B PR}"   # v1.x is still the tip step B restored
T=$(mktemp -d); trap 'rm -rf $T' EXIT
eval "$(grep '^consumer() ' docs/ROADMAP.md)"   # an empty match makes the next line fail
consumer master
cd $T && GOPROXY=direct go list -m github.com/sshaplygin/go-socket.io/v2@master
```

Then Stage V1 starts; the work stopped in step A is replayed or restarted as the table says and
merges after MV1.

**Superseded by the restructure** (history is not rewritten; these checks no longer run as
written): Stage 1b, in *Base and branch `v1.x`*, the sentence that makes the `v1.5.0` tag on
`v1.x` at M4 (the Tags decision replaces it), and step 0 (branch cut, `branches: [v1.x]`, Dependabot `target-branch`, the
`## v1.5.0 (unreleased, branch v1.x)` heading), its `v1.x` gates block and `consumer v1.x`; in
the Stage 1 DoD, the fifth `git grep` (`@master` is again the `go get` form of `README.md`
until the tag, and the `until a release` sentence names `master`) and the 1.K remark that its
`awk` is not for `master` (the root `CHANGELOG.md` is the `v1.x` file again); in the Stage 1
tag-time gates, the ancestry lines and the `@v1.x` grep, rewritten above for `master`.

## Stage 0. Documentation baseline

Landed. Maintain the documentation ownership map in CLAUDE.md; protocol facts,
release history and implementation commands stay in their respective files.

## Stage 1. Infrastructure and known bugs (released with Stage V1 as `v1.5.0` on the owner's order)

No protocol changes. Allowed API changes are `engineio.Options.Logger`,
`engineio.Options.WriteBufferSize` (temporary v1 placement), `socketio.ErrWriteBufferFull`,
the logger exports
listed in the changelog, `Deprecated:` notices on `logger.Error` and `logger.Info`, and the already-landed session logger parameter. That
session constructor signature change must be called out in v1 migration notes;
root `NewServer` and handler signatures stay unchanged. `gorilla/websocket` stays in v1; the
transport swap happens in stage 2 where the transport is rewritten.

Tasks:

- **1.R Redis:** protect `requests` and room access; time out peer queries; propagate
  adapter construction errors through the 1I integration step; reconnect subscriptions with backoff after receive
  failures. Each fix has a regression test, including two-server tests under `-race`.
  - *Known limitations (`v1.5.0` ships them):* found while diagnosing the CI flake
    that PR #18 (`511d973`) fixed in the test only. Line numbers are
    `redis_broadcast.go` at `82aa740`.
    1. *Unconfirmed subscription:* `subscribe` (`:192-206`) sends PSUBSCRIBE and
       SUBSCRIBE and returns without reading the confirmations, which `dispatch`
       reads later (`:678`). `newRedisBroadcast` (`:117-187`), and with it the
       handler registration that builds the broadcast (`createNamespace`,
       `server.go:382`), returns without waiting until Redis has registered the
       instance. Until it has, broadcasts and `Server.ClearRoom` requests published
       by peers do not reach the instance, peers' `Server.RoomLen` and
       `Server.Rooms` do not count it, and its own can miss its local members too,
       because its own answer also travels through Redis. Every resubscribe after
       a receive error (`:687`, `:699-723`) opens the same window.
    2. *Full wait for missing answers:* `Len` and `AllRooms` (`:315-348`,
       `:212-241`) expect as many answers as PUBSUB NUMSUB reports for the request
       channel (`AllRooms` uses 0 when NUMSUB fails, `:220`), and `onResponse`
       signals only when the answer count equals it (`:533`, `:549`). When NUMSUB is
       0, as in item 1, or an answer is missing, they wait the full
       `redisRequestTimeout` (5 s, `:52`) and return the answers received by then.

    Stage 1 does not change them, and the 1.R and 1I contracts promise neither
    registration on return nor an early answer. Reading the confirmations changes
    when handler registration and `Server.Close` (which waits for a registration,
    `server.go:87-88`) return, and needs its own deadline: the dial context's close
    hook has stopped once the dial returns (`:143`). redigo's `ReceiveWithTimeout`
    exists in v1.8.9 and in the v2.0.0+incompatible that `_examples/gf` resolves.
    Returning early when NUMSUB is 0 is not a safe fix for item 2: NUMSUB and
    PUBLISH are separate commands (`:220`, `:226`; `:327`, `:338`), so an instance
    that registers between them answers, and v1 counts local members through Redis.
    Counting the receivers that PUBLISH reports instead would end the wait at once
    when nobody can answer; the full wait while the requester itself is unregistered
    (its answers on the response channel are lost) still needs item 1, and a counted
    peer that never answers still costs the full wait. Tests that rely on peer
    requests or answers wait until NUMSUB of the request channel counts the
    instance, which also covers the PSUBSCRIBE sent before it on the same
    connection. Tests that rely only on broadcasts may wait on NUMPAT only with
    miniredis, whose NUMPAT counts each client's patterns; Redis 7 and later count
    unique patterns, which every instance of a namespace shares. PR #18 applies
    this rule (`waitRedisSubscribers`) and adds `delayRedisSubscriptions`, which
    holds PSUBSCRIBE with a miniredis pre-hook and releases it after a set time. 1.K
    carries the limitations into the `v1.5.0` release notes and godoc; the v2
    requirement is 2.2 *Readiness*.
- **1.B Backpressure:** each connection has a bounded queue of outbound packets. The
  rules below apply to `Server` connections and to `Client` alike.
  - *Size:* temporary v1 `engineio.Options.WriteBufferSize` counts socket.io packets
    (unrelated to `websocket.Transport.WriteBufferSize`, which counts bytes); 0 and
    negative values mean the default 64; no opt-out in v1. A packet the writer has
    started (see *Overflow*) no longer counts toward it. 1.B uses an unexported
    default; 1I wires the option.
  - *Drain deadline:* `engineio.Options.PingTimeout` as passed to `NewServer` /
    `NewClient`; nil options, 0 and negative values mean one minute. It is read by the
    socket.io layer without changing the `engineio.Conn` interface. For `Client` it is
    the local option, not the server's handshake value. 1.B keeps it in an unexported
    per-connection field (tests set it); 1I wires it.
  - *Emit* never blocks. Emits from `OnConnect` are queued and written once the writer
    starts. Packets the library queues itself (ACK replies, namespace CONNECT replies)
    follow the same rules as `Emit`, so the read goroutine never blocks on a full queue.
  - *Connected namespace:* a namespace counts as connected from the moment it is
    registered, before its `OnConnect` runs, as in v1.4. A namespace whose `OnConnect`
    failed therefore gets its one `OnDisconnect` when the connection closes.
  - *First close decides:* a connection is closed at most once. The first close fixes
    which `OnDisconnect` calls run and whether Emits still queue; `OnDisconnect` runs
    exactly once per connected namespace whatever the number and kind of closes, also
    when a peer namespace DISCONNECT is being dispatched as the close starts; later
    `Close` calls return `nil` and change nothing. A later library-started close can
    only shorten a drain: any read or writer failure during a drain, whatever its
    cause, ends the drain at once and discards the rest, without a report. Once any
    close has started, an `Emit` is dropped silently, never blocks and is not reported,
    except that a draining close still queues packets until its seal.
  - *Overflow* (no close started): an Emit that finds the queue full marks the
    connection overflowed, drops that packet and every later one, and starts a
    discarding close without blocking the emitter. `OnDisconnect`, leaving the rooms
    and one `ErrWriteBufferFull` report (exported sentinel) run on the connection's
    own goroutines. An overflow before those goroutines start always takes the
    connect-failure path, whatever `OnConnect` returned; there `serveConn` runs them
    synchronously and passes a nil `Conn` to the report, as v1.4 does for connect
    failures, and an `OnConnect` error is reported separately. The report goes to
    `OnError` of the namespace of the overflowing packet and is delivered although the
    connection is closing; with
    no `OnError` registered it is dropped (1.L logs it). Report and `OnDisconnect` have
    no guaranteed order. After the overflow flag is set the socket.io writer starts no
    further packet. A packet has started once the writer called `NextWriter` for its
    first frame; the overflow's engine.io close makes a blocked `NextWriter` fail. A
    started packet counts as in flight together with its binary attachments.
    Engine.io control frames are out of scope.
  - *Draining close:* only `Conn.Close` or `Client.Close` called by application code
    (including from handlers) drains. `Close` runs `OnDisconnect`; the queue is
    *sealed* when every `OnDisconnect` called by this `Close` has returned. Packets
    queued before the seal, including Emits from `OnDisconnect`, are written by the
    writer goroutine in the background; an Emit that finds the queue full before the
    seal is dropped silently and not reported. The drain ends when the queue is empty
    after the seal, or at the drain deadline measured from the start of `Close`,
    whichever is first; remaining packets are discarded and the engine.io connection
    is closed. From the start of `Close` the read goroutine keeps reading, so engine.io
    pings are answered and a peer close is detected, but it dispatches no CONNECT,
    EVENT, ACK or DISCONNECT packet. `Close` returns `nil` without waiting. A `Close`
    from root `OnConnect` that then returns nil is not a connect failure: the
    goroutines start and the drain runs.
  - *Discarding closes:* every close started inside the library discards the queue and
    closes the engine.io connection at once: header read or decode error, argument
    decode error, dispatch error, encode error (any `Encode` failure, marshal or
    transport write; in v1.4 the connection stayed open), peer close (engine.io CLOSE
    or the transport closed or failed from the peer side), ping timeout, overflow, and
    a connect failure before the writer starts. Reports are routed as in v1.4: a
    failure to read or decode a packet header goes to root `OnError`, and so do a peer
    close and a ping timeout, which reach the reader as such a failure; an argument
    decode, dispatch or encode error goes to `OnError` of the packet's namespace; a
    connect failure goes exactly once to root `OnError` with a nil `Conn`, whether it
    came from `Encode` or from `OnConnect`. A report of the close's own cause comes
    first: the close starts only after its `OnError` call returns, so Emits from that
    call still queue and can overflow. On the connect-failure path, where an overflow
    may already have started the close, `serveConn` delivers the connect-failure
    report, then the overflow report if any, and only then discards the queue, runs
    `OnDisconnect` and closes engine.io (v1.4 closed first and reported after). Once any
    close has started, a read or writer failure, including one caused by the library's
    own engine.io close, is not reported and starts no new close; during a drain it
    ends the drain (see *First close decides*). `Server.Close`: see 1I *Shutdown*.
  - *Namespace DISCONNECT:* a socket.io DISCONNECT from the peer ends only that
    namespace: its `OnDisconnect` runs once and its rooms are left; the engine.io
    connection and the queue stay as they are.
  - *Docs:* godoc of `WriteBufferSize`, `Emit` and `Close` states the rules above;
    that more than `WriteBufferSize` packets queued faster than the writer drains them
    *can* close a healthy client; that polling writes one engine.io frame per poll
    round trip (a packet with k binary attachments needs k+1), so polling clients
    overflow at much lower emit rates; that an encode error now closes the connection;
    that a closing error is now reported before the close's effects run; and that after `Close` returns the transport closes asynchronously, so
    `Server.Count` still counts the session until then. The changelog entry is
    written in 1I.
  - *Broadcasts:* `Send`, `SendAll` and `ForEach` emit or call back after releasing
    the room lock. The Redis broadcast already does this (1.R); 1.B changes the
    in-memory one. One copy per socket for `SendAll` is owned by 2.2; 1.B points the
    pinning comment in `lifecycle_test.go` at 2.2 only.
  - *Tests (1.B):* deterministic under `-race` with a fake `engineio.Conn` whose
    writer can be blocked; 1.B adds an unexported dial seam to `Client` so the `Client`
    cases use the same fake. Every close case also asserts the `OnDisconnect` call
    count. Side: S = `Server`, C = `Client` (root namespace only).
    1. 1B-T1 (S): one stalled member does not block another member of its room.
    2. 1B-T2 (S, C): a draining `Close` delivers N queued packets, N = 1 and N = 64,
       and returns while the writer is still blocked.
    3. 1B-T3 (S, C): a draining `Close` writes Emits from `OnDisconnect`, including one
       made after the writer emptied the queue.
    4. 1B-T4 (S, C): an Emit after the seal and an Emit that finds the queue full
       during `OnDisconnect` are dropped without report.
    5. 1B-T5 (S, C): after a draining `Close` an incoming EVENT runs no handler and an
       incoming CONNECT runs no `OnConnect`.
    6. 1B-T6 (S, C): with a 50 ms drain deadline a stalled peer's engine.io connection
       closes within 50 ms + 1 s, and `OnError` is not called.
    7. 1B-T7 (S, C): a read-error close discards and closes at once.
    8. 1B-T8 (S, C): an engine.io-CLOSE close discards, closes at once and is reported
       exactly once to root `OnError`.
    9. 1B-T9 (S, C): a dispatch-error close discards and closes at once; on S, an
       argument decode error in a non-root namespace is reported to that namespace's
       `OnError` and not to root.
    10. 1B-T10 (S, C): an encode error is reported once to `OnError` of the packet's
        namespace, then the connection discards and closes at once.
    11. 1B-T11 (S, C): a connect failure discards, closes at once and is reported
        exactly once, before `OnDisconnect` runs. On C the trigger is the CONNECT
        `Encode` failure and nothing can be queued; a Client `OnConnect` error is a
        dispatch error (1B-T9).
    12. 1B-T12 (S, C): a read-error close whose `OnDisconnect` emits
        `WriteBufferSize`+1 packets reports no `ErrWriteBufferFull` and nothing for
        the dropped Emits.
    13. 1B-T13 (S, C): a read failure from the fake reader during a draining `Close`
        ends the drain at once, unreported.
    14. 1B-T14 (S, C): a peer namespace DISCONNECT whose `OnDisconnect` is held on a
        channel races `Close`; `OnDisconnect` runs once for that namespace and once for
        each other connected namespace.
    15. 1B-T15 (S, C): with a packet in flight (its `NextWriter` called and blocked),
        an overflow reports `ErrWriteBufferFull` exactly once, to the
        namespace of the overflowing packet.
    16. 1B-T16 (S, C): with a binary packet in flight (its `NextWriter` called and
        blocked) an overflow lets the writer start no further packet.
    17. 1B-T17 (S): an overflow inside root `OnConnect` returning nil takes the
        connect-failure path, reports `ErrWriteBufferFull` once and writes nothing
        after it.
    18. 1B-T18 (S): the same with `OnConnect` returning an error; `ErrWriteBufferFull`
        and the `OnConnect` error are each reported once.
    19. 1B-T19 (S): an overflow triggered inside `BroadcastToRoom` does not deadlock.
    20. 1B-T20 (S): an overflow triggered inside `ForEach` does not deadlock.
    21. 1B-T21 (S, C): with a packet in flight (its `NextWriter` called and blocked),
        a dispatch error whose `OnError`
        emits `WriteBufferSize`+1 packets does not deadlock; `ErrWriteBufferFull` is
        reported once and `OnDisconnect` runs once.
    22. 1B-T22 (S, C): `Close` from `OnError` does not deadlock.
    23. 1B-T23 (S, C): `Close` from `OnDisconnect` does not deadlock.
    24. 1B-T24 (S, C): `Close` called twice concurrently does not deadlock.
    25. 1B-T25 (S, C): a namespace DISCONNECT keeps the session open; on S the other
        namespaces keep working.
    26. 1B-T26 (S): a draining `Close` from root `OnConnect` returning nil delivers the
        Emits queued before the seal and closes the engine.io connection within the
        drain deadline.
    27. 1B-T27 (S, C): with a packet in flight, an ACK reply that finds the queue full
        starts an overflow close
        and the read goroutine does not block; on S, the same for a namespace CONNECT
        reply.
  - *Gate record:* a test covering a case names it in its doc comment, for example
    `// Covers 1B-T3 (S, C).`; the stage 1 DoD checks that every (case, side) pair of
    every list the stage 1 DoD names is named by a passing test.
- **1I Integration** (the integrator, after 1.R and 1.B). The rules below complete
  the 1.R and 1.B items; public signatures stay unchanged.
  - *Options:* `NewServer` and `NewClient` pass `engineio.Options.WriteBufferSize`
    and `PingTimeout` to every connection they create, normalised as 1.B *Size* and
    *Drain deadline* say. Engine.io ignores `WriteBufferSize`, and its own handling of
    `PingTimeout` does not change.
  - *Redis construction errors:* when the Redis broadcast of a namespace cannot be
    created, its handler holds a no-op broadcast together with the error, so no
    broadcast call can reach a nil value. The error wraps the Redis error with `%w`
    and names the namespace (root as `/`). Handler creation is serialised by a server-level
    creation mutex, not by the handlers lock that packet dispatch reads: under it the
    server rechecks, builds, records the first construction error in registration
    order and only then stores the handler, so one namespace never builds two
    broadcasts and a slow Redis dial never stalls dispatch. `Serve` returns nil once
    `Close` has been called, whether or not a construction error was recorded.
    Otherwise it reads the recorded error once on entry, without taking the creation
    mutex, and returns it without accepting a connection. The engine keeps completing
    handshakes after that without serving them until the caller calls `Close`. The
    error is kept: registering more handlers does not rebuild the broadcast.
    A connection to such a namespace fails before the namespace is registered and
    before any CONNECT packet for it is encoded, so its `OnConnect` and `OnDisconnect`
    never run. For root this is a connect failure (1.B): nothing is written, no
    namespace is connected, and the error is reported once to root `OnError` with a
    nil `Conn`. For another namespace it is a CONNECT dispatch error (1.B): the error
    is reported once to that namespace's `OnError`, whose `Conn` room methods do
    nothing and whose `Rooms` returns nil; the other connected namespaces then get
    their `OnDisconnect` by the 1.B dispatch-error close.
    The `Server` room methods for such a namespace do nothing: `RoomLen` returns -1
    (as for an unknown namespace or a failed Redis query), `Rooms` nil, `ForEach`
    false without calling its function, and the other methods false.
  - *Shutdown:* `Server.Close` closes engine.io, then, under the creation mutex,
    marks the server closed and stops the Redis subscriber and publisher connections
    of every namespace (1.R's unexported `close`); a registration racing `Close` is
    therefore either stopped or builds nothing. With `Adapter` set, a handler
    registered after `Close` builds no Redis broadcast and holds the no-op broadcast
    with an unexported "server closed" error, which is not recorded for `Serve` (see
    *Redis construction errors* for what `Serve` returns after `Close`). `Close` waits
    for a registration already holding the creation mutex, up to the Redis dial
    timeout. v1 `Server.Close` does not close sessions;
    for sessions still open, cross-instance broadcasts stop and the `Close` godoc says
    so. Engine.io `Close` no longer closes the channel that hands sessions to
    `Accept`; `Accept` returns `io.EOF` once `Close` was called. Every session not yet
    handed to `Accept` when `Close` is called (buffered, or its sender still waiting)
    and every session whose handshake completes after `Close` is closed and removed
    from the session manager, and its hand-off goroutine ends, so no handshake can
    panic with a send on a closed channel or stay open unserved.
  - *Docs:* godoc of `engineio.Options.WriteBufferSize` and `Emit` as 1.B *Docs*
    says; the `Conn.Close` and `Client.Close` godoc name `PingTimeout` (for `Client`,
    its local option) as the drain deadline; the `Adapter`, `Serve` and `Close` godoc
    state the rules above; the `Serve` godoc warns that after an error return
    handshakes still complete unserved until `Close`, and the `Close` godoc that it
    can wait for a registration's Redis dial. `CHANGELOG.md` gets the 1.R,
    1.B and 1I entries, written by the integrator for the whole wave (an exception to
    the per-PR entry rule in `CONTRIBUTING.md`): fix entries follow the stage DoD's
    changelog rule, and 1.B and 1I behaviour entries also link to the godoc instead of
    repeating it.
  - *Tests (1I):* every C case uses the 1.B dial seam.
    1. 1I-T1 (S): 1B-T1 against the Redis broadcast.
    2. 1I-T2 (S, C): `WriteBufferSize` 0 and negative give 64.
    3. 1I-T3 (S, C): a custom `WriteBufferSize` is used, checked on the stored
       per-connection value.
    4. 1I-T4 (S, C): nil options and `PingTimeout` 0 give a one-minute drain deadline,
       checked on the stored per-connection value.
    5. 1I-T5 (S, C): a negative `PingTimeout` gives one minute, checked on the stored
       per-connection value (engine.io sessions with a negative timeout expire at once).
    6. 1I-T6 (S, C): a custom `PingTimeout` becomes the drain deadline, checked on the
       stored per-connection value. The live bound is 1B-T6: on a real session the
       engine.io write deadline equals `PingTimeout`, so a live test could not tell the
       two apart.
    7. 1I-T7 (S): with `Adapter` set and Redis stopped, two namespaces are registered
       in a known order; Redis is restarted and another handler is registered on the
       first one. `Serve`, run in a goroutine, returns within 1 s an error that names
       the first namespace and matches `*net.OpError` with `errors.As`; `RoomLen` on
       the first namespace still returns -1.
    8. 1I-T8 (S): handlers are registered after `Serve` passed its entry check (the
       test waits on an unexported signal `Serve` gives after that check). A root
       connection to a failed root namespace: no socket.io packet is written,
       `OnConnect` and `OnDisconnect` are not called, root `OnError` gets one error,
       with a nil `Conn`, that matches `*net.OpError`, and the engine.io connection
       closes. A CONNECT to a failed non-root namespace: no CONNECT reply for it is
       written, its `OnConnect` and `OnDisconnect` are not called, its `OnError` gets
       one such error and root `OnError` none, `Join`, `Leave`, `LeaveAll` and `Rooms`
       on that `Conn` do not panic and `Rooms` returns nil, root `OnDisconnect` runs
       once and the connection closes.
    9. 1I-T9 (S): on a failed namespace, `JoinRoom`, `LeaveRoom`, `LeaveAllRooms`,
       `ClearRoom`, `BroadcastToRoom` and `BroadcastToNamespace` return false,
       `RoomLen` returns -1, `Rooms` returns nil, and `ForEach` returns false without
       calling its function.
    10. 1I-T10 (S): two working namespaces are registered, Redis is stopped, a third
        namespace is registered and fails, and Redis is restarted. Once both working
        namespaces are subscribed again (PUBSUB NUMPAT = 2) and no broadcast is in
        flight, `Server.Close` does not panic, and within 1 s Redis reports no
        subscriber on any namespace channel and no client connection (miniredis
        `CurrentConnectionCount` is 0). A handler registered after `Close` opens no
        Redis connection, its `RoomLen` returns -1, and `Serve` then returns nil.
    11. 1I-T11 (S, on `engineio.Server`): with nothing calling `Accept`, two
        websocket engine.io clients complete the handshake; then `Close` is called and
        a third completes its handshake. Nothing panics, `Accept` returns `io.EOF`,
        the race detector reports nothing, and within 1 s `engineio.Server.Count` is
        0 and the `NextReader` of each of the three clients returns an error.
    12. 1I-T12 (S): with `Adapter` set and Redis reached through a listener that
        delays each accepted connection by 50 ms, 8 goroutines released by one barrier
        call `OnEvent` with distinct event names on one new namespace, under `-race`.
        Before `Close`, miniredis `CurrentConnectionCount` equals that of a single
        namespace's broadcast; after `Close` it is 0 within 1 s.
- **1.S Runtime fixes (landed; its lifecycle tests stay a stage gate):** synchronous session registration before a second request can
  use its SID; `Manager.Count` uses `RLock`; correct EOF result from `Server.Serve`.
  Cover session lifecycle and root connect/event/ack/namespace/room/disconnect paths.
- **1.L Logging (minimal v1).** Library code logs only through `slog`, by the rules
  below. The other records of the original plan move to 2.4 (*Logging and overhead*).
  - *Calls:* the remaining `fmt.Printf` is replaced. Every `logger.Error` and
    `logger.Info` call site moves to a `*slog.Logger` call: the instance logger where
    one is reachable, `logger.Log` otherwise. Both functions get `Deprecated:` notices
    naming `logger.Log` and `engineio.Options.Logger`; `make lint` (staticcheck SA1019)
    shows that no library code calls them, with no `//nolint` for SA1019.
  - *Messages and keys:* every library record's message is a constant matching
    `^(engineio|socketio|logger): [a-z][a-z0-9 ]*$`: `engineio` for `engineio/...`,
    `socketio` for the root package, `parser` and the Redis broadcast, `logger` for
    package `logger`. Attribute keys come only from `sid`, `nsp`, `err`, `transport`,
    `remote_addr`, `reason`, `duration`, `event`, `ack_id`, `type` and `value`; the root
    namespace is logged as `/`. The `SOCKETIO_LOG_LEVEL` warning becomes
    `logger: invalid level ignored` with `value`. No record carries packet payloads.
  - *Levels:* the library logs no ERROR or INFO records; records an application emits
    through the deprecated `logger.Error` and `logger.Info` are the application's.
    Expected closure (`io.EOF`, a closed connection, a peer close, a ping timeout, and
    any failure after a close of that session or connection has started) logs at most
    DEBUG and never `socketio: unhandled error`. The error that starts a close
    (including `ErrWriteBufferFull`) and the errors reported on the 1.B connect-failure
    path are not failures after a close has started. An error delivered to a
    registered `OnError` logs at most DEBUG. Any other error whose namespace has no
    `OnError` logs exactly one `socketio: unhandled error` WARN with `sid`, `nsp` and
    `err` (this is the record 1.B *Overflow* refers to; for an overflow `nsp` is the
    overflowing packet's namespace, also when it has no `OnError`); a CONNECT to a namespace
    without handlers is such an error. Every other record of an error that is
    delivered to `OnError` or logged as `socketio: unhandled error`, before or after,
    logs at most DEBUG, and so does a log call whose error is also returned to its
    caller. At the socket.io and parser layers every failure returned by engine.io
    `NextReader` or by the frame reader it returns, and every failure returned by
    `NextWriter` or by the frame writer it returns (`Close` included), is expected
    closure. Of the errors returned by the decoder and encoder, only a parser decode
    error that is not a failure returned by the frame reader, and a marshal error in
    `Encode`, are other errors; socket.io tells them apart by wrapping, unexported,
    the frame reader and writer it gives the decoder and encoder. The wrappers record
    where a failure came from, for the packet being decoded or encoded (cleared at
    each `NextReader` and `NextWriter`), and do not change the error values passed to
    `OnError`; the frame reader's `io.EOF` passes through unchanged. An `io.EOF` that
    the frame reader returns before the parser has finished a packet (an empty or
    truncated packet) is a parser decode error, not expected closure; the `io.EOF` in
    the expected-closure list is one returned by `NextReader` or `NextWriter`. At the engine.io
    session layer, a failure of the session's own frame reader or writer that the
    session reports as a close reason logs at most DEBUG, and so do the websocket
    wrapper's "frame not closed" reminders and the transport-layer log calls
    (`engineio/packet` included) made while returning a failure of the session's frame
    writer. Other records of a failure that is logged
    as `request rejected` log at most DEBUG.
    Failures on an upgrade probe connection after its transport `Accept` succeeded log
    at most DEBUG: the session keeps its old transport. The polling POST log calls (unsupported
    content type, `FeedIn`, writing the answer) log at most DEBUG: the client gets the
    400 answer or has gone. Every other existing log call that reports a failure no
    caller receives logs WARN. Failures v1 does not log today stay unlogged: the Redis
    broadcast's publish, decode and resubscribe errors (4b) and the polling GET 500
    answers and invalid-method 400 (2.1). Only the `socketio.Server` side of these
    rules is tested in v1; `Client` shares the code.
  - *Boundary records:* engine.io rows come from sessions of `engineio.Server`,
    socket.io rows from `socketio.Server` connections. `Client` and the engine.io
    client emit none of them in v1. The keys are a contract reused by 2.4.

    | Message | Level | Keys |
    | --- | --- | --- |
    | `engineio: request rejected` | WARN; DEBUG for `unknown sid` | `transport`, `remote_addr`, `reason`, `err` |
    | `engineio: session open` | DEBUG | `sid`, `transport`, `remote_addr` |
    | `engineio: session close` | DEBUG | `sid`, `transport`, `reason`, `duration`, `err` |
    | `socketio: namespace connect` | DEBUG | `sid`, `nsp`, `err` |
    | `socketio: disconnect` | DEBUG | `sid`, `nsp`, `reason` |

    `err` is omitted when there is no error. *request rejected:* one record per
    request `ServeHTTP` rejects, and one per failed session initialisation, logged
    where it fails (including the hand-off goroutine that runs `InitSession`); the
    polling transport's own 4xx/5xx answers are not `request rejected`. `reason` is `bad transport` (unknown transport),
    `checker` (`RequestChecker` error), `unknown sid` (sid not found), `accept`
    (transport `Accept` failed, on upgrade too, including a websocket handshake
    error), `init` (session creation or `InitSession` failed) or `bad upgrade`
    (upgrade to an earlier transport). *session open:* once, after `InitSession`
    succeeded. *session close:* exactly once per session that logged open, when it
    closes; a session that never logged open logs no close. The session logs its own
    open at the end of a successful `InitSession`. It classifies failures returned by
    `NextReader` and `NextWriter` and by the frame reader and writer they return, so
    it wraps those; `io.EOF` at the end of a frame is not a failure. Failures of the
    frames the session reads or writes itself on its active connection (ping and
    pong, CLOSE) and of setting its deadlines are candidate first causes too
    (`transport error` unless the deadline had passed). Failures on an upgrade probe
    connection before the switch are not; after a switch the session sets the new
    connection's deadline again, so `ping timeout` uses the active connection's
    deadline; a failure to set it closes the session as `transport error`. 1.L also
    makes the session-creation `Accept` path skip `http.Error` after a websocket
    handshake error, as the upgrade path already does, so net/http no longer logs a
    second `WriteHeader`.
    `engineio.Server.Close` passes `server shutting down` to the session without new
    exported API in `engineio/session` (a hook in an `engineio/internal` package,
    importable only by packages under `engineio/`). `reason` is the first cause the session
    observed: `transport close` (CLOSE packet from the client), `ping timeout` (a read
    or write failed after the deadline set from `PingTimeout` had passed),
    `transport error` (any other transport read or write failure, including EOF and a
    peer close), `forced close` (`Close` called on the
    session by the application or by socket.io) or `server shutting down`
    (`engineio.Server.Close` closed a session never accepted, 1I). `duration` runs
    from session open, as `slog.Duration`; `err` is set only for `transport error`.
    *namespace connect:* one per namespace
    CONNECT handled for a `socketio.Server` connection, root included, after
    `OnConnect` returned or the connect failed; a connect failed by an overflow carries
    `ErrWriteBufferFull`, joined with the `OnConnect` error when there is one
    (`errors.Is` holds for both); this applies to root, where an overflow takes the
    1.B connect-failure path. For another namespace the record carries `err` only when
    `OnConnect` fails or the namespace's broadcast failed (1I); an overflow during its
    `OnConnect` is logged by the `OnError` or unhandled-error rule. A CONNECT to a
    namespace without handlers gets no record, and neither does a CONNECT dropped
    because a close has started. *disconnect:* one per connected
    namespace when its disconnect runs (1.B: exactly once per connected namespace),
    whether or not an `OnDisconnect` handler is registered; `reason` is `namespace disconnect` for a peer DISCONNECT of that namespace
    and `connection close` otherwise; text sent by the peer is never logged.
  - *Tests (1.L):* side P = polling, W = websocket, S = `socketio.Server`; a P or W case
    that names socket.io behaviour runs on `socketio.Server` over that transport, the
    others on `engineio.Server`. Gate record as in 1.B. Cases 1L-T8, 1L-T9 and 1L-T13
    capture both the instance logger and `slog.Default` (not in parallel, restored in
    `Cleanup`), and their counts cover both. The upgrade-probe, session-layer and
    reminder DEBUG limits are checked by review only.
    1. 1L-T1 (P, W): a CLOSE packet from the client gives `transport close`.
    2. 1L-T2 (W): the peer closing the websocket gives `transport error` with `err`.
    3. 1L-T3 (P, W): no client traffic with `PingTimeout` 100 ms gives `ping timeout`;
       on W also after a polling-to-websocket upgrade, with `transport=websocket`.
    4. 1L-T4 (P, W): `Close` by the application gives `forced close`.
    5. 1L-T5 (P): `engineio.Server.Close` with a session never accepted gives
       `server shutting down`.
    6. 1L-T6 (P, W): a CLOSE packet followed by `Close` gives one record,
       `transport close`.
    7. 1L-T7 (P, W): every session-close record has a `duration` (> 0 for 1L-T3) and
       no `err` except for `transport error`; a session that read a message before
       `Close` still gives `forced close`.
    8. 1L-T8 (S): with root `OnError` registered, it receives `io.EOF` itself (`==`) for
       an engine.io CLOSE, and a fake frame reader's sentinel error unchanged when it
       fails mid-frame; a peer close with no root `OnError` logs no WARN; an event handler
       that panics (recovered as an error) with `OnError` registered logs nothing
       above DEBUG; the same without `OnError` logs exactly one
       `socketio: unhandled error` WARN; a CONNECT to a namespace without handlers
       logs one such WARN; an overflow with no `OnError` on the packet's namespace
       logs exactly one such WARN with `ErrWriteBufferFull` and that `nsp`; the 1B-T18
       case without root `OnError` logs one such WARN for each of its two errors; an
       argument decode error without `OnError` logs exactly one WARN in total; an
       Emit whose argument cannot be marshalled (a `chan`) without `OnError` on its
       namespace logs exactly one such WARN with that `nsp`; a frame writer that fails
       on `Write` or `Close` (fake `engineio.Conn`) without `OnError` logs no WARN; a
       frame with an invalid packet type without root `OnError` logs exactly one such
       WARN; an empty message frame and an EVENT `2` with no data, each without root
       `OnError`, log exactly one such WARN.
    9. 1L-T9 (S): each `request rejected` reason is logged once by its trigger,
       `unknown sid` at DEBUG and the others at WARN; `init` is triggered through a
       fault-injecting transport in `engineio.Options.Transports`, and that session
       logs no open and no close; a polling POST with an unsupported `Content-Type`
       gets 400 and logs nothing above DEBUG; each trigger logs no record from the
       module's code (by record PC, as in 1L-T11) above DEBUG other than its
       `request rejected`. `accept` is triggered by a websocket handshake error at
       session creation, which also logs no net/http `superfluous WriteHeader` record.
    10. 1L-T10 (S): for `/` and `/chat`, `namespace connect` without `err` on success
        and with `err` when `OnConnect` fails; `disconnect` with `namespace
        disconnect` for a peer DISCONNECT that carries text (the text is not logged)
        and `connection close` when the connection closes, also for a connected
       namespace without an `OnDisconnect` handler; in the 1B-T17 case the root record's
       `err` matches `ErrWriteBufferFull`, and in the 1B-T18 case `errors.Is` holds for
       both errors; an overflow during a non-root `OnConnect`, without `OnError` on
       that namespace, leaves its record without `err` and logs one
       `socketio: unhandled error` WARN; in the two 1I-T8 cases, the root record and
       the non-root record carry the construction error; a CONNECT read after a
       draining `Close` started logs no `namespace connect` record.
    11. 1L-T11 (S): `TestNoBadKeyAttrs` installs a checking handler as the server's
        `Options.Logger` and with `slog.SetDefault` (the scenario's Go client has no
        `Options.Logger`), sets `logger.Level` to `LevelTrace` and runs the
        `TestLifecycleRootNamespace` scenario. For records emitted from the module's
        code (by record PC), including keys added through `WithAttrs`, it fails on a
        message outside the pattern, a key outside the list, a `!BADKEY` attribute or
        an ERROR or INFO level. On the instance-logger handler it asserts one `sid`
        on the server's session open, root namespace connect and disconnect. Records
        whose PC is in the deprecated `logger.Error` or `logger.Info` are skipped. It
        does not run in parallel and restores the default logger and `logger.Level` in
        `Cleanup`.
    12. 1L-T12 (S): `TestServerLoggerOption` asserts `socketio: unhandled error` with
        `nsp=/nope` for a CONNECT to a namespace without handlers, through the
        instance logger only.
    13. 1L-T13 (P, W): a ping timeout (`PingTimeout` 100 ms, no client traffic after
        the handshake) without root `OnError` logs no WARN, and the session close
        reason is `ping timeout`. On P it reaches socket.io as a write failure on the
        1.B connect-failure path: root `OnConnect` is not called, the root
        `namespace connect` record carries `err`, and root gets a `disconnect` with
        `connection close`.
  - *Docs:* the package `logger` godoc documents the variable, the levels, the
    message pattern and the keys. `CHANGELOG.md` entries cover the deprecation, the
    key renames, the level changes (ERROR and INFO to DEBUG and WARN), the records, the
    deadline reset after an upgrade switch and the session-creation handshake-error
    answer.
    1.L edits no Markdown file other than `CHANGELOG.md`.
- **1.D Docs.** Files: `README.md`, `engineio/README.md`, `logger/README.md`,
  `CLAUDE.md`, `CONTRIBUTING.md`, `CHANGELOG.md`.
  - `engineio/README.md` keeps a title, one paragraph saying what the package is,
    and links to `README.md`, `docs/PROTOCOL.md` and its godoc; no install, examples
    or API usage. Its `CLAUDE.md` map row: owns "what the engineio package is; links
    to README.md, docs/PROTOCOL.md and its godoc"; must not contain "install,
    examples, API usage".
  - `logger/README.md` is deleted: it tells users to assign `logger.Log`, which
    bypasses `logger.Wrap`, and the package godoc owns its subject (1.L).
  - Link form (1.D owns it, by URL kind): until `v1.5.0` is tagged, `go get` commands
    use `@v1.x`, the branch cut in 1b step 0, and pkg.go.dev links carry no version
    (`https://pkg.go.dev/github.com/sshaplygin/go-socket.io[/pkg]`), because pkg.go.dev
    accepts only a semantic version, `latest` or `master` and answers HTTP 400 for
    `@v1.x`. 1.D replaces `@master` in the `go get` command of `README.md` with `@v1.x`
    and strips `@master` from every pkg.go.dev link in `README.md`,
    `engineio/README.md` and `CHANGELOG.md`, in a commit on `master` before the cut, so
    both branches carry it, and writes into `README.md` one sentence containing the
    words `until a release` that tells users to use the branch until a release is
    tagged. The tag-time release commit and the reason for the branch name are owned by
    [`CONTRIBUTING.md`](../CONTRIBUTING.md#releases); the tag-time gates are in the
    Stage 1 Acceptance. The `@v1.x` form is superseded: until a tag exists, `go get` uses
    `@master` for v1 and `/v2@master` for v2 (*Repository layout*).
  - Already satisfied at `9716ec0` and guarded by the DoD: no `godoc.org` links;
    the README badges point at this fork.
- **1.K Known limitations** (wave 1C, after wave 1B has merged, so no other task
  edits these files at the same time). Files: the godoc of `Server.Adapter`,
  `RoomLen` and `Rooms` in `server.go`, and a `### Known limitations` subsection in
  the `## Unreleased` section of `CHANGELOG.md`, which becomes `v1.5.0`. No code,
  test or other documentation change.
  - The godoc states the behaviour; from the tag it, not 1.R, records the two 1.R
    *Known limitations*. The subsection names each limitation in one sentence,
    without line numbers, and links the pkg.go.dev godoc of `Server.Adapter`,
    `Server.RoomLen` and `Server.Rooms` in the form 1.D owns (unversioned; a link merged as `@master`
    is stripped by the 1.D link-form commit), which the tag-time release commit
    (`CONTRIBUTING.md`) pins to `@v1.5.0`. It adds no entry under `### Fixed` or
    `### Changed`.
  - The `Server.Adapter` godoc says that a namespace receives peers' broadcasts and
    requests and is counted by them only once Redis has registered its
    subscription, which handler registration does not wait for, and again only once
    a lost subscription has been reopened. The `RoomLen` and `Rooms` godoc say, for a
    namespace registered after `Adapter`, that they wait the full 5 s when this
    instance has not yet registered its subscription or an instance that Redis
    counts does not answer (`Rooms` also when Redis cannot report that count); that
    an instance that has not registered is not counted, so they return without its
    rooms; and that in each case `RoomLen` can undercount and `Rooms` can omit rooms.
  - *Check (1C join gate, and again on the `v1.5.0` release commit at tag time):* `make lint`
    passes; `CHANGELOG.md` has exactly one `### Known limitations` heading in all of
    the `## Unreleased` section and, on the release commit, the `v1.5.0` section
    (whatever date suffix its heading carries; an empty `## Unreleased` may stay
    above it), so
    `awk '/^## /{s=$0} s ~ /^## \[?(Unreleased|v1\.5\.0)\]?( |$)/ && /^### Known limitations$/{c++} END{exit c!=1}' CHANGELOG.md`
    exits 0 (at `82aa740` it exits 1). The awk counts across the Unreleased and `v1.5.0`
    sections, so it is meant for the `v1.x` tree and the tagged `v1.5.0` tree, not for
    `master` after step 0d, whose own `## Unreleased` may later carry its own entries;
    `for m in Server.Adapter Server.RoomLen Server.Rooms; do go doc . $m | grep -q subscription || echo $m; done`
    prints nothing (a case-sensitive substring match; at `82aa740` it prints all
    three). The grep shows only that each comment was edited; the content check is
    the owner's review of the subsection and the three godoc comments against 1.R.

DoD: `make lint test-race` green on ubuntu/macos/windows for `stable` and `oldstable`;
an additional Ubuntu job builds/tests the root on Go 1.22 with automatic toolchain
upgrades disabled. From v2 this job covers every shipped runtime module;
`govulncheck` clean: with the newest stable Go release and a `govulncheck` built by that
same Go (`go install golang.org/x/vuln/cmd/govulncheck@latest` under that toolchain),
`govulncheck ./...` exits 0 in the root module and
`for d in _examples/*/go.mod; do (cd "$(dirname "$d")" && govulncheck ./...) || echo "$d"; done`
prints nothing; a standard-library finding is cleared by the toolchain, not by code;
two-instance Redis test under `-race` passes; every (case, side)
pair of the 1.B, 1I and 1.L test lists is named by a passing test (see 1.B *Gate record*); `engineio/session` coverage ≥ 70%, root
package ≥ 60%; `CHANGELOG.md` lists every fix with the issue or line it addresses.
The 1.K check passes (1.K says when it runs and on which trees).
Logging gate: `TestLogLevelFromEnv`, `TestLogLevelInvalidEnv` (also asserting that
stderr contains the message `logger: invalid level ignored` and `value=bogus`),
`TestWrapOverridesHandlerLevel` and `TestTraceDisabledNoAlloc` pass; the package
`logger` godoc documents the variable, the levels, the message pattern and the keys;
`make lint` passes with the `Deprecated:` notices in place. Links: every badge in
`README.md` shows the fork's status; `engineio/README.md` has no install or example
code, links to `README.md`, `docs/PROTOCOL.md` and its godoc, and `CLAUDE.md` has its
row; `logger/README.md` does not exist; `CONTRIBUTING.md` has the `v1.5.0` link-switch
release step. Each of the first five commands below exits 1
with no output, and the last pipeline prints nothing:

```sh
git grep -nE '(^|[^[:alnum:]_])(log|fmt)\.Print' -- '*.go' ':(exclude)*_test.go' ':(exclude,glob)**/_examples/**' ':(exclude).github'
git grep -nE '\.(Error|Info)\("' -- '*.go' ':(exclude)*_test.go' ':(exclude,glob)**/_examples/**' ':(exclude).github' ':(exclude)logger'
git grep -nE 'https?://godoc[.]org' -- '*.md'
git grep -nE 'pkg\.go\.dev/[^) ]*@|go get github\.com/sshaplygin/go-socket\.io(/[a-z_/]+)?([^@a-z_/]|$)' -- README.md engineio/README.md CHANGELOG.md
git grep -n '@master' -- README.md engineio/README.md CHANGELOG.md
git grep -nE '\.(Debug|Warn)\("' -- '*.go' ':(exclude)*_test.go' ':(exclude,glob)**/_examples/**' ':(exclude).github' | grep -vE '\.(Debug|Warn)\("(engineio|socketio|logger): [a-z][a-z0-9 ]*"[,)]'
```

Acceptance: `_examples/default-http` works unchanged against `socket.io-client` 2.x.
Owner runs `SOCKETIO_LOG_LEVEL=debug go run .` in
`_examples/default-http`, opens the browser page, sends one event and closes the tab:
with one `sid`, the log shows `engineio: session open`, `socketio: namespace connect`
for `/`, `socketio: disconnect` for `/` and `engineio: session close` (the pre-chat
example the release commit was accepted on also connected `/chat`).
Closing the tab gives `reason` `transport error` or `ping timeout`, because
socket.io-client 1.x and 2.x send no CLOSE then; `transport close` is covered by 1L-T1.
Unset, the application's handler controls the level; invalid values behave as 2.4
specifies. Every badge and link in `README.md` resolves on GitHub, and every pkg.go.dev
URL in `README.md`, `engineio/README.md` and `CHANGELOG.md` answers HTTP 200 (needs
network; prints nothing; the same check runs on `$CUT` in the `v1.x` gates):

```sh
test -z "$(for u in $(cat README.md engineio/README.md CHANGELOG.md | grep -o 'https://pkg.go.dev/[^) ]*' | sed 's/#.*//' | sort -u); do test "$(curl -s -o /dev/null -w '%{http_code}' $u)" = 200 || echo $u; done)"
```

Tag-time gates (run once, after the owner's declaration and explicit order, when the `v1.5.0` release
commit of [`CONTRIBUTING.md`](../CONTRIBUTING.md#releases) is tagged and pushed; `bash`
and `set -e`, rules as in the Stage 1b DoD; not part of the stage 1 gate). The block
proves that the tag sits on `master` and not on the frozen `v1.x`, that the released files carry
`@v1.5.0` (the `go get` command and the pinned pkg.go.dev links) and no `@master`, that the 1.K check passes on the tagged tree, and that a
consumer that previously used upstream builds against the fork at the tag after
updating its imports, without an upstream-path `replace` directive. Stage 1b runs
`consumer v1.x` from the same definition at M1b closure:

```sh
T=$(mktemp -d); TAG=$T/tag
git fetch --tags origin
git merge-base --is-ancestor v1.5.0 origin/master
test -z "$(git tag --merged origin/v1.x -l 'v1.5.*')"   # the frozen branch carries no v1.5 tag
rel() { git show v1.5.0:CHANGELOG.md | awk '/^## /{s=($0 ~ /^## v1\.5\.0( |$)/)} s'; }
test -n "$(rel)"
test -z "$({ git show v1.5.0:README.md; git show v1.5.0:engineio/README.md; rel; } | grep -F '@master')"
git show v1.5.0:README.md | grep -q '@v1\.5\.0'
git show v1.5.0:engineio/README.md | grep -q '@v1\.5\.0'
rel | grep -q '@v1\.5\.0'
git show v1.5.0:CHANGELOG.md | grep -q '^## v1\.5\.0 - '
test -z "$(git show v1.5.0:CHANGELOG.md | awk '/^## /{s=$0} s ~ /^## Unreleased/ && /^### /')"   # no Unreleased entries remain
test -z "$(git show v1.5.0:README.md | grep -F 'until a release')"
# the 1.K check on the tagged tree: its two commands are read from the tagged copy of this file
git worktree add --detach $TAG v1.5.0
K1=$(sed -En 's/^ *`(awk .*CHANGELOG\.md)`$/\1/p' $TAG/docs/ROADMAP.md)
K2=$(sed -En 's/^ *`(for m in .*done)`$/\1/p' $TAG/docs/ROADMAP.md)
test -n "$K1"
test -n "$K2"
(cd $TAG; eval "$K1")
test -z "$(cd $TAG; eval "$K2")"
git worktree remove --force $TAG
# consumer build; the same function takes master before the tag
consumer() ( : ${T:?set T to a mktemp -d directory first}; d=$(mktemp -d $T/c.XXXXXX) && cd $d && go mod init example.com/consumer && printf 'package main\n\nimport _ "github.com/sshaplygin/go-socket.io"\n\nfunc main() {}\n' >main.go && GOPROXY=direct go get github.com/sshaplygin/go-socket.io@$1 && go build ./... )
consumer v1.5.0
```

## Stage 1b. Package layout (prerequisite to stage 2)

**History.** Stage 1b closed (M1b) before the restructure and is not re-run. Its layout
(target tree, `api` block, map) describes the `v2/` tree, paths relative to `v2/`; the
repository root keeps the pre-1b names of the v1 tree. Superseded by the 2026-10-10
decisions: the sentence of *Base and branch `v1.x`* that makes the `v1.5.0` tag on `v1.x` at M4
(the Tags decision replaces it), step 0 and its branch-specific files, the `v1.x` gates block
and `consumer v1.x` (list in *Repository restructure*); the DoD and Acceptance blocks passed on `master` at M1b.

Structural refactoring only: moves, explicit file merges, import rewrites and the API
changes listed below; no behaviour change or new features. Keep the cyclic v1 root core
together until the atomic transition in 2.0: the memory and Redis broadcast
(`broadcast.go`, `redis_broadcast.go`, `adapter_options.go`, `helpers.go`), their tests,
redigo and `Server.Adapter` stay in the root package unchanged until 2.0 removes them
with the legacy runtime; nothing Redis-related is built before M4 (stage 4b). Tests
follow their files (same rename).
Step 0a is a branch push; steps 0b to 0d and steps 1–3 are separate PRs, merged in this order with
`make lint test-race examples` green on the target branch after each: step 2 edits the files step
1 creates, and step 3 renames or merges root files whose tests step 2 edits
(`connection_handlers_test.go`). Commit subjects and PR titles
are `refactor(1b.<step>): ...`. Within a PR, pure `git mv` commits (no content edit)
come first, then merges into a target file, then content edits; intermediate commits
may not build, the head of each PR does. Steps 1–3 are the PRs merged with their
commits kept ([`CONTRIBUTING.md`](../CONTRIBUTING.md) rule 5).

**Base and branch `v1.x` (this section owns the cut; no other section creates the
branch).** The branch is `v1.x`; why it is not `v1`, the tagging rule and the release
commit are in [`CONTRIBUTING.md`](../CONTRIBUTING.md#releases). `$CUT` is the last `master` commit with CI green once the pre-cut PRs have
merged: every stage 1 task and the 1.D link-form commit (`go get` moves from `@master` to `@v1.x`
and pkg.go.dev links lose `@master`, so both branches carry it). The branch is cut from `$CUT` without a tag, before the first 1b
commit. The tag `v1.5.0` is made later on `v1.x`, only on the owner's command, at M4
(Milestones). `$CUT` replaces the tag as the
base of the DoD diffs; each 1b PR records `CUT=<sha>` on its own line of the body (the
issue #2 ledger holds the same line). Between `$CUT` and the merge of step 3 only
`refactor(1b.` commits change Go files (tests included) on `master`; Go files under
`_examples/` and `engineio/_examples/` are outside the freeze, and `make examples` is
their build gate. A PR that only adds files under `_experiments/<name>/` (a standalone
module, rule in Stage 2 *Prepared components*) is exempt from the freeze and
from the map and subject gates; the live tree is not
exempt until G2. A `v1.x` fix is made
on `v1.x` and forward-ported after step 3 (rule in
[`CONTRIBUTING.md`](../CONTRIBUTING.md#releases)), so it never conflicts with a rename.
A flaky test that fails on `master` during the freeze is re-run by the integrator; one that
stays red stops the 1b merges and goes to the owner, and a fix the owner allows travels in
the next step's PR under its `refactor(1b.<step>)` subject, with the reason in the body.
Step 0 precedes steps 1 to 3:

- 0a. `git branch v1.x $CUT && git push origin v1.x`; no tag.
- 0b. One PR into `v1.x`, `.github/` only: `ci.yaml` (`push`, `pull_request`) and
  `benchmarks.yml` (`pull_request`) list `branches: [v1.x]`. Workflow files are read from
  the branch under test, so `master`'s copies do not apply to `v1.x`.
- 0c. One PR into `master`, `.github/dependabot.yml` only: a second `gomod` and a
  second `github-actions` entry, otherwise identical, with `target-branch: v1.x` (Dependabot
  reads its configuration from the default branch). The weekly CI cron runs on `master`
  only, so `v1.x` has no scheduled vulnerability scan. The `target-branch` entries give
  version-update PRs only (security updates always use the default branch); the
  vulnerability check of `v1.x` is the Stage 1 `govulncheck` gate, which the owner runs on
  `v1.x` before the M4 tag.
- 0d. One PR into `master`, `CHANGELOG.md` only: the `## Unreleased` section that `v1.x`
  carries is renamed `## v1.5.0 (unreleased, branch v1.x)` and an empty `## Unreleased`
  is added above it, so that master's own entries never share a section with the entries
  of the v1 release (rule in [`CONTRIBUTING.md`](../CONTRIBUTING.md#releases)). `v1.x`
  keeps `## Unreleased`.

**Breaking changes on `master` (`v1.x` keeps the old API; no aliases).** Recorded for
`docs/MIGRATION.md` (2.5). The `api` block is the complete list of exported-signature
changes: a gate pattern, then what it means. Each pattern is anchored to the diff side
and the package (`^[<>] /engineio/session: `) and names its symbol, so the same word in
another package, or an extra symbol in a listed one, is a diff. The DoD diffs the exported API of every
package against `$CUT` and allows only lines that match a pattern. Consumers of
`go get ...@master` break. The 1b PRs write no `CHANGELOG.md` entry (the exemption
that [`CONTRIBUTING.md`](../CONTRIBUTING.md) rule 3 allows): the `api` block and the map
are their record, and 2.5 carries it into `docs/MIGRATION.md`. Path:line references to
pre-1b files in `CHANGELOG.md` (the `engineio/client.go` and `engineio/dialer.go`
entries) name the tree at `$CUT`, which is the `v1.x` tree, and are not rewritten on
`master`: they sit in the `v1.5.0` section as history.

```api
^> /engineio/client: (package client // import |type Dialer struct \{( \| [[:space:]]Transports \[\]transport\.Transport)?$|type Opener interface \{( \| [[:space:]]Open\(\) \(transport\.ConnParameters, error\))?$|func \(d \*Dialer\) Dial\(urlStr string, requestHeader http\.Header\) \(engineio\.Conn, error\)$) # new package of step 1: exactly the package clause, `Dialer` with its `Transports` field and `Dial`, and `Opener` with `Open`; any other exported symbol there is a diff
^> /engineio/internal/logtest: (package logtest|type Recorder struct \{$|func NewRecorder\(|func SetDefault\(|func \([a-z]+ \*?Recorder\) (Find|Enabled|Handle|WithAttrs|WithGroup)\() # new test-support package of step 1: exactly `Recorder`, `NewRecorder`, `SetDefault` and the methods `Find`, `Enabled`, `Handle`, `WithAttrs`, `WithGroup` (the `slog.Handler` methods); any other exported symbol there is a diff. The patterns pin the symbols, not their parameter lists, which the test-support package leaves to the diff review
^< /engineio: (type (Dialer struct|Opener interface) \{|func \(d \*Dialer\) Dial\() # engineio.Dialer, Dialer.Dial and engineio.Opener leave engineio for engineio/client (removal side only); an alias in engineio would import it and restore the cycle
^[<>] /engineio: type Conn interface \{ \| [[:space:]](NextReader\(\) \((session\.FrameType|frame\.Type), io\.ReadCloser, error\)|NextWriter\([a-zA-Z]+ (session\.FrameType|frame\.Type)\) \(io\.WriteCloser, error\))$ # engineio.Conn.NextReader and NextWriter, session.FrameType to frame.Type; breaks every external implementer of Conn
^[<>] /parser: type Frame(Reader|Writer) interface \{ \| [[:space:]](NextReader\(\) \((session\.FrameType|frame\.Type), io\.ReadCloser, error\)|NextWriter\([a-zA-Z]+ (session\.FrameType|frame\.Type)\) \(io\.WriteCloser, error\))$ # parser.FrameReader.NextReader and parser.FrameWriter.NextWriter, same change; breaks every caller of parser.NewDecoder and parser.NewEncoder with its own reader or writer
^[<>] /engineio/session: func \(s \*Session\) (NextReader\(\) \((FrameType|frame\.Type), io\.ReadCloser, error\)|NextWriter\([a-zA-Z]+ (FrameType|frame\.Type)\) \(io\.WriteCloser, error\))$ # session.Session.NextReader and NextWriter, same change
^< /engineio/session: (const \(|type FrameType frame\.Type$) # session.FrameType, TEXT and BINARY give way to frame.Type, frame.String and frame.Binary (removal side only)
```

Target tree (root module; rows added after 1b are marked):

| Path | Package | Holds |
| --- | --- | --- |
| `.` | `socketio` | public API; the legacy root runtime, including the memory and Redis broadcast, `Server.Adapter` and redigo, is removed in 2.0 |
| `engineio/` | `engineio` | server side: `Server`, `Conn`, options; `hooks.go` (hook types since 2.0, fire points in 2.4) |
| `engineio/client/` | `client` | Engine.IO client: `Dialer`, `Opener`; imports `engineio` for `engineio.Conn` only |
| `engineio/internal/logtest/` | `logtest` | log recorder shared by the `engineio` and `engineio/client` tests: `Recorder`, `NewRecorder`, `Recorder.Find`, `SetDefault` and the `slog.Handler` methods |
| `engineio/session`, `frame`, `packet`, `payload`, `transport/...`, `internal`, `parser/`, `logger/` | same packages; only the file changes in the map (`session`, `packet`) and the signatures in the `api` block (`session`, `parser`) differ | `engineio/internal` holds the 1.L shutdown hook |
| `adapter/codec/` (2.2), `adaptertest/` (4b), `client/` (2.3, root client removed in 2.0), `contrib/otel/` (2.4) | later | not present at 1b; no `adapter/` directory exists before 2.2 |

Source-to-target map. Its owner is this block; the PR body of each step repeats the
rows of that step. `-` means no old path (a file the step creates) or no new path (a
deleted file); rows that merge several files list each old path. Test files that stay
at their path are described in the steps, not here.

```map
1 engineio/client.go engineio/client/client.go
1 engineio/dialer.go engineio/client/dialer.go
1 - engineio/client/client_log_test.go
1 - engineio/export_test.go
1 - engineio/internal/logtest/logtest.go
2 engineio/connect.go engineio/conn.go
2 engineio/server_options.go engineio/options.go
2 engineio/types.go engineio/options.go
2 engineio/server_options_test.go engineio/options_test.go
2 engineio/session/base.go -
2 engineio/session/session_manager.go engineio/session/manager.go
2 engineio/session/session_manager_test.go engineio/session/manager_test.go
2 engineio/session/session_id_generator.go engineio/session/id_generator.go
2 engineio/packet/fake_discarder.go engineio/packet/fake.go
2 engineio/packet/fake_frame.go engineio/packet/fake.go
2 engineio/packet/fake_reader.go engineio/packet/fake.go
2 engineio/packet/fake_writer.go engineio/packet/fake.go
3 connection_handlers.go packet_handlers.go
3 connection_handlers_test.go packet_handlers_test.go
3 namespace_handlers.go namespace_handler.go
3 namespace_conn.go namespace.go
3 namespaces.go namespace.go
3 handler.go event_handler.go
3 handler_test.go event_handler_test.go
```

1. **`engineio/client`.** `Dialer` and `Opener` move with `client.go` and `dialer.go` to
   package `client`. Root files import it as `eioclient` (root `client.go`,
   `lifecycle_test.go`, `server_test.go`, `log_test.go` for `Opener`), because locals
   and the 2.3 package `client/` would clash with `client`. An in-package `engineio`
   test cannot import `engineio/client` (import cycle), so only external `engineio_test`
   files may. Tests: `server_test.go`, `server_close_test.go` and the server-side
   `TestSessionCloseRecord` with `logFixture` (from `session_log_test.go`) become package
   `engineio_test`, and every `Opener` assertion in them (`f.cl.(Opener)`, `p.(Opener)`) becomes `client.Opener`;
   `engineio/export_test.go` exposes `ConnChanLen(*Server) int` for the two checks of
   `Server.connChan`. `TestDialFailureRecords`, `TestClientPeerCloseRecords`,
   `TestClientPingFailureRecord`, `TestClientReaderCloseRecord`, `brokenConn` and
   `brokenReader` move to `engineio/client/client_log_test.go` (package `client`; they
   need `engineio.NewServer` and `engineio.Options`). `recorder`, `newRecorder`,
   `recorder.find` and `setDefault` move to `engineio/internal/logtest` as `Recorder`,
   `NewRecorder`, `Recorder.Find` and `SetDefault`; the `slog.Handler` methods of the
   recorder (`Enabled`, `Handle`, `WithAttrs`, `WithGroup`) become exported with it, which
   the `api` block lists. The package imports no package of this repository, so in-package `engineio`
   tests may use it without an import cycle; `readAll` is defined locally in each of the two test packages. `engineio/_examples` builds (DoD) and takes no change other
   than the import-path edits that the Acceptance filter below allows.
2. **Names and frame type.** Files per the map; `fake.go` stays a non-test file because
   `NewFakeConnReader`, `NewFakeConnWriter`, `NewFakeConstReader` and `FakeDiscardWriter`
   are exported. `session.FrameType`, `TEXT` and `BINARY` become `frame.Type`,
   `frame.String` and `frame.Binary` in every user: root `connection.go`
   (`queueWriter.NextWriter`, `frameReader.NextReader`), `engineio/conn.go` and
   `engineio/client/client.go`, `parser/decoder.go`, `parser/encoder.go`, and the tests
   `backpressure_test.go`, `connection_handlers_test.go`, `lifecycle_test.go`,
   `log_test.go`, `server_test.go`, `engineio/server_test.go`, `parser/decoder_test.go`,
   `parser/encoder_test.go`; inside `engineio/session` the unqualified uses in `session.go`
   (`NextReader`, `NextWriter`) and `session_lifecycle_test.go`. Root test locals named
   `frame` shadow the package: rename every one to `msg` in files that import
   `engineio/frame` (`backpressure_test.go:65` fails to compile otherwise). Test
   expectations keep the typed `frame.Type` values.
3. **Root file names by role.** Files per the map: `namespaces.go` joins `namespace.go`
   because it is the registry of `*namespaceConn`, `namespace_handlers.go` joins
   `namespace_handler.go`. `CLAUDE.md` edits made by this step: one layout row per
   directory the layout check lists (`engineio/client/`, `engineio/internal/logtest/`
   and every other package directory), the `engineio/` row without "and client", the `logger/` row naming `engineio/client` in place of "client dialer",
   and `handler.go` → `event_handler.go` in Conventions. Stage 2 paths already match.

DoD, run with `bash` and `set -e` on the committed head of step 3 (M1b closure). The
build, test and `go doc` lines print their usual output; every other line, the `test`
and `ge` lines included, exits 0 and prints nothing. Every gate line in the DoD, `v1.x`,
Acceptance and tag-time blocks is one command or one `test`: none starts with `!` and
none is an `&&` or `||` list, because `set -e` ignores both (a failing non-final member
of a list does not stop the shell); a chain inside `$(...)` is judged by the output it
prints. Scratch files live in `$T`, outside the tree:

```sh
CUT=${CUT:?the SHA recorded in the PR bodies}; MOD=$(go list -m); T=$(mktemp -d); BASE=$T/base
git worktree add -q --detach $BASE $CUT
FIRST=$(git log --reverse --format='%H %s' $CUT..HEAD | awk '/ refactor\(1b\./{print $1; exit}')
make lint test-race examples
go build -o /dev/null ./engineio/_examples   # -o: a bare build of one main package writes the binary `_examples`, which collides with the directory of that name; belongs to the root module; make examples does not build it (it builds ./_examples/client and the example modules)
go test -count=1 ./.github/benchmarks ./.github/benchmarks/report
go test -run '^$' -bench . -benchtime=1x ./... >/dev/null   # the benchmark job's input still compiles and runs
# layering
go list ./engineio/client >/dev/null   # the package exists: go list errors are not read as empty output
DEPS=$(go list -f '{{join .Deps "\n"}}{{"\n"}}{{join .Imports "\n"}}{{"\n"}}{{join .TestImports "\n"}}' ./engineio)   # a go list error (an import cycle included) stops the shell here
test -z "$(echo "$DEPS" | grep "^$MOD/engineio/client$")"
test "$(cat $(ls engineio/client/*.go | grep -v _test.go) | sed 's,//.*,,' | grep -o 'engineio\.[A-Za-z]*' | sort -u)" = engineio.Conn   # code only: a comment that names engineio.Server does not count
test -z "$(grep -rn --exclude='*_test.go' "[A-Za-z_.] \"$MOD/engineio\"" engineio/client)"   # no aliased or dot import, which the line above would not see
# removed API stays removed
test ! -e engineio/session/base.go
test -z "$(go doc -all ./engineio/session | grep -E '\b(FrameType|TEXT|BINARY)\b')"
test -z "$(go doc ./engineio Dialer 2>/dev/null)$(go doc ./engineio Opener 2>/dev/null)"
test -z "$(grep -rn 'session\.\(FrameType\|TEXT\|BINARY\)' --include='*.go' .)"
# map: old paths gone, new paths present, no other non-test Go file added or deleted
awk '/^```map$/{m=1;next} /^```$/{m=0} m{print $2, $3}' docs/ROADMAP.md >$T/map.txt
test -s $T/map.txt   # an empty map would make the checks below vacuous
test -z "$(while read o n; do { [ "$o" = - ] || [ ! -e "$o" ]; } && { [ "$n" = - ] || [ -e "$n" ]; } || echo "map: $o $n"; done <$T/map.txt)"
chg() { git diff --no-renames --name-only --diff-filter=$1 $CUT HEAD -- '*.go' ':(exclude)*_test.go' ':(exclude,glob)**/_examples/**' ':(exclude,glob)_experiments/**' | sort; }
test -z "$(comm -13 <(awk '{print $2}' $T/map.txt | sort -u) <(chg A))"
test -z "$(comm -13 <(awk '{print $1}' $T/map.txt | sort -u) <(chg D))"
test -z "$(git log --format=%s $CUT..HEAD -- '*.go' ':(exclude,glob)**/_examples/**' ':(exclude,glob)_experiments/**' | grep -v '^refactor(1b\.')"
# _experiments stays standalone: no root-module import, every Go file under a go.mod of its own, no go.work
test -z "$(go list -deps -test -f '{{.Dir}}' ./... | grep '/_experiments/')$(git ls-files go.work)$(git ls-files '_experiments/*.go' | while read f; do d=$(dirname $f); until [ -e $d/go.mod ] || [ $d = _experiments ]; do d=$(dirname $d); done; [ -e $d/go.mod ] || echo $f; done)"
# prefix rule: no directory has more than two non-test files sharing a <prefix>_
pkgdirs() { find . \( -name _examples -o -name _experiments -o -name .github -o -name .git \) -prune -o -name '*.go' ! -name '*_test.go' -print | xargs -n1 dirname | sort -u; }
test -z "$(for d in $(pkgdirs); do ls $d/*.go | grep -v _test.go | xargs -n1 basename | sed -n 's/^\([A-Za-z0-9]*\)_.*/\1/p' | sort | uniq -c | awk -v d=$d '$1>2{print d,$2,$1}'; done)"
# regression: no test or subtest result, (test, Covers id, sides) triple or per-test assertion lost
res() { (cd $1 && go test -count=1 -v ./... | awk '$1=="---" && $2~/^(PASS|SKIP):/{gsub(/0x[0-9a-f]+/,"0x"); print $2,$3}'; go test -list '^(Benchmark|Fuzz|Example)' ./... | grep -E '^(Benchmark|Fuzz|Example)') | sort; }
test -z "$(comm -23 <(res $BASE) <(res .))"   # a multiset: a name in two packages counts twice
tests() { (cd $1 && find . -name '*_test.go' -not -path './_examples/*' | xargs awk "$2" | sort); }
covers='FNR==1{n=0} /^\/\/ Covers /{sub(/^\/\/ Covers /,""); k[n++]=$0} /^func /{split($2,f,"("); for(i=0;i<n;i++)print f[1],k[i]; n=0}'
test -z "$(comm -23 <(tests $BASE "$covers") <(tests . "$covers"))"   # 52 ids and 4 free-text markers at 7a7a71d
asserts='/^func /{split($2,f,"("); fn=f[1]} {c[fn]+=gsub(/(assert|require)\.[A-Za-z]+\(|t\.(Fatal|Error)f?\(/,"&")} END{for(k in c)if(k~/^Test/)print k,c[k]}'
test -z "$(join <(tests $BASE "$asserts") <(tests . "$asserts") | awk '$3<$2')"   # per test; the diff review stays
# coverage not below the pre-1b numbers: same -coverpkg method on both trees
cov() { d=$1; shift; (cd $d && go test -count=1 -coverpkg="$(echo $* | tr ' ' ,)" -coverprofile=$T/c.out "$@" >/dev/null && go tool cover -func=$T/c.out | awk '/^total:/{print $3+0}'); }
ge() { awk -v n=$# -v a="$1" -v b="$2" 'BEGIN{exit !(n==2 && a~/^[0-9]+(\.[0-9]+)?$/ && b~/^[0-9]+(\.[0-9]+)?$/ && a+0>=b+0)}'; }   # exactly two numeric figures, else it fails
ge "$(cov . ./engineio ./engineio/client)" 75.8   # every operand is quoted: a failed or empty cov is an empty argument, which ge rejects
ge "$(cov . .)" "$(cov $BASE .)"
ge "$(cov . ./engineio/session)" "$(cov $BASE ./engineio/session)"
ge "$(cov . ./parser)" "$(cov $BASE ./parser)"
```

The per-test assertion count (the `asserts` gate) counts only `(assert|require).X(` and
`t.Fatal`/`t.Error` calls: a removed `should.X(` or `must.X(` call, or an in-place
weakening of an assertion, is left to the diff review.

A floor is the figure `cov` gives on `$CUT` for the package that held the code before 1b
(at `7a7a71d`, Go 1.25.5: root 92.4%, `engineio/session` 74.8%, `parser` 78.8%); `HEAD` is
measured over the new packages together with the same `-coverpkg` method. `engineio` alone
varies between runs (75.8% in two of three, 76.7% in one, at identical code), so the
figure is bimodal and its floor, the `75.8` in the `ge` line, is the lower mode.

The `v1.x` gates (variables as in the DoD; run with `bash` and `set -e`, rules as in the
DoD). Each group runs at the stated moment, not later, because `v1.x` receives patches
afterwards:

```sh
# every 1b PR: the body records $CUT; $CUT carries the branch link form and had CI green
gh pr view --json body --jq .body | tr -d '\r' | grep -qx "CUT=$CUT"
test -z "$(for f in README.md engineio/README.md CHANGELOG.md; do git show $CUT:$f; done | grep '@master')"
git show $CUT:README.md | grep -q '@v1\.x'
test -z "$(for f in README.md engineio/README.md CHANGELOG.md; do git show $CUT:$f; done | grep -o 'https://pkg.go.dev/[^) ]*' | grep '@')"
test -z "$(for u in $(for f in README.md engineio/README.md CHANGELOG.md; do git show $CUT:$f; done | grep -o 'https://pkg.go.dev/[^) ]*' | sed 's/#.*//' | sort -u); do test "$(curl -s -o /dev/null -w '%{http_code}' $u)" = 200 || echo $u; done)"
git show $CUT:README.md | grep -q 'until a release'   # the sentence the tag-time absence check relies on (1.D)
test "$(gh run list --branch master --workflow CI --commit $CUT --json conclusion --jq '.[0].conclusion')" = success
# before step 0b merges: the branch is at $CUT, and no v1.5 tag exists (it is made at M4)
test "$(git rev-parse origin/v1.x)" = "$CUT"
test -z "$(git tag -l 'v1.5.*')"
# after step 0b merged, before the first v1.x patch: v1.x differs from $CUT in .github/ only
git merge-base --is-ancestor $CUT origin/master
git merge-base --is-ancestor $CUT origin/v1.x
test -z "$(git diff --name-only $CUT origin/v1.x | grep -v '^\.github/')"
# at M1b closure: step 0d on master (v1.x keeps Unreleased), triggers, Dependabot and the CI run of the current v1.x head
# the next check belongs to the period before the tag: the release forward-port renames that heading, so it is not re-run after it
test "$(git show origin/master:CHANGELOG.md | grep -c '^## v1\.5\.0 (unreleased, branch v1\.x)$')" -eq 1
test "$(git show origin/master:CHANGELOG.md | grep -c '^## Unreleased$')" -eq 1
test "$(git show origin/v1.x:CHANGELOG.md | grep -c '^## Unreleased$')" -eq 1
test "$(git show origin/v1.x:.github/workflows/ci.yaml | grep -c 'branches: \[v1\.x\]')" -eq 2
test "$(git show origin/v1.x:.github/workflows/benchmarks.yml | grep -c 'branches: \[v1\.x\]')" -eq 1
test "$(grep -c 'target-branch: v1\.x' .github/dependabot.yml)" -eq 2
test "$(gh run list --branch v1.x --workflow CI --commit "$(git rev-parse origin/v1.x)" --json conclusion --jq '.[0].conclusion')" = success
test -z "$(git tag -l 'v1.*' --contains $FIRST)"
eval "$(grep '^consumer() ' docs/ROADMAP.md)"   # the one definition, in the Stage 1 tag-time gates; an empty match makes the next line fail
consumer v1.x
```

Acceptance (same shell and rules as the DoD):

```sh
# every example builds (make examples, DoD); a line changed in _examples or engineio/_examples by a refactor(1b. commit is an import-path edit for a moved identifier (other commits are not examined here)
test -z "$(git log -p -U0 --format= --grep='^refactor(1b\.' $CUT..HEAD -- _examples engineio/_examples | grep -E '^[+-]' | grep -vE '^(\+\+\+|---)' | grep -vE 'engineio/(client|frame|session)"|engineio\.(Dialer|Opener)|eioclient\.|session\.(FrameType|TEXT|BINARY)|frame\.(Type|String|Binary)')"
test "$(go doc ./engineio/client | grep -cE '^type (Dialer|Opener) ')" -eq 2
# exported API of every package against $CUT: only the `api` block of Stage 1b may differ; the awk keeps the package clause and drops the package comment (unindented text up to the first section header)
api() { (cd $1 && for p in $(go list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}' ./...); do go doc -all $p | awk -v p="${p#$MOD}" 'NR==1{print p": "$0; h=1; next} h&&/^(CONSTANTS|VARIABLES|FUNCTIONS|TYPES)$/{h=0; next} h{next} /^(\t\t|    |\t\/\/|[})]|[A-Z]+$)/{next} /^[^\t ]/{c=$0; print p": "$0; next} /^\t/{print p": "c" | "$0}'; done | sort); }
ALLOWED=$(awk -F' # ' '/^```api$/{m=1;next} /^```$/{m=0} m{print $1}' docs/ROADMAP.md | paste -sd'|' -)
test -n "$ALLOWED"   # an empty pattern list would make the next line drop every diff line
test -z "$(diff <(api $BASE) <(api .) | grep '^[<>]' | grep -vE "$ALLOWED")"
test -z "$(diff <(api $BASE | grep '^: ') <(api . | grep '^: '))"   # the root exported surface is unchanged: root lines have an empty package prefix
# CLAUDE.md layout: every package directory has a row, every row path exists
rows() { awk -F'|' '/^\| Path/{t=1;next} t&&/^$/{exit} t{print $2}' CLAUDE.md | grep -o '`[^`]*`' | tr -d '`'; }   # the code spans of the Path column only: prose does not count as a row
test -z "$(for d in $(pkgdirs | grep -v '^\.$'); do rows | grep -qx "${d#./}/" || echo "no row: $d"; done)"
test -z "$(rows | grep '/$' | while read p; do [ -e "$p" ] || echo "no path: $p"; done)"
git worktree remove --force $BASE
```

## Stage V1. Complete v1 (repository-root module), then `v1.5.0`

Owner decisions of 2026-10-10 (D1 to D8), 2026-10-13 (D9 to D14) and 2026-10-14 (D15 to D17),
recorded as given; D9 to D17 are quoted in the owner's two comments on PR #68, which its body
cites per answer. The stage starts when row R has passed step C
and ends before the Stage 2 continuation. Paths and `make` targets are those of the repository
root (the v1 module). [PARITY.md](PARITY.md) owns every matrix row (status, evidence, plan,
decision tag); this section owns the order, the PR scope and the gates. Stage 7 (the `client`
package) is executed inside it as PR V1-9.

| ID | Decision (settled) |
| --- | --- |
| D1 | Order: finish and release v1 (the root of `master`) first, with full Socket.IO v4 / Engine.IO v3 support, example implementations and comparable parity with the TS/JS reference (v1: socket.io 2.5.0, engine.io 3.6.2; v2: socket.io 4.x, engine.io 6.x); then continue v2. |
| D2 | In-memory implementation first. Redis work (Except on the Redis path, the `DB` option, binary arguments across instances, the 5 s `RoomLen`/`Rooms` waits of 1.R, a cluster-correct two-instance chat example) is Stage V1R below, the next v1 minor. `v1.5.0` = memory parity + examples + finished Go client, tagged only on the owner's explicit order. |
| D3 | A mixed Go/Node Redis cluster (the `socket.io-redis` wire format) is not in v1: a documented deviation (PROTOCOL.md); the v2 `adapter/codec` carries it. |
| D4 | JSONP is declared unsupported and removed. |
| D5 | New public API is accepted for v1: `Except` on `Server` (memory half in `v1.5.0`), a connect-rejection reason, per-namespace disconnect with the DISCONNECT packet the server then writes (K20, R9), and the other 2.x features the matrix marks ABSENT or PARTIAL unless the owner marks them out. The checklist is the `Plan` column of PARITY.md. |
| D6 | The Go client is finished before the tag: websocket, several namespaces per `Client`, the close packet, and reconnection (the reference client has it, row C3). The `client` package of Stage 7 moves before the tag. |
| D7 | Release gate clients: `socket.io-client` 1.7.4 and 2.5.0, in a CI job (1.0 to 1.3 are not guaranteed). |
| D8 | Fixed as bugs, in the PRs named in PARITY.md: polling binary-mode UTF-16 length (P16, non-ASCII fails); an unknown namespace or rejected CONNECT answers an ERROR packet and keeps the root socket (S10, R8; socket.io 2.5.0 says `Invalid namespace`); `BroadcastToNamespace` duplicates per room (S12); random session id as `base64id` (E15); `Emit(ev, nil)` panic (B1); a wrong-type event argument closes the connection (B2); no payload size limit (E4: the limit is the reference's `maxHttpBufferSize` default, 1e6 bytes, by D1, with an `Options` field to raise it, an `ADD` entry of E4 that V1-0 specifies); no attachment-count cap (R6: a `5999999999999-` header took 19.5 s in one decode); JSONP `j` reflection (P5, closed by D4); `Server.Close` leaves sessions running (S17, E22); Redis `DB` ignored (A10, in V1R). D15 does not reopen any of them; it covers only the defaults they leave open. |
| D9 | (2026-10-13, answer to O1.) Parity means functional parity of the wire and of the documented features with an idiomatic Go API: a row is `PARITY` when a reference client or server cannot tell the difference; the reference's API shape is not copied. |
| D10 | (2026-10-13, answer to O2; not the recommendation.) An `ADD` row that needs a method on the exported `Conn` or `Namespace` adds it directly to the interface. The owner confirmed it after being shown that external implementers and mocks of those interfaces stop compiling: the version has not been raised since the original library, so the API may be broken now (D13), and the break is recorded (V1-14: `### Changed` entry with a migration note). |
| D11 | (2026-10-13, answer to O4; not the recommendation.) `perMessageDeflate`, `httpCompression`, `cookie` (E8, E9, E10) and the `compress` flag (K17, C11) are in `v1.5.0`, as PR V1-15 below; they are not deviations. |
| D12 | (2026-10-13, answer to O5.) The v1 client gets the full reconnection options and the `reconnect*` events of the reference client (C3), as PR V1-11 is written. |
| D13 | (2026-10-13, engineering rules.) Packages are reused between v1 and v2 as far as possible; code is not duplicated without real necessity. Once the API is fixed it is not broken; backward compatibility is kept except across the majors v1 and v2. |
| D14 | (2026-10-13, code sharing between the modules; the recommendation accepted as worded.) The two modules stay independent: no third shared module, no cross-`require`, no `go.work`. The byte-identical foundation (`logger`, `engineio/frame`, the `engineio/transport` interfaces and manager, the session id generator and manager, `engineio/packet`, `transport/utils`, `payload/pauser.go`; about 650 non-test lines was the estimate in the question, the measured count is in *Shared-file drift check*) is kept as copies guarded by a CI drift check, `make sync-check`: a manifest of shared files compared between the root and `v2/` after the import-path rewrite. A file leaves the manifest in the PR that intentionally diverges it, with the reason. The check is its own PR (*Shared-file drift check* below). |
| D15 | (2026-10-14, answer to O3; not the recommendation.) CORS is enabled by default, as in the reference, and an option disables it (S4, P8). The other defaults are aligned to the reference: `pingInterval` 25000 ms and `pingTimeout` 20000 ms of engine.io 3.6.2 (E1, E2), and `Conn.ID()` in a non-root namespace is `/ns#id` (K1). |
| D16 | (2026-10-14, answer to O6; not the recommendation.) The full engine layer as in Node is in `v1.5.0`: the `engine` handle, the `clients` map and count, the engine events (`connection`, `initial_headers`, `headers`, `connection_error`) and the engine socket state (`readyState`, `transport`, `upgraded`); rows S18, E17, E19, E20. Code work, so it adds PRs to this stage: V1-16 and V1-17. |
| D17 | (2026-10-14, tests and examples.) Working examples that run against `socket.io-client` 1.7 and 2.5. Mandatory tests that check the contract between JS and Go and the API coverage. |

**How the plan applies D10, D11, D13, D15, D16 and D17** (plan choices, not the owner's words). The
plan reads D13's "fixed" API as the API released by the first tag of a major line (`v1.5.0`
for v1, `v2.0.0` for v2): inside that major it is not broken, and before the first tag of a
line it may still change. The V1-0
contracts list the methods D10 adds, and PARITY.md tags their rows `D10`. The exported
`Broadcast` follows the same rule (*`Except` with the Redis adapter* below). D11's `compress`
flag also covers server broadcasts (S21), because a server emit and a broadcast reach the same
transports. A break of implementers in a minor release departs from the SemVer rule of
`CONTRIBUTING.md`; D13 allows it because that rule starts with `v1.5.0`, the first tag of
this fork's v1 line, as D4's removal of JSONP in the same release does. A new major is not
open to v1 in any case: `/v2` is the path of the v2 module. D15's CORS option switches a
server-level default off; the per-transport `CheckOrigin` stays as the override for its
transport (V1-18). D15's ping defaults and `Conn.ID` format go to the PRs that own those
files, V1-2 and V1-5, as `CHG` rows; V1-2 changes the close rule to the reference's together
with the values (E1, the V1-2 row), because the values alone would close every idle client:
today's deadline, `PingTimeout` after a PING, is shorter than the advertised `PingInterval`
once both are the reference's. Of the four engine events D16 names, engine.io 3.6.2
(the v1 reference, D1) has `connection` (E18, the pull model of `Accept`) and emits `headers`
on its transports only (`polling.js:405`, `websocket.js:36`, where the cookie is added);
`initial_headers` and `connection_error` are events of engine.io 4.x that the owner named, so
V1-17 defines them over the 3.6.2 behaviour they observe (the handshake response and the
rejections of P11), beside the socket-level events of E19. The engine socket state (E20) adds
methods to the exported `engineio.Conn`, so that interface follows the D10 rule too: external
implementers and mocks of it stop compiling, and V1-14's `### Changed` entry names it. The
plan reads D17's JS/Go contract tests as differential tests, the same scenario run against
the reference Node server and against the Go server with the observable results compared
(V1-19), and its API coverage as two gates, a test for every reference API row of PARITY.md
and the coverage of the Go API (V1-20); the examples are V1-12 and V1-13. D17's
API-coverage gate (V1-20) reads the `// Covers` markers the per-PR DoD already requires; its
Go API coverage is a ratchet from a measured baseline, not a target chosen here. Silence is
not an answer: a decision row is recorded only from a comment of the repository owner on the
PR that records it, which that PR's body cites as `Owner answer O<n>: <answer> <URL of the
comment>` (D9 to D17: the two owner comments on #68); no decision is pending.

**PR sequence.** Commits are titled `<type>(V1.<n>): ...`. A PR whose `Plan` rows include `ADD`
changes the public API only as recorded by V1-0 (CLAUDE.md: public API goes through the roadmap
first). Rows for a PR are the `Plan` entries naming it (`covers V1-<n>` below lists them).
The exported-API diff checks (the Stage 1b `api` block against `$CUT`, step C of the restructure
against `$V1TIP`) close before this stage and are not re-run in it; no Stage V1 gate diffs the
exported API, so the methods D10 adds need no allow-list entry: the V1-0 contracts record them,
V1-14's `### Changed` entry announces them, and Stage 7 compares `client` with `$TIP`, which has them
(the client side of each: Stage 7, *D10 methods and the alias*).

| PR | Scope |
| --- | --- |
| V1-0 | Docs only: signatures and semantics of every `ADD` row as subsections of this stage (the methods added to `Conn`, `Namespace`, `Broadcast` and `engineio.Conn` by D10 and D16, `Options` fields and per-emit flags, those of V1-15, V1-16, V1-17 and V1-18 included, errors, and the close error type of `engineio/session` that V1-4 adds for K24 with the `Writable` methods V1-4 adds for V1-6's K16 as `### V1-4 contract`); three-agent validation (CLAUDE.md) |
| V1-1 | `engineio/payload` and `engineio/transport/polling`: binary payload length in bytes, not UTF-16 units (P16), JSONP removed with its PROTOCOL.md deviation (P5, P6); the CORS default is V1-18's (S4, P8: D15), `OPTIONS` answers (P9) and overlapping polls (P18) are checked by V1-8 |
| V1-2 | `engineio` server and session: random session id, payload limit (1e6 bytes, `Options` field to raise it), upgrade timeout, `allowUpgrades`, JSON error replies, handshake must be GET with the reference answer (HTTP 400 `{"code":2,"message":"Bad handshake method"}`, P12), which also closes `OPTIONS` without `sid` (E14: today it creates a session in `Server.ServeHTTP`), liveness on any packet with the reference's close rule: the session closes with reason `ping timeout` after `PingInterval + PingTimeout` without any received packet, the window re-armed by every packet (engine.io 3.6.2 `socket.js:136-142`; today `session.go:78,223,397-409` arms a deadline of `PingTimeout` alone, re-armed by a PING only, which under the new defaults would close an idle client 20 s after each of its 25 s pings), and the reference defaults `PingInterval` 25 s and `PingTimeout` 20 s (E1, E2: D15; a `### Changed` entry names both values and the window; the test `TestPingTimeoutIdleClient` and its command are in the per-PR checks below), request checker, `Server.Close` closes sessions; the engine half of V1-7's K5, so that it edits no `engineio/` file: the session keeps its handshake request (`Session.SetRequest` and `Request`, `### V1-2 contract`), and `Options.ConnInitor` runs on the handshake request's goroutine, with the request live, before the OPEN packet is written and so before `Accept` returns the session, for a websocket-first handshake as a polling one already does (the `SetContext` workaround of K5's evidence races with the root's copy of the context today; the change is in `### V1-2 contract`) |
| V1-3 | `parser` and root defects: attachment cap, payload validation, `Emit(ev, nil)`, wrong-type argument, per-room duplicates |
| V1-4 | Packets: ERROR written and decoded, unknown namespace keeps the root socket, DISCONNECT written (R9), `0/nsp,` reply form, disconnect reasons (K24: the engine's close cause, recorded by `session.go:134` and only logged today, is exposed from `engineio/session` as an exported error type that `NextReader` returns wrapped after the close, and the root maps it to `transport close`, `ping timeout` and `transport error`; the type is V1-0's `### V1-4 contract`), the writability report `Writable` of the session, the engine client and the polling and websocket connections that V1-6's `Volatile` reads (K16, the same contract; the two are the one `engineio/` edit among V1-3 to V1-7, so V1-4 merges before V1-6 and V1-16, wave V1B), and `disconnecting` order, encode error |
| V1-5 | Namespace API: `Of`, `Use` and event middleware, connect-rejection reason, per-namespace disconnect, `connected`/`sockets`, `send`, `Server.Close` waiting for sessions, `Conn.ID()` as `/ns#id` outside the root namespace (K1: D15; a `### Changed` entry names it) |
| V1-6 | Memory broadcast: `Except` and broadcast except the sender (new `Broadcast` methods; the Redis type returns `ErrExceptUnsupported` and drops a message with unknown `opts` entries or keys), room union with dedupe, id lists, `Join` of several rooms, `volatile` and `local` flags, callback-on-broadcast rule |
| V1-7 | Dynamic namespaces, handshake data (query, headers, request, time), late ack and several listeners per event, adapter injection |
| V1-8 | The `conformance` CI job (D7): Node `socket.io-client` 1.7.4 and 2.5.0 against the Go server, plus the Go client against a Node `socket.io` 2.5.0 server; required check; also the `conformance` Makefile target and the CLAUDE.md lines (Commands, "tests need no external service") for the tag |
| V1-9 | Stage 7 as written below: the `client` package, behaviour-preserving, its own DoD; its commits are titled `<type>(7.<n>)` as Stage 7 requires |
| V1-10 | `client` I: websocket and upgrade, several namespaces per `Client`, DISCONNECT on close, connect timeout and `connect_error`, `path`/`query`/headers, emit buffering |
| V1-11 | `client` II: reconnection with options and `reconnect*` events (D12), `once`/`off`/`id`/`connected`, flags (`compress` is V1-15's) |
| V1-12 | Examples `_examples/ack` and `_examples/binary`, each with its own `go.mod`, a `client.js` (exit 0 on success) and a README. Also owns the `Makefile` `examples` change: every example module is built and vetted (`go build`, `go vet`), the identical-copy `cmp` runs only for directories that hold a `chat.go` (the new ones have none), and a new target `examples-node` starts each example that has a `client.js` on a random port and runs it with `node`, `NODE_PATH` set to `CONFORMANCE_NODE_MODULES` (V1-8: the client install of the conformance job's matrix entry, so 1.7.4 and 2.5.0 both in the job: D17); with the variable unset the target fails with a message naming it, as the tagged tests do, and never skips; a local run installs each version with the V1-8 `npm install` line into a temporary prefix and runs the target once per version (the command block of the per-PR checks and of the MV1 DoD); `_examples/default-http` gets a `client.js` too (`add user` answered by `login`, what the page does), so the existing chat runs under both versions; the conformance job runs the target in each matrix entry; `CLAUDE.md` `make examples` line updated in the same PR |
| V1-13 | Examples `_examples/namespaces` (namespaces, rooms, auth) and `_examples/middleware`, same rules, `examples-node` extended; `_examples/default-http` gets broadcast-except-sender, and every `chat.go` copy changes with it in the same PR, except `redis-adapter` and `redis-adapter-unix-socket`: `Except` is not available over Redis in `v1.5.0` (D2, contract in V1-0), so they keep their `chat.go` with the `others()` helper, named in `CHAT_SKIP` of the Makefile (the copy check is `CHAT_COPIES`, the `chat.go` directories minus `CHAT_SKIP`) until V1R; the browser page for 1.7 (D17): `_examples/asset/index-1.7.html`, the upstream page loading `socket.io-client@1.7.4` from jsDelivr with its integrity hash, beside `index.html` (2.5.0), so every example that mounts `asset/` serves both, and `_examples/README.md` says which URL opens each |
| V1-14 | Release preparation: PROTOCOL.md deviations (D3, D4) as the headings `### Deviation D3: mixed Go/Node Redis cluster` and `### Deviation D4: JSONP unsupported` under *Known deviations from the v3/v4 specs* (the unique strings the MV1 DoD greps for), README supported versions, `CHANGELOG.md` `## Unreleased` complete, v1 migration notes. D10 communication: one `### Changed` entry that names `Conn`, `Namespace`, `Broadcast` and `engineio.Conn` and every method V1-0 added to each, and the one changed signature, V1-6's `Namespace.Join(room string)` to `Join(rooms ...string)` (both forms; callers compile unchanged, implementers do not), says that implementers and mocks of them stop compiling in this minor release, and what a client connection returns for the server-only ones (Stage 7), and carries the migration note (add the methods, or embed the interface in a mock); the default changes of V1-15 are `### Changed` entries too (the `io` cookie set after every polling handshake, polling responses of 1024 bytes or more compressed when the client accepts it), each naming the reference option (`cookie`, `httpCompression`), as are the D15 defaults written by V1-2 (`PingInterval`, `PingTimeout`), V1-5 (`Conn.ID`) and V1-18 (CORS), and V1-5's connect rejection (a middleware or `OnConnect` error answers an ERROR packet and keeps the connection open, where it used to close it; `### V1-5 contract`); two more `### Changed` entries for behaviour an application observes: the `OnDisconnect` reasons of V1-4 (K24: `transport close`, `ping timeout`, `transport error` and `forced close`, where every connection close reported `client namespace disconnect`; `### V1-4 contract`) and V1-5's `Conn.Close`, now the reference's `disconnect(true)` (a DISCONNECT packet per namespace and the reason `server namespace disconnect`, so a reference client reports `io server disconnect` and does not reconnect, where it saw a transport close and reconnected; `### V1-5 contract`); no tag |
| V1-15 | Compression and cookies (D11): `perMessageDeflate` for the websocket transport (E8), `httpCompression` for polling responses (E9), the `cookie` option (E10), the per-emit `compress` flag of `Conn` (K17), of server broadcasts (S21) and of the client (C11); contract and checks in *Compression and cookies (V1-15)* below |
| V1-16 | Engine layer I (D16): `Server.Engine()`, the `*engineio.Server` the socket.io server runs on (S18); `engineio.Server.Clients()`, a snapshot of the open and closing sessions by id, beside `Count` (E17); the engine socket state on `engineio.Conn`, `ReadyState()`, `Transport()` and `Upgraded()` (E20), which the engine-connection accessor of K4 (V1-7) reaches from a socket; contract and checks in *Engine layer (V1-16, V1-17)* below |
| V1-17 | Engine layer II (D16): the engine events as observer hooks on `engineio.Options`: the server-level `connection`, `initial_headers`, `headers` and `connection_error` named by D16, and the socket-level `flush`, `drain`, `packet`, `packetCreate`, `heartbeat`, `upgrading`, `upgrade` and `error` of E19; contract and checks in *Engine layer (V1-16, V1-17)* below |
| V1-18 | CORS (D15): the server-level default that accepts every origin and writes the headers of `polling-xhr.js:48-62` on every polling response (P8), one `engineio.Options` field that disables it (S4), the per-transport `CheckOrigin` kept as the override; `### Changed` entry; contract and checks in *CORS default and option (V1-18)* below |
| V1-19 | Differential JS/Go contract tests (D17): the scenarios of *Differential contract tests (V1-19)* below, each run by the conformance job's Node client against the reference `socket.io@2.5.0` server and against the Go server, the two transcripts compared; new files under `internal/conformance` and its `testdata/node`, tag `conformance`, no new Go dependency |
| V1-20 | API-coverage gate (D17): `make parity-check`, a Go test that fails when a PARITY.md row with a `DONE` entry (with `PARITY_STRICT=1`, also `ADD`, `CHG`, `BUG` or `TEST`) has no `// Covers <ID>` marker above a test function, and `make cover-check`, the statement-coverage floor and the ceiling on exported functions no test executes, both ratchets from the values measured on its head; `Makefile`, `internal/parity`, a `lint-v1` step in `.github/workflows/ci.yaml`, the CLAUDE.md Commands lines; checks in *API coverage gate (V1-20)* below |

**`Except` with the Redis adapter in `v1.5.0` (rows A4, K14, S21; D2, D10).** Decided here by
the rule of D10: the `Except` options are methods added directly to the exported `Broadcast`
interface. That is the simplest mechanism consistent with D10: one interface for the server to
call, the same rule as `Conn` and `Namespace`; `redis_broadcast.go` is in this module and
changes in V1-6 anyway, and an external `Broadcast` cannot be injected before V1-7 (S5), so only
mocks break, the breakage D10 accepts and V1-14 documents. The Redis `Broadcast` implements
the two new methods of `### V1-6 contract` as follows: `SendTo` returns an error wrapping
`ErrExceptUnsupported` and sends nothing only when `opts.Except` is not empty, before any
Redis I/O, and otherwise delivers as the rest of this paragraph says (one message per room
for a union, `Local` and `volatile` below); `IDs` lists this instance's sockets, no error.
D5's `Except` on
`Server`, which V1-6 offers as `Server.Of(nsp).Except(ids...)` (the `ServerNamespace` form,
`### V1-6 contract`; no `Server` method takes a namespace string for it), and
broadcast-except-sender return that error, never a delivery to every instance including the
sender. V1-0 records the names; V1-6 tests the error with a fake `Broadcast` that returns it
(the caller gets it, nothing is sent) and with the Redis type's method, which returns it before
any Redis I/O, so the test needs no Redis server; V1-14 lists it under `### Known limitations`
of the changelog, and V1R implements the methods over Redis and removes the entry. The decoder
`v1.5.0` ships must stay safe when V1R extends the Go-only Redis message, since D13 freezes it
at the tag: today `onMessage` uses `opts[0]` (room) and `opts[1]` (event) and ignores the rest
(`redis_broadcast.go:375-393`), so it would deliver a V1R `Except` broadcast to the excluded
sockets. V1-6 makes it drop and log a message whose `opts` has more than those two entries,
or whose top-level keys are not exactly `opts` and `args`, delivering nothing;
`TestRedisBroadcastDropsUnknownOpts` feeds `onMessage` a message with a third `opts` entry and
`TestRedisBroadcastDropsUnknownKeys` one with an extra top-level key, and each asserts that no
connection receives it (no Redis server). The forward rule for V1R, which that decoder must
survive: every new field of the message goes into `opts`, never into a new top-level key, and
an entry is added only for a non-default value (an exclusion, `volatile`, `compress` false, a
room union or an id list, and a marker whenever B3 changes how `args` are encoded, so that a
`v1.5.0` instance drops such a message instead of decoding its arguments wrongly). A plain
broadcast therefore still reaches `v1.5.0` instances, and in a mixed cluster every broadcast
that carries an entry misses their sockets instead of reaching excluded ones or arriving
corrupted. In `v1.5.0` the Redis `Broadcast` sends a room union (S13) as one message per
room, so the local instance dedupes and another instance delivers once per room a socket is
in, and answers an id list (S15, A5) from this instance's connections only; V1-14 lists both
under `### Known limitations`, V1R carries them.
The V1-6 flags on the Redis `Broadcast`: `local` delivers to this instance's connections and
publishes nothing, as the reference adapter does; `volatile` is honoured for this instance's
connections and not carried, so other instances send normally. V1-6 tests both without a Redis
server (`TestRedisBroadcastLocalFlag`, `TestRedisBroadcastVolatileFlag`; counted in the V1-6
block below), V1-14 lists `volatile` under `### Known limitations` beside `compress`, and V1R carries
it (A4).

**Conformance contract (V1-8).** CI job `conformance` in `.github/workflows/ci.yaml`, a required
check, on ubuntu with the Node LTS release from `actions/setup-node`. The clients are installed
from npm under exact versions, `socket.io-client@1.7.4` and `socket.io-client@2.5.0`, one matrix
entry each (D7), the version written literally in the workflow so that `grep 'socket.io-client@<version>'` finds it. The Go server under test is started by a Go test on a random port;
that test needs Node and `npm`, so it sits behind the build tag `conformance` and is not part of
`make test`, `make test-race` or `go test ./...` (CLAUDE.md: tests need no external service; V1-8
adds the exception for the tag to that sentence). Layout, fixed here: the Go tests are
`internal/conformance/*_test.go`, each file starting with `//go:build conformance`; the scenarios
and `package.json` (`"private": true`, no dependencies: the job installs the client) are
`internal/conformance/testdata/node/`, which the Go build skips. Two installs with separate
prefixes: the job installs the matrix entry's client with `npm install --no-save --prefix
internal/conformance/testdata/node socket.io-client@<version>` and the reference server with
`npm install --no-save --prefix internal/conformance/testdata/node/server socket.io@2.5.0`,
under its own prefix because `socket.io@2.5.0` depends on `socket.io-client@2.5.0`, which must
not shadow the 1.7.4 entry's client (a second `--no-save` install into one prefix recomputes
the tree from a `package.json` that lists neither). The server scripts live in
`testdata/node/server/` (its own `package.json`, `"private": true`, no dependencies) and
resolve `socket.io` from the `node_modules` beside them; the client scripts in
`testdata/node/` resolve `socket.io-client` from theirs. The job exports
`CONFORMANCE_NODE_MODULES=$GITHUB_WORKSPACE/internal/conformance/testdata/node/node_modules`,
the client install as an absolute path, which the tagged tests and `examples-node` (V1-12)
pass to `node` as `NODE_PATH` unchanged, so a script outside `testdata/node` (an example's
`client.js`) finds the same client. The value must be absolute because its consumers run
`node` from other working directories: the tagged tests from `internal/conformance` (the
`go test` cwd of the package) and `examples-node` from each example's directory, where a
path relative to the repository root resolves to nothing. Each consumer checks it
(`filepath.IsAbs` in the tests, `case "$CONFORMANCE_NODE_MODULES" in /*) ;; *) exit 1;; esac`
in the target) and fails naming the variable when it is relative, so a wrong export fails
where it is consumed instead of running a scenario without the client. It then
runs the new `make conformance` target, `go vet -tags conformance ./internal/conformance/... && go test
-race -count=1 -tags conformance ./internal/conformance/...`; under the tag a missing `node` or
`CONFORMANCE_NODE_MODULES` fails the test (a skip would make the job green without running a
scenario), and no `TestMain` checks them, because `-list` runs `TestMain`. `covers` and the
`Covers` markers find tagged tests with `go test -tags conformance ./... -list .` (the helper
below uses it; the default list is a subset). Scenarios,
each run by both clients over polling and over websocket: connect and disconnect on `/`; a
namespace connect, an unknown namespace answered by ERROR with the root socket kept (S10); ack
in both directions; binary event and binary ack in both directions; non-ASCII text (P16);
rooms and broadcast except the sender (K14); middleware rejection with its reason; a server-side
disconnect seen as `io server disconnect` (K20); the upgrade; a reconnect. A second step runs the
Go `client` against a Node `socket.io@2.5.0` server (from V1-10 on), a script in
`testdata/node/server/` started by tagged tests of the same package; V1-8 installs
`socket.io@2.5.0` under the server prefix from the start, so that step and V1-19 edit no
workflow. Rows marked `TEST` in PARITY.md
are closed by a named test: P9 (OPTIONS answer), P15 (websocket framing), P17 (graceful close)
and P18 (two overlapping polls, which a stock client never sends) by Go tests in the same
package that start the server and use raw HTTP and websocket clients, N7 by a Go test in V1-6.

**Compression and cookies (V1-15; rows E8, E9, E10, K17, S21, C11; D11).** V1-0 writes
`### V1-15 contract` with the `engineio.Options` fields and the flag methods. Defaults are
engine.io 3.6.2's (D1, D9; as E4 takes its limit), from its `lib/server.js:47-61,317-329` and
`lib/transports/polling.js:289-300`: `perMessageDeflate` off, threshold 1024 bytes when on;
`httpCompression` on, threshold 1024 bytes, gzip or deflate as `Accept-Encoding` allows, only
when a packet of the response has `compress` set. The cookie `io` (`Path=/`, `HttpOnly`,
`SameSite=Strict`) is set on every response of the polling transport that made the handshake,
since engine.io adds it on that transport's `headers` event (`polling.js:395-405`); a
websocket-first handshake sets none, since 3.6.2 listens for `headers` on the connected `ws`
socket (`transports/websocket.js:31`), which never emits it (ws 7.5.10 emits it on the server
only), and a reference client cannot tell the difference (D9). Fields: the name, the path (no
path: no `HttpOnly`), `HttpOnly` (`cookieHttpOnly`), switching it off, and a full cookie for the
object form of `cookie` (`server.js:319-320`: domain, expiry, `Secure`, `SameSite`). The `compress` flag is true unless set false on an emit
(socket.io 2.5.0); `false` sends that packet uncompressed on either transport. The threshold
applies to the whole encoded packet, which the transport buffers up to the threshold before it
chooses. The E4 limit applies to the inflated message, as `ws` applies `maxPayload` (engine.io
`server.js:111`) after inflating: gorilla's `SetReadLimit` counts wire bytes (v1.5.3
`conn.go:917-924`), so with compression on it is not the E4 check, and a message of a few KB
on the wire must not inflate past 1e6 bytes; the Go client's read limit, when it has one, is
enforced the same way. `### V1-15 contract` states this rule and the close code (1009). The client
offers permessage-deflate with threshold 1024, as `engine.io-client` 3.5.6 and 1.8.6 (the two
gate clients) do under Node, and its emit takes the same flag. Over the Redis `Broadcast` the
flag is not carried: the Go-only Redis message has no flags, so V1-0 keeps the flag apart from
the `Except` options, the Redis type accepts a broadcast with `compress` false without
`ErrExceptUnsupported`, honours it for its own connections and publishes without it, and the
other instances send with the default (`true`). V1-14 lists this under `### Known limitations`
beside the `Except` entry; V1R carries the flag (S21) and removes the entry. V1-15 also updates PROTOCOL.md
(v1 implements them) and adds two tagged tests to `internal/conformance`:
`TestConformancePerMessageDeflate` (the matrix's Node client sends and receives a 4 KB event
over websocket against a Go server with `perMessageDeflate` on; the test asserts from the
recorded upgrade response that `permessage-deflate` was negotiated) and
`TestConformanceClientPerMessageDeflate` (the Go client does the same against Node
`socket.io@2.5.0` with `perMessageDeflate: true`). Entry: V1-2, V1-6 and V1-11
merged (engineio options and handshake, broadcast flags, the client's websocket and flags).
Named tests, each under the `// Covers` of its row: `TestPerMessageDeflateNegotiated` (E8: the
extension is negotiated when on and not when off; a frame below the threshold or sent with
`compress` false has RSV1 clear, read from the raw frame), `TestPerMessageDeflateLimit` (E8
with E4: a compressed message under 1e6 bytes on the wire that inflates to 1e8 bytes closes the
session with 1009 while the process's `runtime.MemStats.TotalAlloc` grows by less than 2e7
bytes; it calls no `t.Parallel`, so no other test of its package binary runs beside it and the
bound measures only that message), `TestPollingHTTPCompression` (E9:
gzip and deflate above the threshold, none below it or without `Accept-Encoding`),
`TestHandshakeCookie` (E10: default, custom name and path, `HttpOnly` off, the full-cookie form,
off; set again on a later poll of the session, absent after a websocket-first handshake), `TestEmitCompressFlag` (K17; a subtest for
`c.Compress(false).To(room)`: the room's other sockets get the packet uncompressed and
the sender gets nothing),
`TestBroadcastCompressFlag` (S21), `TestClientCompress` (C11, untagged: the Go client against an
in-process Go server). These seven need no Node and run under `./...`; the two conformance tests
run in the `conformance` job, green per the per-PR DoD. Besides the per-PR DoD, on its head:

```sh
V15='TestPerMessageDeflateNegotiated|TestPerMessageDeflateLimit|TestPollingHTTPCompression|TestHandshakeCookie|TestEmitCompressFlag|TestBroadcastCompressFlag|TestClientCompress'
test "$(go test -race -count=1 -json -run "^($V15)\$" ./... | jq -r 'select(.Action=="pass" and .Test!=null and (.Test|contains("/")|not))|.Test' | sort -u | wc -l)" -eq 7   # all seven exist and pass
for p in TestPerMessageDeflateNegotiated:E8 TestPerMessageDeflateLimit:E8 TestPollingHTTPCompression:E9 TestHandshakeCookie:E10 TestEmitCompressFlag:K17 TestBroadcastCompressFlag:S21 TestClientCompress:C11; do git grep -h -B4 -E "^func ${p%%:*}[(]" -- '*_test.go' | grep -qE "^// Covers ${p#*:}( |$)" || { echo "no // Covers ${p#*:} above ${p%%:*}" >&2; exit 1; }; done   # each under its row's marker, which covers alone does not prove for rows shared with V1-6 and V1-11
V15C='TestConformancePerMessageDeflate|TestConformanceClientPerMessageDeflate'
test "$(go test -tags conformance -list "^($V15C)\$" ./internal/conformance/... | grep -cE "^($V15C)\$")" -eq 2   # both scenarios exist (-list needs no Node)
```

**Engine layer (V1-16, V1-17; rows S18, E17, E19, E20; D16).** V1-0 writes `### V1-16
contract` and `### V1-17 contract`, with the names; the semantics are fixed here. V1-16:
`Server.Engine()` returns the `*engineio.Server` that `NewServer` created, never nil (S18;
`Server` is a struct, so S18 is not a D10 row). `engineio.Server.Clients()` returns a copy of
the sessions whose `ReadyState()` is `open` or `closing`, keyed by id, taken under the
server's lock, so a caller iterates without holding it. That is the membership of 3.6.2's
`clients` (`server.js:334-341`: a socket enters after its constructor has set it `open`
and leaves on its `close` event): a session registered for its handshake but still
`opening` is not listed, and one whose state is `closed` is not listed from that moment,
whether or not anything has called `Remove`. The filter is on the state, so the session
manager's lifetime rule is unchanged: a session stays registered until `Remove` (which the
socket.io server calls when a connection's read goroutine ends, `server.go:334`), the
init failure or `Server.Close` drops it. `Count()` stays (E16) and keeps counting the
registered sessions, so it can exceed `len(Clients())` by the sessions still opening or
closed and not yet removed, where 3.6.2's `clientsCount` is always `len(clients)`
(`server.js:334-341`): a D9 difference of an existing method, recorded in the evidence
cell of row E16 of PARITY.md, whose status stays `PARITY` because `len(Clients())` gives
the reference value from V1-16 on; `Remove(sid)` stays too. `engineio.Conn` gains `ReadyState()`,
returning one of the four 3.6.2 states as an `engineio.ReadyState` (`opening`, `open`,
`closing`, `closed`, the strings of `socket.js`), `Transport()`, the name of the current
transport (`polling` or `websocket`, so after an upgrade the new one), and `Upgraded()`
(E20; `close(discard)` and `send` are `Close` and `NextWriter`, already there). The methods
are added directly to the interface, as D10 does for `Conn` (*How the plan applies*):
`engineio.Server`'s sessions implement them, the test fakes of this module are updated, and so
is the engine client (`engineio/client.go`, `type client`, which `Dial` returns and the root
`socketio.Client` wraps at `client.go:92`), which already has `Transport()` (`client.go:50`):
its `ReadyState()` is `open` once `Dial` has returned and `closed` after `Close` or after
the ping loop fails, never `opening` or `closing`, since `Dial` returns an open connection
and `Close` is synchronous. On the ping-failure path `pingFailed` only logs: the connection
is closed by the deferred `conn.Close()` of `serve()` (`client.go:113-117`), which bypasses
`client.Close()` and so, today, the `close` channel. V1-16 makes that defer go through the
same state transition as `Close` (the `closeOnce` body, then `conn.Close()`), so
`ReadyState()` is `closed` after the ping loop has returned by any path, and the test
`TestEngineClientState` of `### V1-16 contract` asserts it on that path;
`Upgraded()` is false until V1-10's client upgrade switches the transport and true after
it; `Transport()` keeps its value. Stage 7's *D10 methods and the alias* covers the root
`Conn`; this sentence covers the `engineio.Conn` side of the client, and V1-0's
`### V1-16 contract` lists both. V1-14's `### Changed` entry names `engineio.Conn`. The
socket.io `Conn` reaches them through the
engine-connection accessor of K4, which V1-7 adds under D10: V1-7 returns the `engineio.Conn`,
V1-16 gives it the state, so the two PRs do not conflict. V1-17: the events are observer hooks,
a struct of func fields on `engineio.Options` in the shape of `v2/engineio/hooks.go` (D13: the
same shape, not the same signatures, since v1 has no context plumbing); a nil field is ignored,
a set one is called synchronously on the goroutine of the event, so a hook that blocks delays
that session only, and a hook must not call back into the session it observes. Server-level:
`connection` fires when a session is created, after its handshake and before `Accept`
returns it, so the session is `open` in the hook (3.6.2 `server.js:342`); `initial_headers`
fires once per session with the request and the response headers of the handshake response
(the polling handshake and the websocket upgrade), before they are written; `headers` fires
with the request and the headers of every response of a session (each polling response, the
upgrade response once), after `initial_headers` on the handshake, so V1-15's cookie is visible
in it; `connection_error` fires when `ServeHTTP` rejects a request, with the request, the code
and the message of P11 (0 `Transport unknown`, 1 `Session ID unknown`, 2 `Bad handshake
method`, 3 `Bad request`, 4 `Forbidden`) and the error of a request checker. Socket-level, each
with the `engineio.Conn`: `packet` (a packet read, with its type), `packetCreate` (a packet
about to be written), `heartbeat` (a ping received), `upgrading` and `upgrade` (the probe
accepted, the transport switched), `flush` and `drain` (a polling write batch starts and ends),
`error` (the error that closes the session, before `close`). `connection` is E18's event: the
hook does not replace `Accept`, which stays the way the socket.io server takes sessions.
Named tests, each under the `// Covers` of its row: `TestServerEngine` (S18),
`TestEngineClients` (E17: two open sessions, the map has both ids and `Count` agrees; one
closed by its `Close`, with no `Remove` call, is absent from every snapshot taken after its
`ReadyState` is `closed`, while `Count` still
includes it until `Remove`), `TestEngineConnState` (E20: `open` once `Accept` has returned the session,
`websocket` and `Upgraded` true after the probe, `closing` and `closed` around `Close`;
`opening` is the state between the session's creation and the OPEN packet, which no caller
observes through the API (`Clients()` leaves it out; `Accept` and the `connection` hook get
the session `open`), as in 3.6.2, where the constructor reaches `open` before
`connection` fires (`socket.js:26,45,61`, `server.js:342`), so no test asserts it and V1-17's
`connection` subtest asserts `open`), `TestEngineSocketEvents` (E19: every socket-level hook fires once in a polling
session upgraded to websocket, in the order above), and `TestEngineServerEvents` with the
subtests `connection` (which also asserts `ReadyState` `open` in the hook), `initial_headers`,
`headers` and `connection_error` (the four D16 events; not a row, so counted below). Entry: V1-2 and V1-4 merged (the `engineio` server and session
files V1-2 rewrites; the close cause V1-4 exposes from `engineio/session`, which V1-17's
`error` hook reports). Besides the per-PR DoD, on the V1-17 head:

```sh
E17='TestEngineServerEvents/(connection|initial_headers|headers|connection_error)'
test "$(go test -race -count=1 -json -run "^$E17\$" ./engineio | jq -r 'select(.Action=="pass" and .Test!=null and (.Test|contains("/")))|.Test' | sort -u | wc -l)" -eq 4   # the four D16 server-level events have a passing subtest each
```

**CORS default and option (V1-18; rows S4, P8; D15).** V1-0 writes `### V1-18 contract` with
the field's name. Default, as socket.io 2.5.0 with `origins` at `*:*` over engine.io 3.6.2:
every origin is accepted; a polling response to a request with an `Origin` header carries
`Access-Control-Allow-Origin: <that origin>` and `Access-Control-Allow-Credentials: true`,
one without it carries `Access-Control-Allow-Origin: *` (`polling-xhr.js:48-62`; today
`polling/server.go:63-75` writes them only when a `CheckOrigin` is set); the websocket upgrade
accepts a cross-origin request (today gorilla's same-origin default refuses it when
`CheckOrigin` is nil). The `OPTIONS` answer (P9) is unchanged. One `engineio.Options` field
switches the default off: then the polling transport writes no CORS header and the websocket
transport applies gorilla's same-origin check, the behaviour of `v1.x` with a nil
`CheckOrigin`, so a browser page on another origin is refused by the browser itself.
Mechanism: the decision stays in the transports, which have no access to `Options`
(`server_options.go:30-31`): `polling/server.go:63-75` writes the headers when the
transport's `CheckOrigin` (or `polling.Default`'s) is set and accepts the request, and
`websocket/transport.go:83-88` hands `CheckOrigin` to gorilla's `Upgrader` inside `Accept`,
so the websocket same-origin check is gorilla's, applied there. `engineio.NewServer` carries
the option to them where it resolves `Options.Transports` (`getTransport`,
`server_options.go:82-90`): for
each of this module's two transports (`*polling.Transport`, `*websocket.Transport`, by type
switch) whose `CheckOrigin` is nil, it uses a copy of the transport with `CheckOrigin` set to
a function that accepts every origin when the default is on, and the transport as given when
it is off; `polling.Default` and `websocket.Default` are never mutated, and a transport of
another package is used as given. No file under `engineio/transport/` changes, so the
`transport.Transport` interface (`engineio/transport/transport.go`, a SYNC manifest file) is
untouched and V1-18 has no `sync-check` consequence. A
transport whose `CheckOrigin` is set keeps it, on or off: it is the `fn(origin, cb)` form of
`origins`, and the examples that set one (`_examples/default-http`, through `allowOriginFunc`
in `main.go`) keep working unchanged. `NewServer(nil)` gets the default. The `### Changed`
entry names CORS and the field. Named tests, under the `// Covers` of their rows:
`TestCORSDefault` (P8: the two header forms on polling responses of a session, a
cross-origin websocket upgrade accepted) and `TestCORSDisabled` (S4: with the field set, no
CORS header on any polling response, the cross-origin upgrade refused with HTTP 403, and a
transport's `CheckOrigin` still honoured either way). Entry: V1-2 merged (the `Options`
fields it adds; the transports are V1-1's, merged).

**Differential contract tests (V1-19; D17).** The conformance scenarios of V1-8 assert what
the plan expects; D17 asks for the reference itself as the oracle. `TestDifferential` in
`internal/conformance/differential_test.go` (tag `conformance`, the V1-8 layout and rules)
starts, per scenario, the Go server with the scenario's handlers and the reference server
`testdata/node/server/reference-server.js` (`socket.io@2.5.0` under the server prefix of V1-8)
with the same handlers written in JS, then runs `testdata/node/differential-client.js` with
the job's client version (the matrix entry: 1.7.4 and 2.5.0) against each server, once over
polling and once over websocket; the client prints a JSON transcript of what it observed,
and the test normalises both transcripts and compares them with `reflect.DeepEqual`,
printing the first difference. Compared exactly: the names and arguments of every event the
client receives and of every ack it gets back (JSON-decoded values, so number formatting
does not count), the order of events on one socket, the payload of an `error` event
(`Invalid namespace`, a middleware reason), the reason of each `disconnect` event, the
`connect` and `disconnect` sequence of each namespace socket, the HTTP status and JSON body
of a rejected handshake (P11, P12), the CORS headers of a polling response (P8), the
`pingInterval` and `pingTimeout` of the OPEN packet (E1, E2) and binary payloads (compared as
bytes). Normalised before the comparison: session ids and `socket.id` values (replaced by the
index of the connection in the transcript; `/ns#id` keeps its prefix, K1), `handshake.time`
and `issued` (present and parseable only), and the order of events across different sockets
(sorted by socket, then by sequence). Not compared: wire bytes (the Go encoder's trailing
`\n` of R2 is invisible to a client), timing, and HTTP headers other than the CORS ones.
Scenarios, the table in the test using these names: `connect-root` (connect and disconnect on
`/`, `socket.id` shape), `namespace` (connect to `/chat`, then to an unknown namespace: ERROR
with the root socket kept, S10), `middleware-reject` (a middleware error with its reason,
S11), `ack-both-ways` (K26, K27), `binary` (binary event and ack both ways, R4),
`non-ascii` (P16), `rooms` (join, `to(room)`, broadcast except the sender, leave; K11, K14,
S13), `server-disconnect` (`disconnect()` and `disconnect(true)` both seen as `io server
disconnect`, since the reference writes the DISCONNECT packet before the engine close and
the engine flushes it (`client.js:111-119`, engine `socket.js:462-470`); a connection the
server drops without a packet, `Server.Remove(sid)`, seen as `transport close`; K20, K24),
`handshake-data` (query and a header echoed back; K5, K7),
`rejected-handshake` (bad method, unknown sid, unknown transport by raw HTTP; P11, P12, P13),
`cors` (a polling GET with and without `Origin`; P8), `open-packet` (E1, E2). Twelve scenarios
over two transports: twenty-four subtests per matrix entry, named flat as
`<scenario>-<transport>` (one `t.Run` level, `TestDifferential/connect-root-polling`, no
scenario-level parent subtest), so `go test -json` reports one pass per pair and the count
of the command below is 24. A scenario whose feature the Go
server lacks fails, so V1-19 enters after the server PRs it exercises: V1-3 to V1-7 and
V1-18 (wave V1B) and V1-8 (the job, the installs, the layout). The job runs it as part of
`make conformance`, with no workflow change. Besides the per-PR DoD, on its head:

```sh
for s in connect-root namespace middleware-reject ack-both-ways binary non-ascii rooms server-disconnect handshake-data rejected-handshake cors open-packet; do grep -q "\"$s\"" internal/conformance/differential_test.go || { echo "no scenario $s" >&2; exit 1; }; done   # the twelve scenarios of the list exist by name
test "$(go test -tags conformance -list '^TestDifferential$' ./internal/conformance/... | grep -c '^TestDifferential$')" -eq 1   # -list needs no Node
test "$(go test -race -count=1 -tags conformance -json -run '^TestDifferential$' ./internal/conformance/... | jq -r 'select(.Action=="pass" and .Test!=null and (.Test|contains("/")))|.Test' | sort -u | wc -l)" -eq 24   # every scenario passes over both transports (needs Node and the installs, as the job has)
```

**API coverage gate (V1-20; D17).** Two gates, both `Makefile` targets of the root, run by
CI on every v1 change as a step of the `lint-v1` job (ubuntu, the newest Go), and listed in
the CLAUDE.md Commands. `make parity-check` runs `go test -count=1 -run '^TestParityCovers$'
./internal/parity`: a Go test (portable, so it also runs under `make test` and the `test-v1`
matrix) that reads `docs/PARITY.md` with the row rule of the DoD helper `rows` (`$2` matches
`^[A-Z][0-9]+$`, `$3` is the plan), walks every `*_test.go` of the root module (directories
holding another `go.mod`, `_examples/` and `v2/`, are skipped), collects each `// Covers <ID>`
line whose ID has the row shape and the `func Test...` declaration that follows it within
four lines (the `covers` rule), and fails naming each row that has a `DONE` entry and no
such marker, and each marker whose ID is no row. With `PARITY_STRICT=1` it also demands a
marker for a row with an `ADD`, `CHG`, `BUG` or `TEST` entry: the MV1 DoD runs it so, when
every such row is `DONE` anyway; CI runs it without, because an open row has no test yet.
The Stage 1 and Stage 7 markers (`// Covers 1B-T1 (S, C)`) do not have the row shape and are
ignored. `make cover-check` runs `go test -count=1 -coverpkg=./... -coverprofile=<tmp> ./...`
(cross-package, since a root test that exercises `engineio` counts for its API) and reads
`go tool cover -func=<tmp>`: the `total:` percentage must be at least `COVER_FLOOR`, and
the number of exported functions and methods (name starting with an upper-case letter, in
a non-test file of the module) reported at `0.0%` must be at most `API_UNCOVERED_MAX`,
printing those functions; both are variables of the `Makefile`. Baseline, measured at
`0d9e594` (the base of this plan) by `go test -count=1 -coverpkg=./... -coverprofile=c.out
./... && go tool cover -func=c.out`, its `total:` line and `go tool cover -func=c.out | awk
'$3=="0.0%" && $2 ~ /^[A-Z]/'` (the rule V1-20 implements; `cover -func` lists no test
file, so the non-test condition is implicit): 88.2 % of statements (88.1 % on a second run
of the same tree, since a timing-dependent test moves the total by a tenth; one more reason
the floor is measured on V1-20's head and only kept from falling) and 24
exported functions at 0.0 % (the 7 exported test fakes of `engineio/packet/fake_*.go`,
`engineio.Server.Addr`, three methods of the engine client in `engineio/client.go`, the 9
delegating methods of `namespace_handler.go:44-53`, `polling/server.go:41 URL`,
`errors.go:43 Error` and two `logger` methods). V1-20 sets the two variables to the values it
measures on its own head, which is why no number is fixed here, and its body quotes them.
Ratchet: a later PR may raise `COVER_FLOOR` and lower `API_UNCOVERED_MAX`, never the
reverse, which the per-PR DoD checks against `origin/master`; MV1 requires
`API_UNCOVERED_MAX` 0, the measurable form of D17's "coverage of the API": an exported
function no test executes is the gap, and the 24 above are all reachable without an
external service (a function that is not would be named in the `Makefile` with the reason
and excluded; none at `0d9e594`). The floor is only kept from falling; D17 names no
percentage, so none is set. Entry: V1-8 merged (the same files, `.github/` and the
`Makefile`, and the `-list` rule for tagged tests that `TestParityCovers` shares with
`covers`). Besides the per-PR DoD, on its head:

```sh
make parity-check   # passes on the head: every DONE row has its marker
printf '| Z9 | DONE V1-1 | D1 | probe |\n' >>docs/PARITY.md; if make parity-check; then exit 1; fi; git checkout -- docs/PARITY.md   # a DONE row without a marker fails it
grep -qE '^COVER_FLOOR *[:?]?= *[0-9]+(\.[0-9]+)?$' Makefile; grep -qE '^API_UNCOVERED_MAX *[:?]?= *[0-9]+$' Makefile; make cover-check   # the two ratchets exist and hold on the head
grep -q 'make parity-check' CLAUDE.md; grep -q 'make cover-check' CLAUDE.md; grep -q 'make parity-check cover-check' .github/workflows/ci.yaml   # the Commands lines and the lint-v1 step
```

After V1-20 has merged, the per-PR DoD of every later PR adds one line for the ratchet, run
before `make lint test-race`. V1-20 itself does not run it: on its head `origin/master` has
no `COVER_FLOOR` or `API_UNCOVERED_MAX` yet, so the line would fail there, and its body
quotes the measured values instead, as above:

```sh
v() { git show "$2:Makefile" | awk -F'[:?]?= *' -v k="$1" '$1 ~ "^" k " *$" {print $2}'; }; test "$(echo "$(v COVER_FLOOR HEAD) >= $(v COVER_FLOOR origin/master)" | bc)" -eq 1; test "$(v API_UNCOVERED_MAX HEAD)" -le "$(v API_UNCOVERED_MAX origin/master)"   # a PR never lowers the floor or raises the ceiling
```

Waves V1A to V1F are in *Execution and parallel work*.

Entry of a wave, `bash` and `set -e`, after the DoD helpers `rows` and `covers` below are defined
(wave V1A needs one `### V1-<n> contract` heading per PR that adds exported API, the `ADD`
PRs and V1-4 with its close error, written by V1-0). Each gate is a
separate command, and `wave` exits on the first open PR: in a `&&` list a failing non-final
command does not trigger `set -e`, so predecessors are never chained with `&&`.

```sh
done_pr() { test -z "$(rows | awk -F' *[|] *' -v pr="$1" '$3 ~ ("(^|; )(BUG|ADD|CHG|TEST) " pr "( |;|$)")')" || return 1; test -z "$(covers $1)"; }
wave() { for n in "$@"; do done_pr V1-$n || { echo "open: V1-$n" >&2; exit 1; }; done; }
for n in 2 4 5 6 7 10 11 15 16 17 18; do grep -q "^### V1-$n contract" docs/ROADMAP.md || { echo "V1A: no contract V1-$n" >&2; exit 1; }; done
wave 1 2                                                       # entry of V1B
wave 3 4 5 6 7 16 17 18                                        # entry of V1C
wave 8; grep -q '^  conformance:' .github/workflows/ci.yaml    # entry of V1D, with the next line
grep -q '^parity-check:' Makefile; grep -q '^cover-check:' Makefile   # V1-20 has no row: its targets
wave 10 11; test -f client/client.go                           # entry of V1E, with the next line (V1-12 needs only V1-8; V1-15 needs V1-2, V1-6, V1-11)
test -f internal/conformance/differential_test.go              # V1-19 has no row: its file
wave 15                                                        # entry of V1F, with the next line
test -f _examples/ack/go.mod -a -f _examples/binary/go.mod -a -f _examples/namespaces/go.mod -a -f _examples/middleware/go.mod   # entry of V1F
```

Each PR runs the DoD below with `N` set to its number. `bash` and `set -e`, rules as in the
Stage 1b DoD; the awk reads the pipe-delimited rows of PARITY.md (`$2` is the ID, `$3` the plan).

```sh
rows() { awk -F' *[|] *' '$2 ~ /^[A-Z][0-9]+$/' docs/PARITY.md; }
test "$(go list -m)" = github.com/sshaplygin/go-socket.io; test -f v2/go.mod   # entry: the restructure has merged
test "$(rows | wc -l)" -eq 137   # 133 audit rows and B1 to B4: none lost, none added silently
test -z "$(rows | awk -F' *[|] *' '{n=split($3,e,/; */); for(i=1;i<=n;i++) if (e[i] !~ /^(-|DEV|REDIS|(DONE|DROP|BUG|ADD|CHG|TEST) V1-[0-9]+)$/) print $2}')"   # every entry of every plan is well formed; no decision is pending, so no other kind exists
N=${N:?the PR, e.g. V1-4}
make lint test-race
covers() { all=$(go test -tags conformance ./... -list . 2>/dev/null); for id in $(rows | awk -F' *[|] *' -v pr="$1" '$3 ~ ("(BUG|ADD|CHG|TEST|DONE|DROP) " pr "( |;|$)") {print $2}'); do t=$(git grep -h -A4 -E "^// Covers $id( |$)" -- '*_test.go' | awk '/^func Test/{sub(/[(].*/,"",$2); print $2; exit}'); test -n "$t" && printf '%s\n' "$all" | grep -qx "$t" || echo $id; done; }
test -z "$(covers $N)"   # every row the PR names has a `// Covers <ID>` above a test function that `go test -tags conformance -list` finds
for s in V1-6:TestVolatileEmit:K16 V1-7:TestConnRequest:K5; do test "${s%%:*}" = "$N" || continue; p=${s#*:}; t=${p%%:*}; go test -tags conformance ./... -list "^$t\$" 2>/dev/null | grep -qx "$t" || { echo "no test $t" >&2; exit 1; }; git grep -h -B4 -E "^func $t[(]" -- '*_test.go' | grep -qE "^// Covers ${p#*:}( |$)" || { echo "no // Covers ${p#*:} above $t" >&2; exit 1; }; done   # rows whose marker an earlier PR wrote first (K5 by V1-2's TestSessionRequest, K16 by V1-4's TestSessionWritable), which covers alone would accept
test -z "$(rows | awk -F' *[|] *' -v pr="$N" '$3 ~ ("(^|; )(BUG|ADD|CHG|TEST) " pr "( |;|$)")')"   # the PR rewrote its rows to DONE, in any entry of the plan
b=$(git show origin/master:docs/PARITY.md | awk -F' *[|] *' -v pr="$N" '$3 ~ ("(^|; )BUG " pr "( |;|$)")' | wc -l)
test "$(gh pr view --json body -q .body | grep -c '^fails without the fix: Test')" -eq "$b"   # one line per BUG row of the base
```

Checks specific to a PR, in addition: every PR that owns a `BUG` row (V1-1 to V1-5 as planned,
and any later one) puts one body line `fails without the fix: <TestName>` per `BUG` row, the
test being the one under that row's `// Covers`, and has run it red on the base with only the
PR's test files applied. The DoD block counts the lines against the `BUG` rows of the base. `Server.Close` has two layers and two tests: V1-2 (E22)
owns the `engineio` one (open sessions are closed), V1-5 (S17) the root one (`Close` returns after
the sessions are gone). V1-2, on its head, counts `TestPingTimeoutIdleClient` (E1, under
`// Covers E1`; in package `session`, which reads the cause the session records): with
`PingInterval` 100 ms and `PingTimeout` 50 ms (small values keep the test fast; the default
values are asserted beside it), a client that sends only PING at the interval is still open
after 500 ms, ten times `PingTimeout`, and a client that sends nothing is closed with reason
`ping timeout` within 1 s. The session fakes of that package cannot carry it: `scriptConn`
and its relatives answer `SetReadDeadline` with nil and never time out, so a session over
them is closed by nothing. The test drives the session over a real connection (a
`net.Pipe` or loopback websocket `transport.Conn`, whose read deadline fires) or over a
new fake whose `SetReadDeadline` arms a timer that fails the next `NextReader` once it
passes; either way the close comes from the deadline, not from the script running out.
It is red on the base, where the pinging client is closed 50 ms after its first PING, and
the V1-2 body proves it with the `BUG`-row rule (`fails without the fix:
TestPingTimeoutIdleClient`, run on the base with only the PR's test files applied):

```sh
test "$(go test -race -count=1 -json -run '^TestPingTimeoutIdleClient$' ./engineio/... | jq -r 'select(.Action=="pass" and .Test!=null and (.Test|contains("/")|not))|.Test' | sort -u | wc -l)" -eq 1   # the reference close window: an idle client pinging at the interval stays, a silent one is closed
```

V1-8 and later run `gh pr checks <n>` with `conformance` green; V1-10, V1-11 and V1-15 run the Go client against the
Node 2.5.0 server of the conformance job. V1-12 and V1-13 run `make examples` and then
`examples-node` once per gate client on their head (V1-12 with `ack` and `binary` present;
V1-13 and MV1 with all four new directories), the two lines the MV1 DoD repeats:

```sh
make examples   # builds and vets every example module, cmp of the chat copies
for V in 1.7.4 2.5.0; do d=$(mktemp -d); npm install --no-save --prefix "$d" socket.io-client@$V; CONFORMANCE_NODE_MODULES="$d/node_modules" make examples-node; done   # each client.js under both gate clients, the install line of V1-8 (D17)
```

The `cmp` step of `examples` covers every `CHAT_COPIES` file, so a copy that
V1-13 leaves behind fails there. V1-6, on its head, counts the four Redis-less tests of the
Redis `Broadcast` named in *`Except` with the Redis adapter*:

```sh
V6='TestRedisBroadcastDropsUnknownOpts|TestRedisBroadcastDropsUnknownKeys|TestRedisBroadcastLocalFlag|TestRedisBroadcastVolatileFlag'
test "$(go test -race -count=1 -json -run "^($V6)\$" . | jq -r 'select(.Action=="pass" and .Test!=null and (.Test|contains("/")|not))|.Test' | sort -u | wc -l)" -eq 4   # the decoder guard, the unknown-key guard and the two flags, without a Redis server
```

**MV1 DoD** (V1-14 head, then again on the merged `master`):

```sh
rows() { awk -F' *[|] *' '$2 ~ /^[A-Z][0-9]+$/' docs/PARITY.md; }
make lint test-race examples   # builds and vets all example modules, cmp of the chat copies
for V in 1.7.4 2.5.0; do d=$(mktemp -d); npm install --no-save --prefix "$d" socket.io-client@$V; CONFORMANCE_NODE_MODULES="$d/node_modules" make examples-node; done   # each client.js under both gate clients (D17), as the per-PR checks of V1-12 and V1-13
test -z "$(rows | awk -F' *[|] *' '$3 ~ /(BUG|ADD|CHG|TEST)/')"   # no open row, in any entry of the plan (a row with several entries is open while one of them is)
for V in 1.7.4 2.5.0; do grep -q "socket.io-client@$V" .github/workflows/ci.yaml || { echo "missing client $V" >&2; exit 1; }; done
cl() { awk -v want="$1" 'function out() { if (e != "") print e; e = "" } /^## Unreleased/ {u = 1; next} /^## / {out(); u = 0} u && /^### / {out(); s = $2; next} u && s == want { if (/^- /) { out(); e = $0 } else if (/^  +[^ ]/ && e != "") e = e " " $0; else out() } END {out()}' CHANGELOG.md; }   # one line per entry of that subsection of Unreleased, continuation lines joined
test -n "$(cl Removed | grep -i jsonp)"; test -n "$(cl Added | grep 'go-socket.io/client')"
test -n "$(cl Changed | grep -w Conn | grep -w Namespace | grep -w Broadcast | grep -F 'engineio.Conn' | grep -F 'Join(rooms ...string)')"   # D10 and D16: one entry names the four interfaces and the Join signature change of V1-6
test -n "$(cl Changed | grep -w cookie)"; test -n "$(cl Changed | grep -w httpCompression)"   # the V1-15 default changes
test -n "$(cl Changed | grep -w PingInterval | grep -w PingTimeout)"; test -n "$(cl Changed | grep -iw cors)"; test -n "$(cl Changed | grep -w ID | grep -w namespace)"   # the D15 default changes (V1-2, V1-18, V1-5)
test -n "$(cl Changed | grep -w OnConnect | grep -w ERROR)"   # V1-5's connect rejection keeps the connection open
test -n "$(cl Changed | grep -w OnDisconnect | grep -F 'transport close')"; test -n "$(cl Changed | grep -w Close | grep -w DISCONNECT)"   # V1-4's disconnect reasons; V1-5's Conn.Close sends DISCONNECT per namespace
grep -q '^### Deviation D4: JSONP unsupported' docs/PROTOCOL.md; grep -q '^### Deviation D3: mixed Go/Node Redis cluster' docs/PROTOCOL.md   # the V1-14 headings; master's PROTOCOL.md already mentions JSONP
for d in ack binary namespaces middleware; do test -f _examples/$d/go.mod; done
test -f _examples/default-http/client.js; test -f _examples/asset/index-1.7.html; grep -q 'socket.io-client@1.7.4' _examples/asset/index-1.7.html   # D17: the chat's client script and the 1.7 page (V1-12, V1-13)
grep -q CONFORMANCE_NODE_MODULES Makefile; grep -q examples-node .github/workflows/ci.yaml   # examples-node takes the job's client, so it ran under 1.7.4 and 2.5.0
PARITY_STRICT=1 make parity-check; make cover-check; grep -qE '^API_UNCOVERED_MAX *[:?]?= *0$' Makefile   # D17 gates (V1-20): every row has a test, no exported function without one
test "$(go test -tags conformance -list '^TestDifferential$' ./internal/conformance/... | grep -c '^TestDifferential$')" -eq 1   # the differential scenarios (V1-19) are in the job
test -z "$(git tag -l 'v1.5*')"   # release preparation creates no tag
```

MV1 Acceptance, by the owner: the `conformance` job, V1-15's compression scenario and V1-19's
differential scenarios included, is
green on `master` for both client versions; `_examples/default-http` works unchanged against `socket.io-client` 2.x (Stage 1
Acceptance), and its `client.js` and the two chat pages (`index.html`, `index-1.7.html`) run
against 1.7.4 and 2.5.0 (D17); the four new examples run against a Node client of either version as their READMEs say; the Go
client connects to a Node `socket.io` 2.5.0 server over polling and websocket and reconnects
after the server restarts. The owner then declares the v1 line complete (*Branch and tag
policy*) and orders the release commit and the `v1.5.0` tag; v2 work (Stage 2 continuation)
resumes on the owner's order after MV1, not on the tag.

**Stage V1R. Redis parity (next v1 minor, after MV1).** Scope, from D2: `Except` and
broadcast-except-sender over Redis (A4), the `volatile` (A4) and `compress` (S21) flags across instances, a room union as one
message and id lists across instances (S13, S15, A5), the `DB` option (A10), binary arguments across
instances (B3), the 5 s `RoomLen`/`Rooms` waits (B4, 1.K), and `_examples/redis-adapter` turned
into a cluster-correct two-instance chat that returns to the identical `chat.go` (`CHAT_SKIP` emptied); the mixed Go/Node cluster stays out (D3, A8, A9).
New message entries follow the forward rule of *`Except` with the Redis adapter* (every new
field inside `opts`, an entry only for a non-default value, a marker when the `args` encoding
changes); the changelog names every broadcast kind whose message carries an entry and which
`v1.5.0` instances therefore miss during a rolling upgrade: `Except`, `volatile`, `compress`
false, a room union, an id list and, if B3 adds its marker, binary arguments.
Entry: MV1 accepted. The PR split is recorded here when the stage starts. DoD: `make lint
test-race` with the two-instance tests under `-race`; no `REDIS` entry left in the `Plan`
column (`rows | grep REDIS` prints nothing); the changelog `### Known limitations` of 1.K is
updated; tag `v1.6.0` when `v1.5.0` is the latest tag, only on the owner's order.

**Why Stage 7 moves here.** D6 puts the `client` package before the tag, and the extraction
(V1-9) must follow the server work because the `Conn` and `Namespace` method sets that V1-5 to
V1-7 add are what `client` takes over at `$TIP`. Stage 7 keeps its contract, DoD and
Acceptance where they are (one owner, no copy); only its entry, position and release change
(see there). The finishing features (V1-10, V1-11) are additive changes of `client/` made after
that DoD, so Stage 7 stays a behaviour-preserving extraction that can be reviewed alone.

**Shared-file drift check (PR SYNC, D14; outside the V1 sequence).** One PR adds the root
`Makefile` target `sync-check`, the manifest `.github/sync-manifest` (one line
`<root path> <v2/ path>` per shared file, `#` comments), a `sync-check` job in
`.github/workflows/ci.yaml` that runs on every push and pull request with no path filter (a
change on either side can break it), and the `make sync-check` line of the CLAUDE.md
Commands. The target rewrites `github.com/sshaplygin/go-socket.io/v2` to
`github.com/sshaplygin/go-socket.io` in each `v2/` copy and compares it with `cmp` to the root
file. The manifest holds the D14 list as measured at `18af32b`: 13 non-test files, 724 lines
(`logger/logger.go`; `engineio/frame/frame.go`; `engineio/transport/transport.go`,
`manager.go` and `errors.go`; `engineio/session/session_id_generator.go` and
`session_manager.go`, which `v2/` names `id_generator.go` and `manager.go`;
`engineio/packet/decoder.go`, `encoder.go`, `packet.go` and `types.go`;
`engineio/transport/utils/clock.go`; `engineio/payload/pauser.go`), plus
`engineio/internal/shutdown.go` (8 lines), identical too; the test fakes of `engineio/packet`
differ and stay out. From SYNC on, a PR that changes a manifest file makes the same change in
its copy (D13) or removes the pair, with a body line `sync-check left out: <path>: <reason>`.
The V1 PRs expected to change one are V1-2 (E15: the random default id in
`session_id_generator.go`, also wanted by v2, whose engine.io reference uses `base64id` too)
and V1-16 (`Manager.All` in `session_manager.go`, `### V1-16 contract`, mirrored into
`v2/engineio/session/manager.go`); each mirrors the change into the `v2/` copy whether it
merges before or after SYNC, so the pair stays, and so does any other V1 PR that edits a
listed file without diverging it on purpose.
A removed pair returns to the manifest in the PR that makes the two files identical again.
Entry: row R step C passed; wave SY of *Execution and parallel work*. DoD, `bash` and `set -e`:

```sh
test "$(go list -m)" = github.com/sshaplygin/go-socket.io; test -f v2/go.mod   # entry: the restructure has merged
m=.github/sync-manifest
awk '!/^#/ && NF {print $1; print $2}' "$m" | xargs ls >/dev/null   # every listed path exists
test "$(awk '!/^#/ && NF' "$m" | wc -l)" -eq "$((14 - $(gh pr view --json body -q .body | grep -c '^sync-check left out: ')))"   # the 14 files, less each one a V1 PR diverged before SYNC, named in the body with its reason
make sync-check   # every pair is identical after the import-path rewrite
f=$(awk '!/^#/ && NF {print $2; exit}' "$m"); echo '// drift' >>"$f"; if make sync-check; then exit 1; fi; git checkout -- "$f"   # a changed v2/ copy fails the check
grep -q '^  sync-check:' .github/workflows/ci.yaml; grep -q 'make sync-check' CLAUDE.md
```

`gh pr checks` of the SYNC PR shows the `sync-check` job green; the owner makes it a required
check.

**V1-0 contracts.** The subsections below are the public-API record of Stage V1, the V1-0
row: one `### V1-<n> contract` per PR that adds exported API (the `ADD` PRs 2, 5, 6, 7, 10,
11, 15, 16, 17 and 18) and `### V1-4 contract` for the close error type of
`engineio/session` and the writability report that V1-6's `Volatile` reads. They were written against the code at `2667c25` (file and line
references are to that tree) and the installed reference sources (engine.io 3.6.2,
socket.io 2.5.0, socket.io-adapter 1.1.2, socket.io-client 2.5.0, engine.io-client 3.5.6).
Each contract lists every `ADD` row of its PR, the Go declarations (every ```` ```go ````
fence is a complete file that `gofmt -e` accepts; the comments are the godoc the PR
writes), the semantics, defaults, errors, the goroutine and ordering rules, and the named
tests, each under the `// Covers <ID>` marker of its row: one marker line per row, and a
test that closes several rows stacks their markers directly above its `func`, within the
four lines `covers` and V1-20 read. API shape follows D9 (idiomatic Go, the reference's
shape is not copied: a Go zero value is the reference default, so a boolean option whose
reference default is `true` is named for its `false` meaning, `Disable...`) and D10
(methods directly on the exported interfaces): a method with a namespace meaning goes on
`Namespace`, the per-connection view embedded in `Conn`, and a connection-level one on
`Conn`; the `Broadcast` and `engineio.Conn` additions follow the same rule. The contracts
bind the PRs; a PR that needs to deviate edits its contract first (CLAUDE.md: public API
goes through the roadmap), and the three-agent validation of CLAUDE.md reviews each such
edit. The implementers of the four interfaces in this repository at `2667c25`, which every
addition below must update in the same PR: `Conn` and `Namespace`: `*namespaceConn`
(`namespace_conn.go`), and from V1-9 on the `client` copy of it (Stage 7, *D10 methods
and the alias*); `Broadcast`: `*broadcast` (`broadcast.go`), `*redisBroadcast`
(`redis_broadcast.go`), `nopBroadcast` (`namespace_handler.go`); `engineio.Conn`:
`*session.Session` (`engineio/session/session.go`), the engine client `*client`
(`engineio/client.go`, returned by `Dialer.Dial` and wrapped by the root `Client` at
`client.go:92`), the root test fake `*fakeConn` (`backpressure_test.go:27`, which embeds
a nil `engineio.Conn` and so compiles unchanged but panics when the library calls a new
method on it: V1-16 gives it real methods) and `faultConn` (`log_test.go`, embedding
`*fakeConn`); the `transport.Conn` fakes of the `engineio` and `session` tests
(`scriptConn`, `closingConn`, `heldConn`, `pausableConn`, `failingConn`, `blockingConn`,
`brokenConn`) implement `transport.Conn`, which no contract changes (it is a SYNC manifest
file), so they are untouched. Two root test types stand in for `Conn` the way `*fakeConn`
does for `engineio.Conn`: `redisTestConn` (`redis_broadcast_test.go:30`) and `emitConn`
(`backpressure_test.go:774`) embed a nil `Conn`, define only `ID` and `Emit`, and are
passed to `Broadcast.Join` and `Send`; they compile unchanged after every D10 addition and
panic when the library calls any other method on them, so a PR whose broadcast code calls
a new `Conn` method on its recipients (V1-6's `SendTo` of the memory and Redis types, if it
reads more than `ID` and `Emit`) gives them that method in the same PR. No test mocks `Namespace` or
`Broadcast` today (V1-6 adds the first `Broadcast` fake, for `ErrExceptUnsupported`).
The check that every `ADD` row of PARITY.md is named in the contract of its PR, `bash`:

```sh
rows() { awk -F' *[|] *' '$2 ~ /^[A-Z][0-9]+$/' docs/PARITY.md; }
sec() { awk -v h="### V1-$1 contract" '$0 == h {p=1; next} /^## |^### / {p=0} p' docs/ROADMAP.md; }   # the text of one contract
for pr in 2 5 6 7 10 11 15 16 17 18; do for id in $(rows | awk -F' *[|] *' -v pr="V1-$pr" '$3 ~ ("(^|; )ADD " pr "( |;|$)") {print $2}'); do sec $pr | grep -qw "$id" || echo "V1-$pr: $id"; done; done   # empty output: every ADD row is in the contract of its PR
```

### V1-2 contract

Rows: E3 (`upgradeTimeout`), E4 (`maxHttpBufferSize`; its `BUG` entry is the missing cap,
its `ADD` entry the field) and E7 (`allowUpgrades`). The other V1-2 rows (E1, E2, E5, E14,
E15, E22, P3, P11, P12) change behaviour or defaults without new API and are specified in
the V1-2 row of the PR table. Three fields on `engineio.Options`
(`engineio/server_options.go:16-45`), read by `NewServer` through getters like the
existing ones, so `NewServer(nil)` keeps working:

```go
package engineio

import (
	"net/http"
	"time"
)

// Options is the server configuration. The existing fields stay; V1-2 adds these three.
type Options struct {
	// UpgradeTimeout bounds an upgrade, from the moment the probe transport's request is
	// accepted (Session.Upgrade) until its UPGRADE packet has been read. When it passes
	// the probe connection is closed and the session goes on over its current transport.
	// 0 or a negative value means 10 seconds, the engine.io 3.6.2 upgradeTimeout.
	UpgradeTimeout time.Duration

	// MaxHTTPBufferSize is the largest message accepted from a client, in bytes: the body
	// of a polling POST and one websocket message (the inflated size when
	// permessage-deflate is negotiated, see PerMessageDeflate). 0 or a negative value
	// means 1e6 (1 000 000), the engine.io 3.6.2 maxHttpBufferSize. There is no unlimited
	// setting; math.MaxInt64 is the practical one.
	MaxHTTPBufferSize int64

	// DisableUpgrades keeps every session on the transport of its handshake: the OPEN
	// packet advertises no upgrade and a request that would start one is rejected.
	// false, the zero value, is the engine.io 3.6.2 default allowUpgrades: true.
	DisableUpgrades bool
}

// ConnInitorFunc exists (types.go:11); V1-2 keeps its signature and the goroutine it
// runs on and fixes its order: it is called once per created session, on the
// handshake request's goroutine with the request live (its Context not done, as
// today), after the session is registered and before the OPEN packet is written, so
// before Accept returns the session for both transports. A session whose OPEN packet
// then cannot be written was initialised but is not accepted. Today a polling
// handshake already has this order (the OPEN write waits for the request's FlushOut,
// which follows the initor), but after a websocket handshake the initor runs on the
// request goroutine (server.go:153) concurrently with the OPEN write and with Accept.
type ConnInitorFunc func(*http.Request, Conn)
```

Semantics. The initor: `Server.ServeHTTP` calls `s.connInitor(r, reqSession)` at
`server.go:153` after `newSession` returned, while the goroutine `newSession` started at
`:213-229` runs `InitSession` and sends the session to `connChan`. For a polling
handshake that goroutine blocks in `InitSession` until the request's
`reqSession.ServeHTTP` reaches `FlushOut` (`engineio/payload/payload.go:154-192,374-400`),
which follows the initor, so the order already holds; for a websocket handshake nothing
waits for the request goroutine, and `Accept` and the root's `serveConn` can run before
the initor did. V1-2 passes `r` to `newSession` and calls the initor there, on the
request goroutine, after `s.sessions.Add` and before the `go` statement that starts the
`InitSession` goroutine; the `go` statement happens before that goroutine's first step and
the `connChan` send happens before the receive in `Accept`, so everything the initor did
is visible to the code that receives the session, for both transports, while `r` and its
Context stay live during the call as today. The ServeHTTP call site is removed; no
`### Changed` entry is needed, since a polling handshake keeps its order and a websocket
one gains the polling one. The order is what today's K5 workaround needs (PARITY.md K5:
an initor that calls `SetContext`, which the root copies when it takes the session,
`connection.go:219-221`): with the old order the copy races the initor after a
websocket-first handshake. K5's request itself is kept by the session (`SetRequest` in
the plumbing fences below), not by the initor, so `Conn.Request()` of V1-7 does not
depend on this order. E3: today `session.upgrading` (`engineio/session/session.go:412-595`) waits
`PingTimeout` for the probe PING (`:414`) and without bound for the UPGRADE packet
(`:548`). With the field, the whole sequence (probe PING read, PONG written, UPGRADE read)
has `UpgradeTimeout` from `Session.Upgrade`; when it passes, the probe connection is
closed, the paused polling transport resumes and the session stays open on it (3.6.2
`socket.js:189-195`: `cleanup(); transport.close()`); the timer is stopped when the
upgrade completes or fails for another reason. E4: a polling POST whose body is longer
than the limit is read no further (`http.MaxBytesReader` on `r.Body`), answered HTTP 413
(`http.StatusRequestEntityTooLarge`, plain text), and the session is closed with cause
`transport error` (V1-4's close error); 3.6.2 destroys the connection without a response
(`polling.js:164-167`) and leaves the session to its ping timeout, which a reference
client cannot distinguish (D9): its POST fails either way, it closes and reconnects with a
new sid. A body of exactly the limit is accepted. A websocket message longer than the
limit closes the connection with close code 1009 (`message too big`, what `ws` 7.5.10 does
for `maxPayload`), through gorilla's `SetReadLimit` on the plain connection; with
compression the limit counts inflated bytes, the rule and test of `### V1-15 contract`.
The limit is checked by the transport connection, which the session configures through
the setter of the plumbing fences below (`session.New` keeps its signature). E7: with `DisableUpgrades` the OPEN packet's `upgrades` is `[]`
(`transport.ConnParameters.Upgrades` empty, as `Server.upgrades()` returns `[]` when
`allowUpgrades` is false, 3.6.2 `server.js:122`) and a request with a live sid on another
transport is answered HTTP 400 `{"code":3,"message":"Bad request"}`, the P11 reply form of
P13's "transport changed without upgrade" case; 3.6.2 would still run `maybeUpgrade` for a
client that probes unasked (`server.js:351-420`, `verify` skips the transport check for an
upgrade), a difference no reference client observes, since it probes only what the OPEN
packet advertises. Named tests, each under the `// Covers` of its row:
`TestUpgradeTimeout` (E3: with `UpgradeTimeout` 100 ms, a websocket probe that sends
`2probe`, reads `3probe` and never sends `5` is closed by the server within 1 s while the
session still answers polls; `Options{}` gives 10 s), `TestMaxHTTPBufferSize` (E4,
subtests: `polling`, a POST of limit+1 bytes is answered 413 and the session closes with
`transport error`, one of exactly the limit is accepted; `websocket`, a message of limit+1
bytes is answered with close code 1009; `default`, `Options{}` gives 1e6; it is the test
of the `fails without the fix:` body line of E4's `BUG` entry, red on the base where the
POST of limit+1 bytes is accepted) and `TestDisableUpgrades` (E7: the OPEN packet carries
`"upgrades":[]` and a websocket request with the session's sid is answered 400 with code
3; with the field false the OPEN packet lists `websocket` and the upgrade succeeds) and
`TestConnInitorBeforeAccept` (package `engineio`, no row of its own, so no marker:
under `-race`, an initor that records the request's URL on the session with
`SetContext`, and that `r.Context().Err()` is nil during the call, is observed by the
goroutine that receives the session from `Accept`, for a polling and a websocket
handshake, 50 sessions each; the websocket subtest is red on the base, where the race
detector reports the initor's write against the receiver's read, or the record is
missing; the polling subtest is green on the base and guards that the order and the live
Context are kept; a third subtest, `rejected`, closes the session inside the initor on
20 handshakes of each transport and asserts that `Accept` receives none of them and that
`Count()` is 0 afterwards, the "initialised but not accepted" case of the fence comment,
which leaves nothing behind since the session alone holds its request) and
`TestSessionRequest` (package `engineio`, under `// Covers K5`, the row staying `ADD V1-7`
until V1-7 closes it, and the per-PR DoD still requires V1-7's `TestConnRequest` under
its own marker: `Request` of a session made by `session.New` is nil until `SetRequest`;
the session `Accept` returns from a running `Server` has the handshake request's method,
URL and a header for both transports, and after a websocket upgrade it still has the
polling GET's).

Plumbing of E3, E4 and K5: `session` cannot import `engineio`, and `session.New`, an
exported function of a public package, keeps its signature, so the values reach the
session through setters called as V1-17's `SetHooks` is, and the transport connections
through an optional interface, the pattern of V1-15's `SetSessionID`. The handshake
request is kept by the session, not by the root (`### V1-7 contract`, K5): it lives as
long as the session and goes with it, whether the session was accepted or its OPEN write
failed (`engineio/server.go:213-217` removes it), so nothing outside the session holds a
request:

```go
package session

import (
	"net/http"
	"time"
)

// SetLimits sets the largest message, in bytes, that the session's transport
// connections accept, and the bound of an upgrade (Options.MaxHTTPBufferSize and
// Options.UpgradeTimeout after their defaults). engineio.Server.newSession calls it
// after New and before InitSession, on the request goroutine. A session on which it is
// never called uses 1e6 bytes and 10 seconds. The session passes maxMessageSize to its
// handshake connection here and to an upgrade's probe connection in Upgrade, each
// through the optional interface interface{ SetReadLimit(int64) } when implemented.
func (s *Session) SetLimits(maxMessageSize int64, upgradeTimeout time.Duration) {}

// SetRequest keeps r, the handshake request (the polling GET or the websocket upgrade),
// for Request. engineio.Server.newSession calls it after New, before the ConnInitor and
// before InitSession, on the request goroutine.
func (s *Session) SetRequest(r *http.Request) {}

// Request returns the handshake request SetRequest kept, nil before it was called. Its
// Body is consumed and its Context is done once the handshake response was written, so
// it serves for the URL, headers, TLS state and remote address only. The socket.io
// server reads it through the optional interface interface{ Request() *http.Request }
// on engineio.Conn (K5, V1-7).
func (s *Session) Request() *http.Request { return nil }
```

```go
package polling

// SetReadLimit caps the body of one POST at n bytes: a longer body is read no further
// (http.MaxBytesReader) and is answered 413, and the session closes with cause
// transport error.
func (c *serverConn) SetReadLimit(n int64) {}
```

```go
package websocket

// SetReadLimit caps one message at n bytes: a longer one closes the connection with
// close code 1009 (gorilla's SetReadLimit on the plain connection; with
// permessage-deflate the inflated-size rule of the V1-15 contract applies).
func (c *conn) SetReadLimit(n int64) {}
```

### V1-4 contract

V1-4 has no `ADD` row; it makes two exported additions in `engineio/`, being the one
`engineio/` editor among V1-3 to V1-7 (wave V1B): the writability report that V1-6's
`Volatile` (K16, `### V1-6 contract`) reads, at the end of this contract, and the close
error type of `engineio/session` that K24 (`CHG V1-4`) needs: the engine's close cause, recorded by
`Session.observe` (`engineio/session/session.go:133-136`) into the unexported
`closeCause` and only logged today (`:122-129`), reaches the root, which today reports
`client namespace disconnect` for every connection close (`connection.go:170`,
`types.go:15`). The type lives in `session` (the package that observes the cause and that
`engineio` imports) and `engineio` re-exports it by alias, so a root caller that holds an
`engineio.Conn` names `engineio.CloseError` without importing `session`:

```go
package session

// CloseReason is why a session closed: the first cause the session observed.
type CloseReason string

// The close reasons, the strings of the "engineio: session close" log record and of
// engine.io 3.6.2 (socket.js onClose).
const (
	// ReasonTransportClose: the client sent a CLOSE packet.
	ReasonTransportClose CloseReason = "transport close"
	// ReasonPingTimeout: no packet arrived within PingInterval + PingTimeout (V1-2).
	ReasonPingTimeout CloseReason = "ping timeout"
	// ReasonTransportError: a transport read or write failed before that deadline.
	ReasonTransportError CloseReason = "transport error"
	// ReasonForcedClose: Session.Close was called (Conn.Close, Server.Remove).
	ReasonForcedClose CloseReason = "forced close"
	// ReasonServerShutdown: engineio.Server.Close closed the session (E22).
	ReasonServerShutdown CloseReason = "server shutting down"
)

// CloseError is the error NextReader and NextWriter return, wrapped, from the moment
// the session has closed: every call after the close returns one, and the first call
// that observes the close returns it in place of the bare transport error.
type CloseError struct {
	Reason CloseReason
	// Err is the transport error for ReasonTransportError and io.EOF for
	// ReasonTransportClose, nil for the other reasons.
	Err error
}

// Error returns "engineio: session closed: <reason>" followed by ": <err>" when Err is set.
func (e *CloseError) Error() string { return "" }

// Unwrap returns Err, so errors.Is(err, io.EOF) still holds for a peer close.
func (e *CloseError) Unwrap() error { return e.Err }
```

The fence shows the shape; `Error` is implemented as its comment says. `engineio`
re-exports the type and the reasons by alias, the form `### V1-16 contract` uses for
`ReadyState` too (a new file of package `engineio`, the one root `engineio/` edit of V1-4
beside `engineio/client.go`):

```go
package engineio

import "github.com/sshaplygin/go-socket.io/engineio/session"

// CloseError, CloseReason and the five reasons are session's, re-exported.
type (
	CloseError  = session.CloseError
	CloseReason = session.CloseReason
)

const (
	ReasonTransportClose = session.ReasonTransportClose
	ReasonPingTimeout    = session.ReasonPingTimeout
	ReasonTransportError = session.ReasonTransportError
	ReasonForcedClose    = session.ReasonForcedClose
	ReasonServerShutdown = session.ReasonServerShutdown
)
```

Semantics: the reason is the first cause observed,
as today (`observe` keeps the first); `NextReader` returns `*CloseError` by `errors.As`
whether the close came from the read loop (CLOSE packet, failed read, deadline), from
`Close` or from `Server.Close`, and `NextWriter` does the same once closed; a reader or
writer obtained before the close keeps returning its own errors. The root reads the reason
when `serveRead` ends (`server.go:381-418`, `conn.finish`): it takes `errors.As` of the
decoder's error on the frame reader (`c.reader.failed`), maps it to the `OnDisconnect`
reason of socket.io 2.5.0 (`client.js:240-262` passes the engine reason to every socket's
`onclose`): `transport close`, `ping timeout` and `transport error` unchanged, `forced
close` and `server shutting down` both to `forced close` (3.6.2 `server.js:191-196`
closes each client with `close(true)`, which ends in `onClose('forced close')`, so a Node
server reports `forced close` for a shutdown); `Conn.Close` by the application is
`forced close` in V1-4, and V1-5's K20 then makes it the reference's `disconnect(true)`
(a DISCONNECT packet and `server namespace disconnect` per namespace before the engine
close, `### V1-5 contract`); a DISCONNECT packet from the client stays `client namespace
disconnect`, and V1-5's `Disconnect` writes `server namespace disconnect`. A parse error
of the socket.io decoder is not an engine close:
the root closes the connection itself (K29) and reports `forced close`; 3.6.2's
`client.js:228-235` closes the engine connection too, so the client sees the same
`transport close`. Goroutine rule: the reason is read on the read goroutine after the
decoder has returned, before the `OnDisconnect` calls of `finish`, so every handler of
that connection sees the final reason. Named tests: `TestSessionCloseError` (package
`session`, under `// Covers K24`: one subtest per reason over the `scriptConn` fakes of
`session_lifecycle_test.go`, the deadline cases driven as the V1-2 check describes;
`NextReader` and `NextWriter` after each close return an error whose `errors.As` finds
the `*CloseError` with that reason, and `errors.Is(err, io.EOF)` holds for the CLOSE
packet case) and `TestOnDisconnectReason` (root, the same marker stacked: a client that
sends CLOSE, one that stops sending with small ping values, one whose connection the
test resets, `Conn.Close` from a handler and `Server.Close`; each `OnDisconnect` reason
per the map above; the two existing tests that pin the old reason,
`lifecycle_test.go:117-121`, are rewritten to the new one in the same PR). The new
reasons are a behaviour change for code that switches on the string: V1-14's `### Changed`
entry names them (the V1-14 row of the PR table) and the MV1 DoD greps for it.

Writability, for K16 of V1-6, which edits no `engineio/` file. `*session.Session` and
the engine client (`*client`, `engineio/client.go`) gain `Writable`, and each transport
connection they use gains the same method, which they read through the optional
interface `interface{ Writable() bool }` by a type assertion, so `transport.Conn` (a
SYNC manifest file) does not change and a transport connection without the method counts
as writable:

```go
package session

// Writable reports whether the session's transport can take a frame now: false while a
// frame writer obtained from NextWriter is open, while the transport connection reports
// itself not writable, and once the session has closed. A transport connection without
// a Writable method counts as writable. The root reads it through the optional
// interface interface{ Writable() bool } to drop a volatile packet (K16).
func (s *Session) Writable() bool { return false }
```

```go
package engineio

// Writable reports, for the engine client returned by Dialer.Dial, what
// Session.Writable reports for a server session, over the client's transport connection.
func (c *client) Writable() bool { return false }
```

```go
package polling

// Writable reports, for the server connection, that a poll request is waiting and no
// response is being written; for the client connection, that no POST is in flight.
func (c *serverConn) Writable() bool { return false }

func (c *clientConn) Writable() bool { return false }
```

```go
package websocket

// Writable reports that no message writer of the connection is open.
func (c *conn) Writable() bool { return false }
```

The fences show the shape; each body is implemented as its comment says, under the lock
that already guards the state it reads. Named tests: `TestSessionWritable` (package
`session`, under `// Covers K16`, the row staying `ADD V1-6` until V1-6 closes it, and the
per-PR DoD still requires V1-6's `TestVolatileEmit` under its own marker: a
polling session is writable only while a poll waits, a websocket session not while a
writer is open, no session after its close; a `scriptConn` without the method counts as
writable) and `TestEngineClientWritable` (package `engineio`, the same marker stacked, so
the client-side methods are exercised by their own PR and not first by V1-11's
`TestClientVolatile`: the engine client returned by `Dialer.Dial` over polling is not
writable while a POST is in flight, the test server holding that POST's response, and is
writable when idle; over websocket it is not writable only while a writer obtained from
`NextWriter` is open; neither after `Close`).

### V1-5 contract

Rows: S8 (`of`), S11 and N2 (namespace middleware), S14 (namespace `send`), S16 and N4
(`connected`, `sockets`), K15 (socket `send`), K20 (`disconnect`), K25 (event
middleware); with them K1 (`CHG`, the id form) and S17 (`BUG`, `Server.Close` waits),
which the same API carries. Two parts: a server-side namespace object, and three methods
on `Namespace` (D10: K15, K20, K25 are tagged).

```go
package socketio

// ServerNamespace is a namespace of a Server: the registration and broadcast object of
// socket.io 2.5.0's Namespace (namespace.js). Server.Of returns it; Server.OnConnect,
// OnDisconnect, OnError and OnEvent with a namespace argument are shorthands for its
// methods. It is a struct with unexported fields, not a D10 interface; V1-6 and V1-7 add
// methods to it.
type ServerNamespace struct{}

// Of returns the namespace nsp, creating it with an in-memory broadcast (or the
// Redis one when Adapter was called before) if it does not exist; "/" and "" are the
// root namespace. A name without a leading slash gets one, as in the reference.
func (s *Server) Of(nsp string) *ServerNamespace { return nil }

// Use registers f as namespace middleware; see ServerNamespace.Use.
func (s *Server) Use(nsp string, f func(Conn) error) {}

// Name returns the namespace name, "/" for the root namespace.
func (n *ServerNamespace) Name() string { return "" }

// OnConnect, OnDisconnect, OnError and OnEvent are Server's methods of the same names
// for this namespace, the one-handler-per-namespace rule included (S20).
func (n *ServerNamespace) OnConnect(f func(Conn) error)        {}
func (n *ServerNamespace) OnDisconnect(f func(Conn, string))   {}
func (n *ServerNamespace) OnError(f func(Conn, error))         {}
func (n *ServerNamespace) OnEvent(event string, f interface{}) {}

// Use adds f to the middleware chain of the namespace, run in registration order for
// every connection that sends CONNECT to it, before OnConnect. A non-nil error stops
// the chain, answers the client with an ERROR packet whose data is err.Error(), and
// leaves the connection and its other namespaces open; OnConnect and OnDisconnect are
// not called for that namespace. The Conn f receives is the namespace connection
// before it joined any room; its ID, Handshake (V1-7) and Context are usable.
func (n *ServerNamespace) Use(f func(Conn) error) {}

// Emit sends event with args to every socket connected to the namespace, once per
// socket whatever rooms it is in (S12). It returns nil in V1-5; V1-6 adds the
// callback-on-broadcast error.
func (n *ServerNamespace) Emit(event string, args ...interface{}) error { return nil }

// Send is Emit("message", args...).
func (n *ServerNamespace) Send(args ...interface{}) error { return nil }

// Sockets returns a copy of the sockets connected to the namespace on this instance,
// in no particular order; a socket in a middleware chain or already disconnected is not
// in it. Socket returns the one with id (the Conn.ID form, "/ns#sid" outside the root).
func (n *ServerNamespace) Sockets() []Conn               { return nil }
func (n *ServerNamespace) Socket(id string) (Conn, bool) { return nil, false }

// Namespace is the per-connection view embedded in Conn. The existing methods stay;
// V1-5 adds these three.
type Namespace interface {
	// Send is Emit("message", args...); a trailing func is the ack callback as in Emit.
	Send(args ...interface{})

	// Disconnect disconnects this namespace only: it writes a DISCONNECT packet for it,
	// runs OnDisconnect with "server namespace disconnect", and leaves its rooms. The
	// connection and its other namespaces stay open; a second Disconnect, or one on a
	// namespace already disconnected, does nothing. A reference client reports
	// "io server disconnect" and does not reconnect that namespace.
	Disconnect()

	// Use adds f to the event middleware of this socket, run in registration order on
	// the read goroutine for every EVENT packet of the namespace, after the arguments
	// have been decoded and before the event handler. event is the event name; args
	// are the decoded arguments, typed as the handler's parameters, or as []interface{}
	// of generic JSON values when the event has no handler. A non-nil error skips the
	// handler and the ack and answers the client with an ERROR packet whose data is
	// err.Error(); the connection stays open.
	Use(f func(event string, args []interface{}) error)
}
```

Semantics. S8: `Of` and the `Server.On*` shorthands share one handler table
(`namespaceHandlers`), so a handler set through either is seen by both, and `Of` on a
namespace registered after `Server.Close` with `Adapter` set behaves as the existing
`createNamespace` does (`server.go:420-442`). S11 and N2: the chain runs on the read
goroutine of the connection inside the CONNECT handling (`connectPacketHandler`,
`connection_handlers.go:86-126`, and `conn.connect` for the root namespace,
`connection.go:205-233`), so a middleware that blocks delays that client only; an
`OnConnect` error is treated exactly like a middleware error from V1-5 on (today it fails
the connect and closes the whole connection, `connection.go:236-259`), which is the
behaviour change D5 accepts; the ERROR packet is `4/nsp,"<message>"` (`4"<message>"` for
the root), the form V1-4 encodes (R8), and the data is the message string only: the
object form of `err.data` (socket.io 2.5.0 `socket.js:526`) is not carried, a reference
client receives a string in its `error` event either way. A root-namespace rejection
keeps the engine connection open and answers no later CONNECT of that connection, as the
reference holds them until a root socket exists (`client.js:90-93`), which never happens;
the sid room is joined only after the chain and `OnConnect` have passed. The rejection is
still reported to `OnError` as today (`conn.onError`, `connection.go:404-406`): for a
namespace other than the root, to that namespace's `OnError`; for the root, to root
`OnError` with a nil `Conn` (no root socket exists), once, before the ERROR packet is
written. V1-5 rewrites the sentences of the `Conn.Close` godoc (`connection.go:51-56`) that
lists "a failed connect" among the library's closes and says "a failed connect is
reported to root OnError with a nil Conn": a rejected connect no longer closes the
connection, and the godoc says it is reported as above. V1-14's `### Changed` entry
names the change (the `V1-14` row of the PR table). S14 and K15: the
`message` event, nothing more; a reference client's `socket.on('message', ...)` receives
it. S16 and N4: `Sockets` and `Socket` are this instance's view (the `connected` map of
`namespace.js:55-56`; `sockets` is the same set once `onconnect` ran, the only window a
caller can observe); N4's `ids` is the per-connection ack counter (`conn.nextID`) and its
`rooms` the pending `to()` list, both internal, with the V1-6 builder as the public form
of the latter. K20: `Disconnect` writes `1/nsp,` (`1` for the root) through the ordinary
queue, so packets emitted before it arrive first; `OnDisconnect` runs on the caller's
goroutine before `Disconnect` returns when called from a handler of that connection (the
read goroutine, where every handler runs) and otherwise on the caller's goroutine, in
both cases after the rooms are left (K23's `disconnecting` order, V1-4). `Conn.Close`
becomes the reference's `disconnect(true)` (`socket.js:478-487`, `client.js:111-119`):
it writes a DISCONNECT per connected namespace, runs each `OnDisconnect` with `server
namespace disconnect`, then starts the engine close and drain of today's `Close` godoc
(`connection.go:44-57`), so a reference client sees `io server disconnect` on every
namespace and does not reconnect, where at `2667c25` it saw a transport close and
reconnected: a behaviour change for every application that bounces clients with `Close`,
named by V1-14's `### Changed` entry (the V1-14 row) and grepped for by the MV1 DoD. K25: one chain per socket, kept until the namespace disconnects; the ERROR
packet is the one of S11. K1: `Conn.ID()` returns the engine sid in the root namespace
and `<nsp>#<sid>` elsewhere (`socket.js:64`), and the sid room each socket joins is that
value, so `Rooms()` of a `/chat` socket contains `/chat#<sid>`; `Server.ForEach` and the
room methods take the same ids. S17: `Server.Close` returns after every session has been
closed and its `OnDisconnect` handlers have returned (the engine layer closes them, E22 in
V1-2), with a 10 s bound after which it returns anyway and logs the count still open.
Named tests, each under the `// Covers` of its row: `TestServerOf` (S8: `Of` and the
shorthand register the same handler; `Of("chat")` is `Of("/chat")`),
`TestNamespaceMiddleware` (S11, N2 stacked: order, the ERROR packet form with the
message, the connection and root socket kept, no `OnConnect`; an `OnConnect` error gives
the same packet), `TestNamespaceSend` (S14), `TestNamespaceSockets` (S16, N4 stacked:
two connected sockets, one disconnects, one is rejected by middleware), `TestConnSend`
(K15), `TestConnDisconnect` (K20: the packet, the reason, the other namespace still
open, `Close` writing one packet per namespace), `TestEventMiddleware` (K25),
`TestConnIDNamespace` (K1: the two id forms and the sid room) and `TestServerCloseWaits`
(S17: the `fails without the fix:` test, red on the base where `Close` returns with
sessions open).

### V1-6 contract

Rows: A4 (`broadcast(packet, {rooms, except, flags})`), A5 (`clients` ids on the
adapter), S15 (`clients` on the namespace), K11 (`join` of several rooms), K14
(broadcast except the sender), K16 (`volatile`), S21 (the `volatile` and `local` flags of
server broadcasts; `compress` is `### V1-15 contract`, `json` and `binary` are N/A); with
them S13 (`CHG`, room union), N3 (`CHG`, the per-namespace methods, which the
`ServerNamespace` of V1-5 carries), A1 (`CHG`, internal maps) and N7 (`TEST`, the
callback-on-broadcast rule). The Redis half of A4, A5, S13 and S15 is *`Except` with the
Redis adapter* above, whose rules this contract names and does not repeat. D10 tags: A4,
K11, K14, K16 carry it; A5 gets it in this PR, since `IDs` is a `Broadcast` method.

```go
package socketio

import "errors"

// ErrExceptUnsupported is returned, wrapped, by a Broadcast that cannot exclude sockets:
// the Redis Broadcast in v1.5.0 (Stage V1R implements it). Nothing is sent.
var ErrExceptUnsupported = errors.New("socketio: broadcast except is not supported by this adapter")

// ErrBroadcastAck is returned by a broadcast whose last argument is a func: an
// acknowledgement callback is not supported when broadcasting (socket.io 2.5.0 throws
// "Callbacks are not supported when broadcasting"). Nothing is sent.
var ErrBroadcastAck = errors.New("socketio: callbacks are not supported when broadcasting")

// ErrServerOnly is returned by Emit and Send of a broadcast Emitter (one that To or
// Broadcast started, or that To or Except switched from a single-socket target) on a
// client connection, whose namespace has no Broadcast to deliver through: the root
// Client's connection (client.go:165 creates its handlers with a nil adapter), and from
// V1-9 on the client package's, which declares the variable then (Stage 7, D10 methods and
// the alias) while the root keeps the name as an alias. V1-6 declares it in the root's
// errors.go, beside ErrWriteBufferFull. Nothing is sent.
var ErrServerOnly = errors.New("socketio: server-only method on a client connection")

// BroadcastOptions selects the targets and flags of one broadcast, the opts of
// socket.io-adapter 1.1.2 broadcast(packet, opts). The zero value is every socket of the
// namespace with no flag. V1-15 adds NoCompress.
type BroadcastOptions struct {
	// Rooms: the sockets in any of the rooms, once each (a union with dedupe); empty
	// means every socket of the namespace.
	Rooms []string
	// Except: ids (Conn.ID) of sockets that receive nothing.
	Except []string
	// Volatile: a socket whose connection cannot take the packet now is skipped.
	Volatile bool
	// Local: this instance's sockets only; a cluster adapter publishes nothing.
	Local bool
}

// Broadcast is the room and delivery adapter of a namespace. The existing methods stay;
// V1-6 adds these two (D10: implementers and mocks must add them).
type Broadcast interface {
	// SendTo delivers event with args per opts, once per socket, on the caller's
	// goroutine without holding the adapter's lock, through each target's single-socket
	// Emitter: target.Emit(event, args...) with no flag, target.Volatile().Emit with
	// opts.Volatile, target.Compress(false).Emit with opts.NoCompress (V1-15), both
	// chained when both are set. An adapter uses nothing else of a target, so an
	// injected Broadcast (V1-7) honours the flags exactly as the memory type does. It
	// returns an error wrapping ErrExceptUnsupported, delivering nothing, when
	// opts.Except is not empty and the adapter cannot exclude.
	SendTo(opts BroadcastOptions, event string, args ...interface{}) error

	// IDs returns the ids of the sockets in any of rooms, once each, or of every socket
	// of the namespace when rooms is empty; a copy, in no particular order.
	IDs(rooms ...string) []string
}

// Emitter is a pending emit: a target plus flags, built by chaining and sent by Emit or
// Send. Values are immutable: each method returns a new Emitter, so one can be kept and
// reused. ServerNamespace.To, Except, Volatile and Local start one that targets the
// namespace; Namespace.To and Broadcast start a broadcast from the socket: the namespace
// (or the rooms) with the socket's own ID in Except. Namespace.Volatile and Compress
// (V1-15) start one that targets the socket itself, and it stays so while only Volatile,
// Compress and Local are chained (Local is a no-op on it, as the local flag of socket.io
// 2.5.0 is on a single-socket emit). The first To or Except on it turns it into a
// broadcast from the socket, exactly what Namespace.To or Namespace.Broadcast would have
// started, with the flags kept: c.Volatile().To(r) is c.To(r).Volatile() and
// c.Volatile().Except(ids...) is c.Broadcast().Except(ids...), so the sender never
// receives it (socket.io 2.5.0 socket.js:140-178: any room or the broadcast flag goes to
// the adapter with except [this.id]). V1-6 declares the interface in the root's
// namespace_conn.go, the file of the *namespaceConn that implements it; from V1-9 on it
// is declared in package client and that file holds the alias (Stage 7, D10 methods and
// the alias), so both sides implement it.
type Emitter interface {
	// To adds rooms to the target: the sockets in any of them, once each. On a
	// single-socket target it switches to a broadcast from the socket (see above).
	To(rooms ...string) Emitter
	// Except adds ids of sockets that receive nothing. On a single-socket target it
	// switches to a broadcast from the socket (see above).
	Except(ids ...string) Emitter
	// Volatile marks the packet volatile: a socket whose connection cannot take it now
	// is skipped, as socket.io 2.5.0 does when the transport is not writable.
	Volatile() Emitter
	// Local restricts the broadcast to this instance; a cluster adapter publishes nothing.
	Local() Emitter
	// Emit sends event with args to the target. It returns ErrBroadcastAck when the last
	// argument is a func and the target is a broadcast, and the adapter's error, both
	// without sending; a single-socket target never fails.
	Emit(event string, args ...interface{}) error
	// Send is Emit("message", args...).
	Send(args ...interface{}) error
}

// ServerNamespace gains the builder entry points and the id list. Server.Of(nsp).Except
// is D5's Except on Server (D9: the namespace form; no Server method takes the namespace
// as a string for it).
func (n *ServerNamespace) To(rooms ...string) Emitter   { return nil }
func (n *ServerNamespace) Except(ids ...string) Emitter { return nil }
func (n *ServerNamespace) Volatile() Emitter            { return nil }
func (n *ServerNamespace) Local() Emitter               { return nil }

// SocketIDs returns Broadcast.IDs of the namespace: every socket id, or the sockets in
// any of rooms. Over the Redis Broadcast of v1.5.0 it lists this instance only.
func (n *ServerNamespace) SocketIDs(rooms ...string) []string { return nil }

// Namespace: Join changes from Join(room string) to the variadic form, which every call
// site compiles with unchanged and every implementer must follow (V1-14's ### Changed
// entry names both forms); To, Broadcast and Volatile are new.
type Namespace interface {
	// Join joins the socket to each room, in order, skipping rooms it is already in; a
	// room is joined before the next one is attempted. No error: the memory adapter
	// cannot fail and the Redis one registers locally (socket.io 2.5.0 join(rooms, cb)).
	Join(rooms ...string)
	// To starts a broadcast to the sockets in any of rooms except this one. On a client
	// connection the Emitter's Emit and Send return ErrServerOnly.
	To(rooms ...string) Emitter
	// Broadcast starts a broadcast to every socket of the namespace except this one. On
	// a client connection the Emitter's Emit and Send return ErrServerOnly.
	Broadcast() Emitter
	// Volatile starts an emit to this socket that is dropped when the connection
	// cannot take it now.
	Volatile() Emitter
}
```

Semantics. A4 and S13: the memory `SendTo` collects the targets under its read lock
(rooms in the given order, each socket once, `Except` removed), releases it, then calls
`Emit` on each (the `members` pattern of `broadcast.go:106-118`), so an `Emit` that
closes a connection does not run under the lock; `Send` and `SendAll` stay and are
`SendTo` with one room or none. The Redis `SendTo` is defined in *`Except` with the Redis
adapter*: `ErrExceptUnsupported` before any Redis I/O when `Except` is not empty, one
message per room for a union, `Local` publishes nothing, `Volatile` honoured locally and
not carried. Writable, for `Volatile`: the packet is dropped at `Emit` when the
connection's outbound queue already holds a packet or the engine connection reports that
its transport cannot take a frame now, read through the optional interface `interface{
Writable() bool }` that `*session.Session` and the engine client implement from V1-4 on
(`### V1-4 contract`, the writability fences; V1-6 edits no `engineio/` file); a
connection without the method, `*fakeConn` included, is always writable. The reference checks
`transport.writable` at packet time (`client.js:167`); the queue condition is the Go
equivalent, since a queued packet means the transport did not take the previous one.
N7: `Emitter.Emit`, `ServerNamespace.Emit` and `Send` return `ErrBroadcastAck` for a
trailing func (so V1-5's "returns nil" is superseded here); the legacy
`Server.BroadcastToRoom`, `BroadcastToNamespace`, `Broadcast.Send` and `SendAll`, which
return no error, keep registering one callback per connection, which N7's test records
and their godoc states; no reference client can observe either (D9). A5 and S15: `IDs`
is `clients` of `socket.io-adapter` 1.1.2 (`index.js:173-211`); `Socket(id)` of V1-5
turns an id into its `Conn`. A1: the memory type keeps, beside `rooms` (room to sockets),
the per-socket map of the adapter's `sids` (`index.js:21-26`: socket id to its rooms),
updated together under the same lock by `Join`, `Leave`, `LeaveAll` and `Clear`, so
`Rooms(conn)` and `LeaveAll` read one entry instead of scanning every room
(`getRoomsByConn`, `broadcast.go:155-165`, goes); `IDs` with no room lists the keys of
that map. N3: the per-namespace forms of `to`, `in`, `emit`, `send` and `clients` are the
`ServerNamespace` methods of V1-5 and this contract; the `Server.Broadcast*` methods
that take the namespace as a string stay as they are (N7 records their callback rule). K11: a `Join` with a room already joined is a no-op for that
room (`socket.js:236-246`). K14: `To` and `Broadcast` pass `Except: []string{c.ID()}`,
so over the Redis `Broadcast` of `v1.5.0` their `Emit` returns `ErrExceptUnsupported`
(the `_examples/default-http` `others()` helper stays for the Redis examples, V1-13); on
the root `Client`'s connection, the same `*namespaceConn` type whose handlers
`client.go:165` creates with a nil adapter, `Emit` and `Send` of the `Emitter` return
`ErrServerOnly` and send nothing, from V1-6 on, so the client-side meaning of the two
methods is fixed before V1-9 copies the type into `client`. K16
and S21: the flags on `ServerNamespace` apply to a namespace broadcast; `json` is a no-op
in 2.5.0 and `binary` is decided by `parser.Buffer`, both N/A as PARITY.md says.
Ordering: packets to one socket from one goroutine arrive in call order, broadcasts
included, since every path queues through that socket's `conn.write`. Named tests, each
under the `// Covers` of its row: `TestBroadcastSendTo` (A4: rooms union with dedupe,
`Except`, `Volatile` (a target whose connection is not writable gets nothing, the others
get the packet: the flag went through each target's `Volatile()` Emitter), `Local` on
the memory type; a fake `Broadcast` whose `SendTo`
returns `ErrExceptUnsupported` makes `Conn.Broadcast().Emit` return it with nothing sent),
`TestRedisBroadcastExceptUnsupported` (A4 stacked, no Redis server: the Redis type's
`SendTo` with `Except` returns it before any I/O, the pool untouched), `TestBroadcastIDs`
(A5), `TestNamespaceSocketIDs` (S15), `TestConnJoinRooms` (K11), `TestBroadcastExceptSender`
(K14: `To` and `Broadcast` from a handler reach the other sockets and not the sender; a
subtest each for `c.Volatile().To(room)` and `c.Volatile().Except(id)`, which reach the
room or the namespace minus `id` and not the sender, and for `c.Volatile().Local()`, which
reaches the sender only; a subtest on the root `Client`'s connection gets `ErrServerOnly`
from `To`, `Broadcast` and `Volatile().To(room)` and the server receives nothing),
`TestBroadcastRoomsMaps` (A1: after `Join` of three rooms, `Leave` of one and
`LeaveAll`, `Rooms(conn)`, `AllRooms`, `Len` and `IDs` agree with the two maps at every
step, and a socket left in no room is gone from both), `TestServerNamespaceMethods` (N3:
`Server.Of("/chat").To(room).Emit`, `.Emit`, `.Send` and `.SocketIDs` reach or list the
same sockets as `BroadcastToRoom`, `BroadcastToNamespace` and `RoomLen` of the same
namespace), `TestVolatileEmit` (K16: a polling client with no poll waiting gets nothing, a websocket
client gets the packet), `TestServerBroadcastFlags` (S21: `Volatile` and `Local` on
`ServerNamespace`), `TestRoomUnion` (S13: a socket in two rooms of a `To(a, b)` receives
once), `TestBroadcastCallback` (N7: `ErrBroadcastAck` on the new paths, the legacy
behaviour recorded) and the four Redis-less tests of *`Except` with the Redis adapter*,
counted by the V1-6 block above.

### V1-7 contract

Rows: S5 (pluggable adapter), S9 (dynamic namespaces), N5 (`adapter` handle), N8
(namespace CONNECT query), K4 (`conn`), K5 (`request`), K7 (`handshake.query`), K10
(`handshake.time`, `issued`, `secure`, `xdomain`), K26 (late ack), K28 (several listeners
per event), A2 (`addAll`). D10 tags: K4, K5, K7, K10 carry it (`Conn` methods); K26 and
K28 are untagged on purpose (PARITY.md, *Reading the tables*) and this contract keeps them
off `Conn`.

```go
package socketio

import (
	"net/http"
	"net/url"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
)

// AdapterFunc builds the Broadcast of a namespace; it is called once per namespace,
// under the Server's creation lock, when the namespace is first registered or Of is
// called. An error leaves the namespace with a no-op Broadcast and is reported as a
// Redis construction error is today (Adapter, Serve).
type AdapterFunc func(namespace string) (Broadcast, error)

// SetAdapter installs f for the namespaces created after the call; the last of
// Adapter and SetAdapter wins. Adapter is SetAdapter with the Redis constructor.
func (s *Server) SetAdapter(f AdapterFunc) {}

// OfFunc registers a dynamic namespace (socket.io 2.5.0 of(fn), of(regexp)): a CONNECT
// to a name that no registered namespace has is offered to every match function in
// registration order, with the namespace query of the CONNECT packet; the first that
// returns true creates the namespace, which then exists as if Of had been called, with
// the handlers, listeners and middleware the returned parent holds at that moment. A
// name no function accepts is answered "Invalid namespace" (S10). match runs on the
// read goroutine of the connecting client. For a regexp, pass re.MatchString wrapped.
func (s *Server) OfFunc(match func(name string, query url.Values) bool) *ServerNamespace {
	return nil
}

// Adapter returns the Broadcast of the namespace, nil when its construction failed or
// the namespace is a dynamic parent.
func (n *ServerNamespace) Adapter() Broadcast { return nil }

// On adds a listener for event, called after the ones registered before it; the
// returned func removes it. Once is On for the first event only. Off removes every
// listener of event, the OnEvent one included. OnEvent replaces every listener of
// event with f, as today. All listeners of one event take the same parameter types
// (the arguments are decoded once); On panics, as OnEvent does for a non-func, when
// f's parameter types differ from a listener already registered.
func (n *ServerNamespace) On(event string, f interface{}) (off func())   { return nil }
func (n *ServerNamespace) Once(event string, f interface{}) (off func()) { return nil }
func (n *ServerNamespace) Off(event string)                              {}

// AckFunc and Handshake are declared in the root's connection.go, beside Conn, until
// V1-9 moves them into package client and leaves the aliases there (Stage 7, D10
// methods and the alias).

// AckFunc is the acknowledgement callback of an event handler: a handler (OnEvent, On,
// Once) whose last parameter has this type receives one and owns the ACK: the first
// call sends an ACK packet with args, later calls do nothing, and the call may come
// from any goroutine after the handler returned (a late ack). The handler's return
// values are then not sent. When the client did not ask for an acknowledgement the
// callback does nothing.
type AckFunc func(args ...interface{})

// Handshake is the handshake data of a namespace socket (socket.io 2.5.0
// socket.handshake), built from the handshake request of the connection that
// Conn.Request returns, never from the current transport, so a socket connected after
// the websocket upgrade reports the polling handshake's values, as the reference does.
type Handshake struct {
	// Headers of the handshake request (Request().Header).
	Headers http.Header
	// Time the socket was created; both the reference's time and issued.
	Time time.Time
	// Address is the remote address of the handshake request (Request().RemoteAddr,
	// host and port as net/http gives it; the reference's remoteAddress has no port).
	Address string
	// XDomain reports an Origin header on the handshake request.
	XDomain bool
	// Secure reports a TLS handshake request.
	Secure bool
	// URL is the handshake request URL (*Request().URL).
	URL url.URL
	// Query is the handshake request query merged with the query of the namespace
	// CONNECT packet ("/chat?x=1"), the latter winning on a shared key.
	Query url.Values
}

// Conn gains the connection-level accessors.
type Conn interface {
	// EngineConn returns the engine.io connection the socket runs on, whose state
	// V1-16 exposes; nil on a client connection.
	EngineConn() engineio.Conn
	// Request returns the handshake request of the connection (the polling GET or the
	// websocket upgrade), the one the engine session keeps (session.Session.Request):
	// its Body is consumed and its Context is done once the handshake response was
	// written, so it serves for the URL, headers, TLS state and remote address only.
	// Non-nil from the first call of any handler or middleware of the connection on and
	// for the rest of the Conn's lifetime; nil on a client connection and on an engine
	// connection without a Request method.
	Request() *http.Request
	// Handshake returns the handshake data of this namespace socket, filled when the
	// socket is created, before any handler or middleware of it runs; the zero value
	// on a client connection and when Request is nil.
	Handshake() Handshake
}
```

Semantics. S5: `SetAdapter` replaces the Redis-only construction of
`namespace_handler.go:25-38` with `AdapterFunc`; `Server.Adapter(opts)` keeps its
signature and installs the Redis constructor through it. An injected `Broadcast`
implements the whole interface, V1-6's `SendTo` and `IDs` included; its methods are
called concurrently from handler goroutines and from `Server` methods, and `SendTo` must
call `Emit` without holding its own lock (the rule of V1-6), which the `Broadcast` godoc
states, the v1 counterpart of v2's `docs/ADAPTERS.md`. S9: the parent returned by `OfFunc`
holds registrations and copies them into each child on creation (`parent-namespace.js:
30-37`), so a registration made after a child exists does not reach that child, as in the
reference; the parent's `Emit`, `Send` and builder (`To`, `Except`, `Volatile`, `Local`)
broadcast to every child (`parent-namespace.js:16-26`), its `Sockets` and `SocketIDs`
return the union of its children's, its `Adapter` is nil; a child gets its own
`Broadcast` from the adapter in force. N5: `Adapter` is the handle; `Server.Rooms`,
`RoomLen` and `ForEach` keep working. N8 and K7: the decoder already parses the namespace
query into `parser.Header.Query` (`decoder.go:268-273`); V1-7 stores it on the namespace
connection, and `Handshake().Query` merges it over `Request().URL.Query()`
(`socket.js:116-120`). K4: `EngineConn` returns the `engineio.Conn` embedded in `conn`
(`connection.go:69`). K5: the engine session keeps its handshake request from `newSession` on
(`Session.SetRequest` and `Request`, `### V1-2 contract`, merged in wave V1A). The root
`conn` reads it once, in `newConn`, through the optional interface `interface{ Request()
*http.Request }` on its `engineio.Conn` (the pattern of V1-4's `Writable`) into a field
that `Request()` returns and `Handshake()` is built from, so the guarantee, `Request()`
non-nil from the first handler or middleware call of the connection on, holds because
the engine set the request before the OPEN write and `Accept` returns the session after
it, and the field outlives the read goroutine, so a `Conn` kept by user code or used
from a goroutine started in a handler keeps its request. Nothing is kept outside the
session: a session that is never accepted (its OPEN write failed) is removed by the
engine with its request, and a connection whose `engineio.Conn` has no `Request` method
(the client's, the root test fakes) returns nil. No wrapper of `Options.ConnInitor` and
no map; V1-7 edits no `engineio/` file.
K10: the
fields as commented, all from the kept handshake request `r` of K5 (`socket.js:121-129`
reads `this.request`, the handshake request, never the current transport's;
`Conn.RemoteHeader`, `RemoteAddr` and `URL` read the current transport under
`upgradeLocker`, `engineio/session/session.go:258-283`, so after an upgrade they are the
websocket request's and are not used); `Secure` is `r.TLS != nil`, `XDomain` the presence
of `Origin` (`socket.js:123-127`); `Address` keeps the port of `r.RemoteAddr` (D9: the Go
form; `net.SplitHostPort` gives the reference's value); one `time.Time` stands for `time` and `issued`. K26: `AckFunc`;
today's rule (return values sent when non-empty or when the client asked,
`connection_handlers.go:78-81`) stays for handlers without one. With several listeners
(K28) the ACK is sent once: by the first listener in order that returns a non-empty value
list, or, when any listener of the event takes an `AckFunc`, only by a callback call (no
automatic empty ACK then); otherwise an empty ACK after the last listener when the client
asked. K28: listeners run in registration order on the read goroutine; one added or
removed during a dispatch takes effect from the next event; `Once` removes itself before
it runs. A2: no new method. `Conn.Join(rooms...)` of V1-6 calls `Broadcast.Join` once per
room in order, so an injected adapter sees every room; the reference's `addAll` exists to
update its `sids` and `rooms` maps together, which a Go adapter does inside its own
`Join`, and D9 keeps the `Conn` argument (the id is `connection.ID()`), so the row closes
with the injection of S5 and a test, not a method. Named tests, each under the
`// Covers` of its row: `TestSetAdapter` (S5: an injected fake receives `Join`, `SendTo`
and `IDs`; a constructor error surfaces through `Serve` as the Redis one does),
`TestDynamicNamespaces` (S9: a match creates the child with the parent's handlers, a
second CONNECT reuses it, a rejected name gets `Invalid namespace`, the parent's `Emit`
reaches two children), `TestNamespaceAdapter` (N5), `TestNamespaceConnectQuery` (N8),
`TestConnEngineConn` (K4), `TestConnRequest` (K5: method, URL and a header of the
handshake request, asserted inside the root `OnConnect` and inside a `/chat` namespace
middleware, both non-nil on every one of 20 connections under `-race`, and still
non-nil on a `Conn` the test kept after the connection closed; nil on a connection over
`*fakeConn`, and on the client side from V1-9), `TestHandshakeQuery` (K7: the merge and the override),
`TestHandshakeData` (K10: `Time` within the test's window, `XDomain` with and without
`Origin`, `Secure` over `httptest.NewTLSServer`, the fields read inside the root
`OnConnect` and inside a namespace middleware under `-race`, as `TestConnRequest` does;
a `/chat` socket connected after the websocket upgrade reports the polling handshake's
`Headers`, `URL` and `Address`, a header set only on the upgrade request being absent),
`TestLateAck` (K26: the ACK sent from another goroutine after the handler returned, a
second call ignored, no ACK when the client did not ask), `TestEventListeners` (K28:
order, `Once`, `off()`, `Off`, `OnEvent` replacing, the panic on differing parameter
types) and `TestAdapterJoinRooms` (A2).

### V1-10 contract

Rows: C2 (several namespaces on one connection), C4 (`timeout`, `connect_error`,
`connect_timeout`), C5 (websocket and upgrade), C6 (`query`, `path`, `extraHeaders`);
with them C9 (`CHG`, emit before connect buffered) and C10 (`CHG`, DISCONNECT on close).
Package `client` (Stage 7, merged as V1-9 before this PR); the root keeps only its
deprecated `Client` wrapper, which gains nothing. The reference is socket.io-client 2.5.0
(`lib/manager.js`, `lib/socket.js`) over engine.io-client 3.5.6.

```go
package client

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// ErrConnectTimeout is returned by Connect when the engine connection is not open
// within Options.Timeout (the reference's connect_timeout).
var ErrConnectTimeout = errors.New("client: connect timeout")

// ErrServerOnly already exists here: declared by V1-6 in the root (the V1-6 contract),
// moved into this package and aliased in the root by V1-9 (Stage 7). Not redeclared.

// Options configures a Client made by New. NewClient(addr, *engineio.Options) stays as
// Stage 7 froze it: it maps WriteBufferSize and Logger onto an Options and keeps the
// engineio.Options' PingTimeout as the drain deadline of Close (the unexported limits
// both constructors build, connection.go:29-40 and the Close godoc of client.go:104-106
// at 2667c25); Options has no such field, so a Client made by New drains for the
// default, one minute. V1-11 adds the reconnection fields, V1-15 PerMessageDeflate.
type Options struct {
	// Transports in upgrade order; nil means polling then websocket, the reference
	// client's default: the first is dialled, the others are probed in order (C5).
	Transports []transport.Transport
	// Path of the engine.io endpoint; "" means "/socket.io".
	Path string
	// Query is added to the engine request query of every request. The query of a
	// namespace goes in the socket name ("/chat?x=1"), see Client.Socket.
	Query url.Values
	// Header is sent with every request (the reference's extraHeaders).
	Header http.Header
	// Timeout bounds Connect and each reconnection attempt, from the dial to the OPEN
	// packet. 0 means 20 seconds, the reference default; a negative value means no bound.
	Timeout time.Duration
	// WriteBufferSize and Logger are engineio.Options' fields of the same names.
	WriteBufferSize int
	Logger          *slog.Logger
}

// New returns a Client for addr, "http://host:port[/namespace]": the path names the
// namespace of the Client's own Emit and On* methods, "/" when empty, as NewClient
// reads it. It returns ErrEmptyAddr for an empty addr and the url.Parse error for one
// that does not parse, as NewClient does; a nil opts means the defaults of every field.
// Nothing is dialled until Connect.
func New(addr string, opts *Options) (*Client, error) { return nil, nil }

// Socket returns the socket of namespace nsp, creating it: a name with a query
// ("/chat?x=1") sends that query in its CONNECT packet (N8 on the server). Once the
// Client is connected a new socket sends its CONNECT at once; before, Connect sends
// it. The Client's own Emit, OnConnect, OnDisconnect, OnError and OnEvent (the frozen
// set of Stage 7; Client has no Send) are those of Socket(<namespace of addr>).
func (c *Client) Socket(nsp string) *Socket { return nil }

// Socket is one namespace of a Client (socket.io-client 2.5.0 Socket). Its handlers
// receive the Conn of that namespace, as the Client's do today. V1-11 adds ID,
// Connected, On, Once and Off.
type Socket struct{}

func (s *Socket) Namespace() string                      { return "" }
func (s *Socket) Emit(event string, args ...interface{}) {}
func (s *Socket) Send(args ...interface{})               {}
func (s *Socket) OnConnect(f func(Conn) error)           {}
func (s *Socket) OnDisconnect(f func(Conn, string))      {}
func (s *Socket) OnError(f func(Conn, error))            {}
func (s *Socket) OnEvent(event string, f interface{})    {}
func (s *Socket) Close() error                           { return nil }
```

```go
package engineio

// Dialer gains Upgrade (C5); the existing fields stay.
type Dialer struct {
	// Upgrade dials Transports[0], then probes each following transport in order as
	// engine.io-client 3.5.6 does, staying on the current transport when a probe fails.
	// false, the zero value, keeps today's rule: the last transport first, with fallback.
	Upgrade bool
}
```

Semantics. C2: one engine connection, one `Socket` per namespace; the root namespace
is connected by the server (N10), the others by a CONNECT per socket, sent by `Connect`
for the sockets that exist then and by `Socket` afterwards (`socket.js:107`). `Connect`
returns once the engine connection is open and the CONNECT packets are queued; each
socket's `OnConnect` runs when the server's CONNECT reply arrives, on the read goroutine.
`Socket.Close` is the reference's `socket.disconnect()`: a DISCONNECT packet for that
namespace and `OnDisconnect` with `io client disconnect`; the connection closes when the
last socket has closed (`socket.js:383-397`, `io.destroy`). `Client.Close` does that for
every socket, then closes the engine connection (C10). `OnDisconnect` reasons on the
client are the reference client's: `io client disconnect`, `io server disconnect` (a
DISCONNECT packet from the server), and the engine reasons `transport close`, `ping
timeout` and `transport error` (`socket.js:208-213`, `manager.js:500-511`), the last
three read from the `*session.CloseError` that V1-4 gives `NextReader`; V1-10 makes the
engine client (`engineio/client.go`) return it too: `transport close` for a CLOSE packet,
`ping timeout` when the read deadline armed by the last PONG passes (`client.go:70`),
`transport error` for any other failure, `forced close` after `Close`. C4: `Timeout` as
commented; `Connect` returns the dial error (the reference's `connect_error`) or
`ErrConnectTimeout` (its `connect_timeout`) and leaves the Client in the state before the
call, so `Connect` can be called again; no event is needed for a synchronous call. C5:
`engineio.Dialer` gains the field `Upgrade bool`: when true, `Dial` dials `Transports[0]`
and probes each following transport in order as engine.io-client 3.5.6 does (`2probe`,
`3probe`, `5`, then the switch), staying on the current transport when a probe fails;
when false it keeps today's rule (`dialer.go:36-104`, the last transport first with
fallback), so `Dialer` users are unchanged. `client` sets `Upgrade` and
`Transports: []transport.Transport{polling.Default, websocket.Default}` by default, and
a `Transports` of one entry dials it directly (`websocket.Default` alone: a
websocket-first connection, which V1-15's cookie rule distinguishes). C6: `Path`, `Query`
and `Header` as commented; `Header` reaches every request, the websocket upgrade included
(the `requestHeader` of `Transport.Dial`). C9: an `Emit` on a socket that is not yet
connected is queued and sent after the server's CONNECT reply (`socket.js:160-164`), at
most `WriteBufferSize` packets, beyond which `Emit` drops the packet and logs at WARN;
today it is logged and dropped (`client.go:113-119`). Client-side behaviour of every D10
method (Stage 7, *D10 methods and the alias*; the rule that each stub's godoc states it):
`Send`, `Volatile` and `Compress` (V1-15) work as on the server; `Disconnect` is
`Socket.Close` of that namespace; `Use` (event middleware) registers nothing; `Join`,
`Leave`, `LeaveAll` and `Rooms` keep the local rooms of today; `To` and `Broadcast` return
an `Emitter` whose `Emit` and `Send` return `ErrServerOnly`; `Connected` (V1-11) works;
`EngineConn` and `Request` return nil; `Handshake` returns the zero value; `ID` returns
the engine sid in the root namespace and `<nsp>#<sid>` elsewhere, the reference client's
`socket.id` (`manager.js:109-110`, `generateId`, called at `:96,378`). Named tests, each under the `// Covers`
of its row, all against an in-process Go server unless said otherwise:
`TestClientNamespaces` (C2: two sockets, one connected before `Connect` and one after,
each `OnConnect` once, an event per namespace, `Socket.Close` of one leaves the other),
`TestClientConnectTimeout` (C4: a server that accepts TCP and never answers; `Connect`
returns `ErrConnectTimeout` within `Timeout` + 1 s and a second `Connect` against a real
server works), `TestClientWebsocketUpgrade` (C5: with the default `Transports` the server
session's `Transport()` is `websocket` after the connect, with `polling.Default` alone it
stays `polling`, with `websocket.Default` alone the handshake is a websocket upgrade),
`TestClientOptions` (C6: the path, the query and a header seen by the server's
`Handshake` and `Request`), `TestClientEmitBuffered` (C9: an `Emit` before `Connect`
arrives after the CONNECT reply, in order with later ones) and
`TestClientCloseSendsDisconnect` (C10: the server's `OnDisconnect` reason is `client
namespace disconnect` for `Socket.Close` and `Client.Close`). The conformance step of
V1-8 that runs the Go client against the Node `socket.io@2.5.0` server runs green on the
V1-10 head (the per-PR checks above).

### V1-11 contract

Rows: C3 (reconnection), C8 (`once`, `off`, `ping`, `pong`, `socket.id`, `connected`),
C11 (the `volatile` flag; `compress` is V1-15's, `binary` has no Go counterpart, as K18
says, so the row closes with the two flags). D10 tags: C8 and C11 carry it; the
`Connected` method is the one addition to `Namespace`, implemented by both sides.

```go
package client

import "time"

// Options gains the reconnection fields (socket.io-client 2.5.0 Manager options).
type Options struct {
	// DisableReconnection turns automatic reconnection off; false, the zero value, is
	// the reference default reconnection: true.
	DisableReconnection bool
	// ReconnectionAttempts is the number of attempts before giving up; 0 means
	// unlimited (the reference's Infinity).
	ReconnectionAttempts int
	// ReconnectionDelay is the delay before the first attempt; 0 means 1 second.
	ReconnectionDelay time.Duration
	// ReconnectionDelayMax caps the delay; 0 means 5 seconds.
	ReconnectionDelayMax time.Duration
	// RandomizationFactor is the jitter of the delay, in (0, 1]; 0 means 0.5 and a
	// negative value means no jitter.
	RandomizationFactor float64
}

// The reconnection events of the reference Manager; a nil f removes the handler.
// OnReconnectAttempt is the reference's reconnect_attempt and reconnecting, fired at
// the same moment there.
func (c *Client) OnReconnectAttempt(f func(attempt int)) {}
func (c *Client) OnReconnect(f func(attempt int))        {}
func (c *Client) OnReconnectError(f func(err error))     {}
func (c *Client) OnReconnectFailed(f func())             {}

// OnPing fires when the client writes a PING, OnPong when the PONG arrives, with the
// round trip (the reference's ping and pong events).
func (c *Client) OnPing(f func())                      {}
func (c *Client) OnPong(f func(latency time.Duration)) {}

// ID returns the socket id the server assigned (Conn.ID of the connection) and "" while
// not connected; Connected reports whether the namespace is connected now.
func (s *Socket) ID() string      { return "" }
func (s *Socket) Connected() bool { return false }

// On, Once and Off are the listener methods of V1-7's ServerNamespace, for one socket.
func (s *Socket) On(event string, f interface{}) (off func())   { return nil }
func (s *Socket) Once(event string, f interface{}) (off func()) { return nil }
func (s *Socket) Off(event string)                              {}

// Namespace gains Connected on both sides.
type Namespace interface {
	// Connected reports whether this namespace socket is connected: true from the
	// CONNECT reply (client) or OnConnect (server) until Disconnect, Close or the
	// connection's close.
	Connected() bool
}
```

Plumbing of `OnPing` and `OnPong`: at `2667c25` the engine client writes PING itself, in
`serve()` (`engineio/client.go:119-128`), and consumes PONG inside `NextReader`
(`:68-72`, which only re-arms the read deadline), so package `client` sees neither
event. V1-11 adds two observer fields to `engineio.Dialer` (`engineio/dialer.go`; V1-10
adds `Upgrade` to the same struct and merges first), which `Dial` copies into the
connection it returns; `client.New` sets them to the funcs that fire the two handlers:

```go
package engineio

import "time"

// Dialer gains the heartbeat observers; the existing fields and V1-10's Upgrade stay.
type Dialer struct {
	// OnPing is called after each PING packet has been written, on the connection's
	// ping goroutine (serve); OnPong when NextReader consumes the PONG, on the goroutine
	// that called NextReader, with the time since that PING. A nil field is not called;
	// a set one must not block.
	OnPing func()
	OnPong func(rtt time.Duration)
}
```

Semantics. C3: when the engine connection closes for a reason other than `Client.Close`
or the last socket's `Close`, every socket's `OnDisconnect` runs with the engine reason,
then the Client reconnects unless `DisableReconnection`: attempt n waits
`min(ReconnectionDelayMax, ReconnectionDelay * 2^(n-1))` with the jitter of `backo2`
(`deviation = floor(rand * factor * delay)`, added or subtracted by a second random bit,
then capped), fires `OnReconnectAttempt(n)`, dials with `Timeout`, and on failure fires
`OnReconnectError(err)` and schedules the next attempt, or `OnReconnectFailed` after
`ReconnectionAttempts` failures, after which `Connect` may be called again; on success it
fires `OnReconnect(n)` once the engine connection is open and before the sockets'
CONNECT packets are sent, after which each socket's `OnConnect` runs again on its CONNECT
reply (`manager.js:519-580`). A socket closed by the server (`io server disconnect`) is
not reconnected, and when every socket is gone the connection closes without
reconnecting (`socket.js:216-245`). A socket's `Emit` during a reconnection is buffered
as C9 says. Goroutine rule: one goroutine of the Client runs the reconnection loop and
the four reconnect handlers, after the previous connection's read, write and error
goroutines have ended, so no two handlers of one Client run concurrently; `OnPing` runs on the engine
client's ping goroutine and `OnPong` on the Client's read goroutine (the one that calls
`NextReader`, where the engine client consumes the PONG), through the `Dialer` fields
above, so neither may block. C8: `ID`, `Connected`, `On`, `Once`, `Off` as commented; `Off`
removes the `OnEvent` handler too, the rule of V1-7. C11: socket.io-client 2.5.0 has no
`volatile` flag (its `lib/socket.js` defines `compress`, `:422-424`, and `binary`,
`:435-436`, only; the flag arrived in the 3.x client), so the client's `Volatile` is a
D10 extension, not reference parity: `Namespace.Volatile` is in both method sets by the
alias rule (Stage 7, *D10 methods and the alias*), its semantics are V1-6's over the
engine client's writability (the same optional interface), and the `writable` flag of
engine.io-client 3.5.6's polling transport, false while its POST is pending, is only the
model for that writability; the row closes by D9 and D10 (PARITY.md records it in C11's evidence). A volatile emit on a
socket that is not connected yet (before the CONNECT reply, or during a reconnection) is
dropped, where C9 buffers a plain emit: the connection cannot take it now. Named tests,
each under the `// Covers` of its row:
`TestClientReconnect` (C3: an in-process server closed and reopened on the same address;
`OnReconnectAttempt` and `OnReconnectError` while it is down, `OnReconnect` and the
sockets' `OnConnect` once it is back, with small delays; with `ReconnectionAttempts` 2
against a server that stays down, `OnReconnectFailed` after two attempts; with
`DisableReconnection`, no attempt; after a server-side `Disconnect`, no attempt),
`TestClientSocketState` (C8: `ID` in both forms, `Connected` before, during and after,
`On`, `Once`, `off()` and `Off`, `OnPing` and `OnPong` with a small `PingInterval`) and
`TestClientVolatile` (C11, the D10 extension above over the client-side rule of the V1-4
polling fence: over polling, a volatile emit made while a POST of the client is in
flight, the in-process test server holding that POST's response until the emit has
returned, is dropped, and one made with no POST in flight arrives; over websocket an idle
client's volatile emit arrives; a volatile emit before the CONNECT reply is dropped while
a plain emit made beside it is buffered and arrives). The conformance step against the Node `socket.io@2.5.0`
server (V1-8) runs green on the V1-11 head, the reconnect scenario included.

### V1-15 contract

Rows: E8 (`perMessageDeflate`), E9 (`httpCompression`), E10 (`cookie`), K17
(`compress` on a socket), S21 (`compress` on server broadcasts), C11 (`compress` on the
client). The semantics, defaults, the inflated-size rule of E4 and the Redis rule are
fixed in *Compression and cookies (V1-15)* above; this contract names the fields, the
methods and the plumbing. D10 tags: K17 and C11 carry it (`Compress` on `Namespace`).

```go
package engineio

import (
	"io"
	"net/http"

	"github.com/sshaplygin/go-socket.io/engineio/session"
)

// Options gains the compression and cookie fields; the defaults are engine.io 3.6.2's.
type Options struct {
	// PerMessageDeflate offers the permessage-deflate websocket extension. false, the
	// zero value, is the reference default. When on, a frame whose encoded packet is
	// shorter than CompressionThreshold, or whose packet carries compress false, is
	// written uncompressed.
	PerMessageDeflate bool

	// DisableHTTPCompression turns the gzip and deflate compression of polling
	// responses off; false is the reference default httpCompression: true. When on, a
	// response of CompressionThreshold bytes or more is compressed with the encoding
	// Accept-Encoding allows (gzip preferred), only when a packet of the response
	// carries compress true.
	DisableHTTPCompression bool

	// CompressionThreshold is the size, in bytes of the encoded packet (websocket) or
	// of the response body (polling), from which compression applies; 0 means 1024.
	CompressionThreshold int

	// Cookie is the session cookie set on every response of the polling transport
	// that made the handshake, with Value replaced by the session id. nil means the
	// reference default: Name "io", Path "/", HttpOnly true, SameSite Strict. A
	// non-nil cookie is written as given (Name "" means "io"), so the object form of
	// the reference's cookie (Domain, Expires, Secure, SameSite) is this field, and
	// the reference's rule that a cookie without a path is not HttpOnly is the
	// caller's choice of HttpOnly.
	Cookie *http.Cookie

	// DisableCookie sets no cookie (the reference's cookie: false).
	DisableCookie bool
}

// CompressWriter is implemented by a Conn that honours the per-packet compress flag:
// *session.Session and the engine client. NextWriter is NextWriterCompress(ft, true).
// An optional interface, not a Conn method, so mocks of Conn are unaffected; a Conn
// without it sends every packet with the default.
type CompressWriter interface {
	NextWriterCompress(ft session.FrameType, compress bool) (io.WriteCloser, error)
}
```

```go
package socketio

// Compress starts an Emitter that targets every socket of the namespace (S21), as To,
// Except, Volatile and Local do, whose packet carries the compress flag on; every other
// emit carries true, the socket.io 2.5.0 default. false sends the packet uncompressed on
// either transport. The single-socket form is Namespace.Compress below.
func (n *ServerNamespace) Compress(on bool) Emitter { return nil }

// BroadcastOptions gains the flag, kept apart from the Except options: the Redis
// Broadcast accepts it without ErrExceptUnsupported, honours it for this instance's
// sockets and does not carry it (V1-14 Known limitations; V1R carries it).
type BroadcastOptions struct {
	// NoCompress sends the packet uncompressed; the zero value is the default, true.
	// SendTo passes it as target.Compress(false) (its godoc, the V1-6 contract).
	NoCompress bool
}
```

V1-15 runs after V1-9, so `Emitter` and `Namespace` are declared in `client` and the root
sees the additions through its aliases (Stage 7, *D10 methods and the alias*):

```go
package client

// Emitter and Namespace gain Compress with the same meaning; on a client connection
// it works (C11). Namespace.Compress starts a single-socket Emitter, which a later To or
// Except switches to a broadcast from the socket with the flag kept (the Emitter godoc
// of the V1-6 contract): c.Compress(false).To(room) is c.To(room).Compress(false).
type Emitter interface {
	Compress(on bool) Emitter
}

type Namespace interface {
	Compress(on bool) Emitter
}
```

The transport-level additions, each in a file outside the SYNC manifest
(`engineio/transport/compress.go` is new; the `Transport` structs of the two transports
gain fields, their connections gain methods):

```go
package transport

import (
	"io"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// CompressWriter is implemented by a transport connection that honours the per-packet
// compress flag; NextWriter is NextWriterCompress(ft, pt, true). Optional: a connection
// without it ignores the flag.
type CompressWriter interface {
	NextWriterCompress(ft frame.Type, pt packet.Type, compress bool) (io.WriteCloser, error)
}
```

```go
package websocket

import (
	"io"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// Transport gains the compression fields; the existing fields stay. The zero values
// are off; engineio.NewServer copies Options.PerMessageDeflate and
// CompressionThreshold into them, client.Options.PerMessageDeflate sets
// EnableCompression on Dial with CompressionThreshold 1024.
type Transport struct {
	EnableCompression    bool
	CompressionThreshold int
}

// NextWriterCompress implements transport.CompressWriter: compress false writes the
// message with EnableWriteCompression(false).
func (c *conn) NextWriterCompress(ft frame.Type, pt packet.Type, compress bool) (io.WriteCloser, error) {
	return nil, nil
}
```

```go
package polling

import (
	"io"
	"net/http"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// Transport gains the compression and cookie fields; the existing fields stay. The zero
// values are off (Cookie nil: no cookie); engineio.NewServer copies
// DisableHTTPCompression (negated), CompressionThreshold and the effective cookie of
// Options (the default "io" cookie for a nil Options.Cookie, nil with DisableCookie).
type Transport struct {
	Compression          bool
	CompressionThreshold int
	Cookie               *http.Cookie
}

// NextWriterCompress implements transport.CompressWriter: the flag is kept per packet
// and the response is compressed when any packet of it carries true.
func (c *serverConn) NextWriterCompress(ft frame.Type, pt packet.Type, compress bool) (io.WriteCloser, error) {
	return nil, nil
}

// SetSessionID gives the connection the session id for the cookie's Value. session.New
// calls it through the optional interface interface{ SetSessionID(string) } before the
// handshake response, so the cookie is on that response.
func (c *serverConn) SetSessionID(id string) {}
```

```go
package session

import "io"

// NextWriterCompress implements engineio.CompressWriter for a server session: the
// writer of NextWriter with the compress flag passed to the transport connection's
// NextWriterCompress when it has one; NextWriter is NextWriterCompress(typ, true).
func (s *Session) NextWriterCompress(typ FrameType, compress bool) (io.WriteCloser, error) {
	return nil, nil
}
```

```go
package engineio

import (
	"io"

	"github.com/sshaplygin/go-socket.io/engineio/session"
)

// NextWriterCompress implements CompressWriter for the engine client returned by
// Dialer.Dial, as Session.NextWriterCompress does for a server session.
func (c *client) NextWriterCompress(typ session.FrameType, compress bool) (io.WriteCloser, error) {
	return nil, nil
}
```

```go
package client

// Options gains PerMessageDeflate; the fields of V1-10 and V1-11 stay.
type Options struct {
	// PerMessageDeflate offers the permessage-deflate extension on the websocket
	// transport, threshold 1024, as engine.io-client 3.5.6 does; false is off.
	PerMessageDeflate bool
}
```

Plumbing, so that no SYNC manifest file changes: the flag travels from `Emit` through
the root's packet queue, whose element becomes a root-local unexported struct of the
`parser.Payload` and the compress bool (`writeChan`, `connection.go:80,274-285`; `parser`
is unchanged, since package `socketio` cannot read an unexported field of a `parser`
type), and `serveWrite` (`server.go:359-377`) hands the flag of the packet it encodes to
the root's `queueWriter` (`connection.go:327-344`), whose `NextWriter` calls
`engineio.CompressWriter` with it, which
`*session.Session` and the engine client implement over the optional
`transport.CompressWriter` (`NextWriterCompress(ft frame.Type, pt packet.Type, compress
bool)`, declared in a new file `engineio/transport/compress.go`, outside the manifest)
that the websocket connection (`gorilla` `EnableWriteCompression(false)` for that
message) and the polling connection (the flag kept per packet in the payload, so the
response is compressed when any packet of it carries true) implement; a transport
without it ignores the flag. `engineio.NewServer` carries `PerMessageDeflate`,
`DisableHTTPCompression`, `CompressionThreshold` and the cookie to the two transports of
this module through the copy mechanism of *CORS default and option (V1-18)*: new exported
fields `websocket.Transport.EnableCompression` and `CompressionThreshold`,
`polling.Transport.Compression`, `CompressionThreshold` and `Cookie` (zero values off, the
server-level defaults are `Options`'s); the session id reaches the polling connection
through the optional `interface{ SetSessionID(string) }` that `session.New` calls on its
connection when implemented, before the handshake response, so the cookie is on it.
The inflated-size rule of E4: with the extension negotiated, the websocket reader counts
inflated bytes and closes with 1009 at `MaxHTTPBufferSize`, since gorilla's `SetReadLimit`
counts wire bytes (the paragraph above, `TestPerMessageDeflateLimit`). Client:
`client.Options.PerMessageDeflate bool` offers the extension with threshold 1024 as
engine.io-client 3.5.6 does, through `websocket.Transport.EnableCompression` on `Dial`;
the client has no read limit, as the reference client has none. Named tests: the seven
of *Compression and cookies (V1-15)*, each under the `// Covers` of its row
(`TestPerMessageDeflateNegotiated` and `TestPerMessageDeflateLimit` E8,
`TestPollingHTTPCompression` E9, `TestHandshakeCookie` E10, `TestEmitCompressFlag` K17,
`TestBroadcastCompressFlag` S21, `TestClientCompress` C11), and the two tagged conformance
tests, counted by the V1-15 block above.

### V1-16 contract

Rows: S18 (`engine`), E17 (`clients`), E20 (`readyState`, `upgraded`, `transport`;
`close(discard)` and `send` are `Close` and `NextWriter`). The semantics are fixed in
*Engine layer (V1-16, V1-17)* above; this contract names the declarations and the
implementers. D10 tag: E20 carries it (`engineio.Conn` methods).

```go
package session

// ReadyState is the state of a session, the readyState strings of engine.io 3.6.2.
type ReadyState string

const (
	// StateOpening: from New until the OPEN packet has been written.
	StateOpening ReadyState = "opening"
	// StateOpen: the OPEN packet is written and the session is not closing.
	StateOpen ReadyState = "open"
	// StateClosing: a close has started (Close, a close cause observed, Server.Close)
	// and the transport connection is not closed yet.
	StateClosing ReadyState = "closing"
	// StateClosed: the transport connection is closed.
	StateClosed ReadyState = "closed"
)

// ReadyState returns the state now; Upgraded reports a completed upgrade; Transport is
// the name of the current transport (already there, session.go:101).
func (s *Session) ReadyState() ReadyState { return StateClosed }
func (s *Session) Upgraded() bool         { return false }

// All returns a copy of the registered sessions keyed by session id, taken under the
// manager's lock (session_manager.go, a SYNC manifest file: v2 gets the same method).
// engineio.Server.Clients filters it by state.
func (m *Manager) All() map[string]*Session { return nil }
```

```go
package engineio

import "github.com/sshaplygin/go-socket.io/engineio/session"

// ReadyState and its four values are session's, re-exported.
type ReadyState = session.ReadyState

const (
	StateOpening = session.StateOpening
	StateOpen    = session.StateOpen
	StateClosing = session.StateClosing
	StateClosed  = session.StateClosed
)

// Conn gains the engine socket state (D10: implementers and mocks must add them).
type Conn interface {
	// ReadyState returns the state of the connection now.
	ReadyState() ReadyState
	// Transport returns the name of the current transport, "polling" or "websocket";
	// after an upgrade the new one.
	Transport() string
	// Upgraded reports whether an upgrade has completed.
	Upgraded() bool
}

// Clients returns a copy, taken under the server's lock, of the sessions whose
// ReadyState is StateOpen or StateClosing, keyed by session id: the membership of
// engine.io 3.6.2's clients (the rule is fixed in the Engine layer paragraph of the
// roadmap). Count keeps counting the registered sessions.
func (s *Server) Clients() map[string]Conn { return nil }
```

```go
package socketio

import "github.com/sshaplygin/go-socket.io/engineio"

// Engine returns the engine.io server this server runs on, the one NewServer created;
// never nil.
func (s *Server) Engine() *engineio.Server { return nil }
```

Implementers of the three `engineio.Conn` methods, updated in this PR: `*session.Session`
(`Transport` exists; `ReadyState` reads the `opened` and `closed` fields under
`upgradeLocker` and a new flag set when the transport connection's `Close` has
returned; `Upgraded` a flag set by `switchTransport`); the engine client (`engineio/
client.go`, `Transport` exists at `:50`; `ReadyState` `open` after `Dial`, `closed` after
`Close` or the ping-loop exit as the paragraph above fixes, never `opening` or
`closing`; `Upgraded` false until V1-10's upgrade switched the transport, true after);
the root test fake `*fakeConn` (`backpressure_test.go`), which gets real methods
(`open`, `"fake"`, false) instead of relying on its nil embedded `engineio.Conn`; and
through it `faultConn` (`log_test.go`). V1-14's `### Changed` entry names the three
methods. `Clients` adds `Manager.All() map[string]*Session` to `engineio/session/
session_manager.go`, a SYNC manifest file (D14: the same method is added to the `v2/`
copy, `v2/engineio/session/manager.go`, so the pair stays identical after the
import-path rewrite, with or without the SYNC PR merged; *Shared-file drift check* names
this PR beside V1-2). Goroutine rule: the three
state methods and `Clients` may be called from any goroutine; a hook of V1-17 may call
them on the session it observes (the one read-only exception to "must not call back").
Named tests, each under the `// Covers` of its row: `TestServerEngine` (S18),
`TestEngineClients` (E17), `TestEngineConnState` (E20) as the paragraph above describes,
and `TestEngineClientState` (E20 stacked: the engine client is `open` after `Dial`,
`closed` after `Close`, and `closed` after the ping loop has exited through the deferred
close of `serve()`, driven as `TestClientPingFailureRecord` of `engineio/
session_log_test.go` drives it).

### V1-17 contract

Row: E19 (the socket-level engine events), with the four server-level events D16 names
(`connection`, `initial_headers`, `headers`, `connection_error`), which have no row and
are counted by the V1-17 block above. The semantics of every hook, the goroutine rule
(synchronous on the goroutine of the event, a blocking hook delays that session only,
no call back into the session except the read-only state methods of V1-16) and the
order are fixed in *Engine layer (V1-16, V1-17)* above; this contract names the type.

```go
package engineio

import (
	"net/http"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// Hooks are the engine events of engine.io 3.6.2 as observer callbacks, the shape of
// v2/engineio/hooks.go without its contexts (D13, D16). A nil field is ignored; a set
// one is called synchronously on the goroutine of the event. Header values are
// borrowed for the call: a hook that keeps them copies them. Options.Hooks holds one;
// nil means no observer.
type Hooks struct {
	// Connection fires once per session, after its handshake response and before
	// Accept returns it; c.ReadyState() is StateOpen in the hook.
	Connection func(c Conn)
	// InitialHeaders fires once per session with the handshake request and the
	// headers of its response, before they are written; a hook may add to h.
	InitialHeaders func(r *http.Request, h http.Header)
	// Headers fires with every response of a session (each polling response, the
	// websocket upgrade response once), after InitialHeaders on the handshake, before
	// the headers are written; V1-15's cookie is in h.
	Headers func(r *http.Request, h http.Header)
	// ConnectionError fires when ServeHTTP rejects a request, with the engine.io 3.6.2
	// code and message (0 Transport unknown, 1 Session ID unknown, 2 Bad handshake
	// method, 3 Bad request, 4 Forbidden) and, for a request checker rejection, its
	// error.
	ConnectionError func(r *http.Request, code int, message string, err error)

	// Packet fires for every packet read from the client, PacketCreate for every
	// packet about to be written, each with the packet and frame types.
	Packet       func(c Conn, pt packet.Type, ft frame.Type)
	PacketCreate func(c Conn, pt packet.Type, ft frame.Type)
	// Heartbeat fires when a PING is received, before the PONG is written.
	Heartbeat func(c Conn)
	// Upgrading fires when the probe is accepted, Upgrade when the transport has
	// switched; transport is the name of the new one.
	Upgrading func(c Conn, transport string)
	Upgrade   func(c Conn, transport string)
	// Flush fires when a polling write batch starts, Drain when it has been written.
	Flush func(c Conn)
	Drain func(c Conn)
	// Error fires with the transport error that closes the session (V1-4's cause
	// transport error), before the close; a peer close or a ping timeout fires nothing.
	Error func(c Conn, err error)
}

// Options gains the observer.
type Options struct {
	Hooks *Hooks
}
```

Plumbing: the socket-level hooks fire inside `session.Session`, which cannot import
`engineio`, so `session` exports a `Hooks` struct of the same eight socket-level fields
over `*Session` (plumbing, not user API: `engineio.NewServer` builds it from
`Options.Hooks` by wrapping each set field, and the engine server installs it with
`SetHooks` between `session.New` and `InitSession`, so `session.New`, an exported function
of a public package, keeps its signature):

```go
package session

import (
	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// Hooks is the socket-level half of engineio.Hooks over *Session: engineio.NewServer
// wraps each set field of engineio.Options.Hooks into the field of the same name. A nil
// field is ignored. Plumbing for the engine server, not user API.
type Hooks struct {
	Packet       func(s *Session, pt packet.Type, ft frame.Type)
	PacketCreate func(s *Session, pt packet.Type, ft frame.Type)
	Heartbeat    func(s *Session)
	Upgrading    func(s *Session, transport string)
	Upgrade      func(s *Session, transport string)
	Flush        func(s *Session)
	Drain        func(s *Session)
	Error        func(s *Session, err error)
}

// SetHooks installs h; the engine server calls it after New and before InitSession,
// on the goroutine that created the session, so every event of the session is observed.
func (s *Session) SetHooks(h *Hooks) {}
```

The server-level hooks: `Connection` fires in `engineio.Server.newSession` after
`InitSession` and before the hand-off to `Accept`, on the session goroutine, so after
`Options.ConnInitor`, which V1-2 runs on the request goroutine before the `go` statement
that starts `InitSession`: the initor first, then `Connection`, and a hook that reads what
the initor set with `SetContext` sees it (the `connection` subtest of
`TestEngineServerEvents` asserts that order); `InitialHeaders` and `Headers` fire from the engine server and the session's
`ServeHTTP` (`engineio/server.go:104-184`, `session.go:342-350`) on the headers of the
`http.ResponseWriter` before the transport writes them, for the websocket upgrade before
`Transport.Accept` (gorilla writes `w.Header()` into the upgrade response,
`websocket/transport.go:88`), and for every polling response through a header-observing
`ResponseWriter` wrapper the session installs, so no transport file changes. The wrapper
fires the hook at the first `WriteHeader` or `Write` of the response, once the transport
has set its own headers, so for a polling response the hook runs on the goroutine that
writes the first byte, which is not the request goroutine for a GET: the headers go out
with the first `Write` of the session writer that `FlushOut` hands the response to
(`engineio/payload/payload.go:58-80`, `polling/server.go:107-112`), the session's
`InitSession` goroutine for the handshake and the root's write goroutine (`serveWrite`)
for every later poll; a POST answers on the request goroutine, and the websocket upgrade
fires before `Transport.Accept` on the request goroutine. A `Headers` hook that blocks on
a GET therefore stalls that connection's writer, the "delays that session only" rule of
the paragraph above. The `headers` subtest of `TestEngineServerEvents` asserts that the
hook has fired, with the transport's `Content-Type` in `h`, before the first byte of each
polling response reaches the client; `ConnectionError` fires beside the `reject` calls of `engineio/server.go:234-245`, with
the P11 codes V1-2 introduces. Named tests, each under the `// Covers` of its row:
`TestEngineSocketEvents` (E19) and `TestEngineServerEvents` with its four subtests, as
the paragraph above describes.

### V1-18 contract

Rows: S4 (the option that disables the default), with P8 (`CHG`, the headers on every
polling response). The default, the mechanism (the transport copies made by
`engineio.NewServer`, no file under `engineio/transport/` changed) and the tests are
fixed in *CORS default and option (V1-18)* above; this contract names the field.

```go
package engineio

// Options gains the one CORS switch.
type Options struct {
	// DisableCORS switches the server-level CORS default off. By default every origin
	// is accepted, as socket.io 2.5.0 with origins "*:*": a polling response carries
	// Access-Control-Allow-Origin with the request's Origin and
	// Access-Control-Allow-Credentials true, or Access-Control-Allow-Origin * without an
	// Origin header, and a cross-origin websocket upgrade is accepted. With the field
	// set, polling responses carry no CORS header and the websocket upgrade applies
	// gorilla's same-origin check, the v1.x behaviour with a nil CheckOrigin. A
	// transport whose CheckOrigin is set keeps it either way: it is the origins
	// function of the reference.
	DisableCORS bool
}
```

Named tests, under the `// Covers` of their rows: `TestCORSDefault` (P8) and
`TestCORSDisabled` (S4), as the paragraph above describes. V1-14's `### Changed` entry
names CORS and `DisableCORS`.

## Stage 2. Socket.IO protocol v5 over Engine.IO protocol v4 (tag `v2.0.0`)

Protocol deltas are listed in [PROTOCOL.md](PROTOCOL.md#planned-engineio-v4-and-socketio-v5).
Stage 2 lands in `v2/`, in the tree defined by stage 1b (paths relative to `v2/`). New code for the `Socket` model goes to
the root files `server.go`, `namespace.go`, `socket.go`, `packet_handlers.go`,
`event.go`, `options.go`, `errors.go` and `adapter.go`; the client goes to `client/`.

### Prepared components

An `_experiments/<name>/` directory is a standalone module with its own `go.mod`, never
imported by the root module and not listed in `go.work`; it never joins `./...`. This
rule is permanent (the Stage 1b freeze exempts such PRs only for the 1b window). The
command check of these clauses is the `_experiments stays standalone` line of the Stage
1b DoD. It is also the first step of `make experiments` (the `experiments` CI job,
described in `CLAUDE.md`), which owns the check; the 1b DoD line is a copy that ends with
M1b. The landing PRs of the two modules already on `master` (#44, #45) checked it by
hand. Otherwise `make experiments` runs Go checks only, so a landing PR also records
the Node oracle or script checks of its module that it ran by hand. A module lands in
`master` through a fresh-branch cherry-pick PR of its owned commits (transfer rules in
issue #2; #44, #45, #54, #55 and #53 landed this way and supersede draft PRs #8, #9, #1, #6 and #7); it is later
absorbed into live packages by the PR that lands its consumer, and that PR deletes the
experiment and its `Unreleased` CHANGELOG entry (today `adapter-rooms`; `adapter-wire` went
with the first 2.2 PR, which created `adapter/codec`, `sio5-codec` was absorbed into `parser/` by 2.3P, and
`eio4-websocket`, `ws-bench` and the `internal/eio4` entry went with the 2.1 WebSocket PR). M2 acceptance checks the deletions. A draft PR does not satisfy a gate.

| Component (source PR) | Purpose | Consumer | State in `master` and retirement |
| --- | --- | --- | --- |
| `_experiments/adapter-rooms` (#9, landed as #45) | Node memory-adapter room corpus | 2.2 conformance | landed, unreleased; absorbed into the 2.2 tests by the PR that adds them, which deletes it |

### 2.0 Generic API and lifecycle contract

The 2.0 owner atomically removes the legacy root server/client/namespace/handler
runtime, its v1-specific tests, the v1 memory and Redis broadcast (`broadcast.go`,
`redis_broadcast.go`, `adapter_options.go`, `helpers.go` and their tests, which stage 1b
leaves in the root), `Server.Adapter` and the redigo dependency while introducing the v2
skeleton. No `adapter/` directory exists until 2.2 creates `adapter/codec/`.
v1 stays at the repository root (*Repository layout*); carry applicable regression scenarios into v2 fixtures.
Engine.IO/parser packages remain buildable until their replacements in 2B. Legacy
application examples are excluded from v2 build jobs until migrated in 2.5D; Redis
examples return in 4b. This transition must build/test the entire root module before G2.

Implement the v2 generic API skeleton on Go 1.22 before runtime dispatch.
Positive compile fixtures cover server and client registration,
emit, ack, room broadcast, `Args2`, binary data, and the creating call with `ctx` and an
`AdapterFactory` taking `ctx` (2.2 *Readiness*); negative fixtures must reject a
wrong handler argument, ack return type or emitted payload. Keep these fixtures in
CI. Freeze the shared types consumed by parallel work: `Endpoint`, client registration
interface, `Options`, packet/argument codecs, `Adapter`, `AdapterFactory`, the namespace-creating call (2.2
*Readiness*), both hook structs and result enums. The method-signature inventory and the
acyclic package graph are published in [API.md](API.md); `make graph` checks the graph and
`go test` the fixtures. No placeholder
`any` handler, unresolved signature or TODO in these interfaces passes G2 (`make freeze`
checks it, see the *G2 record*). Runtime
work is assigned to 2.1–2.4; the skeleton contains no claimed runtime implementation.

`Event[T].Handle` registers `func(context.Context, *Socket, T) error` on a namespace;
`AckEvent[T, R].Handle` registers `func(context.Context, *Socket, T) (R, error)`.
Both return a registration error. Client descriptors offer `HandleClient` with the
shared `Endpoint` interface in place of `*Socket`. `Endpoint` lives in the root,
is implemented by server sockets and the Go client, and supplies packet send/ack
operations without importing `client/` into the root. Public methods keep the type
relationship on the generic descriptor, not on an untyped namespace method.

Lifecycle contract, implemented in 2.1/2.3 and instrumented in 2.4:

- The handshake uses `r.Context()` until acceptance/rejection. An accepted session
  gets its own cancellable context derived from `context.WithoutCancel` of the
  handshake context, preserving values but not the HTTP deadline/cancellation.
  Session close/server shutdown cancel it; namespace sockets derive cancellable
  children. Returning from polling/upgrade `ServeHTTP` must not cancel the session.
- One reader per Engine.IO session handles protocol control and ack completion
  independently of user handlers. A bounded worker queue per namespace socket runs
  user events sequentially in receive order; namespace auth/middleware also run off
  the reader with a bounded connect deadline. A blocked handler cannot stop heartbeat
  or ack processing; `EmitWithAck` inside a handler is supported. No ordering is
  promised between different sockets or concurrent external emitters; binary packet
  headers and attachments are queued/written as one indivisible message group.
- Resolve each pending ack exactly once on reply, caller cancellation, timeout,
  enqueue failure or disconnect. Use the earlier of the caller deadline and
  `AckTimeout`; ignore late/duplicate ack IDs. Allocate monotonically increasing IDs
  without reuse for the entire Engine.IO connection lifetime, including namespace
  reconnects; never exceed JavaScript's safe integer maximum (`2^53-1`). Exhaustion
  rejects new ack requests with `ErrAckIDExhausted` until a fresh transport session.
  Handler panics are not recovered by the library; document this application contract.
- `Shutdown(ctx)` rejects new handshakes/events, cancels socket/handler contexts and
  pending acks, drains already queued outbound messages until the deadline, then
  closes transports and owned adapter workers. It is idempotent; `Close()` aborts
  immediately. A socket or session close requested by the application drains the
  session's queued outbound messages within the session ping timeout, as v1
  `Conn.Close` does since 1.B; closes started inside the library discard, as in 1.B.
  A peer namespace DISCONNECT ends only that socket and keeps the session queue, as in 1.B.
  The DoD below includes a disconnect-drain test; the v1 to v2 mapping is in 2.5.
  Application handlers must honor cancellation: Go cannot forcibly
  terminate them, and shutdown returns on its deadline even if one does not exit.
  Injected broker clients, loggers and OTel providers remain application-owned.

DoD: compile fixtures pass on Go 1.22; runtime stages add tests for request-context
independence, handler-initiated ack, ordering, concurrent close/ack/timeout, bounded
queues, graceful/forced shutdown and an application close that drains within the ping
timeout while a library close discards, all under `-race`. Include timeout of A, new
request B and late ACK A: B must remain pending until its own terminal condition.

**G2 record.** The 2A owner reviewed every open item of the skeleton. Each is either
settled, with the declaration, the fixtures and [API.md](API.md) changed together, or moved
out of the freeze to the stage that defines it; a moved item is an addition and changes no
frozen declaration. API.md (*Frozen contract*) lists the result; this table owns the reasons.

| Item | Decision | Reason | Left to |
| --- | --- | --- | --- |
| `Adapter`, `AdapterFactory`, `BroadcastOptions`, both hook structs | settled as declared (2.2, 2.4) | 2B consumers build on them; they match the roadmap text line for line | none |
| `RemoteSocket` | settled: four fields as Node's `fetchSockets`; `Handshake` omits exactly `auth` and the `authorization`, `cookie` and `proxy-authorization` headers and keeps every other key, header, `url` and `query` as Node does (they may carry credentials); `Data` nil or valid JSON, no binary | a snapshot crosses node boundaries and reaches application code; Node's own snapshot carries the three headers and `auth`, so a decoding adapter drops them too. The rest is not redacted because it is Node's own data that applications read back, a value filter would guess, and dropping more keys later changes observable snapshots; the limit is documented in the godoc and API.md | producer API for `Data` (2.3S); shared helper `socketio.RedactHandshake` (declared by 2A, written by 2.2) and conformance case (2.2, 4b) |
| `BroadcastFlags` | settled: `Local` only | volatile and compress need the queue and codec design, timeout belongs to broadcast acks, which the contract does not have; a new field is additive for keyed literals | later flags, additive |
| `Options` | settled: field names, budget names and the defaults of 2.3 *Resource limits* | the names were fixed by 2.3 and the tests pin the defaults | none |
| Payload preview redaction | moved out: the redactor type and `engineio.Options.PayloadRedactor` removed from the skeleton | the boundary hands unredacted packet bytes to a trusted component; its method set follows from the fire points and the Socket.IO classifier, which 2.4E and 2.4S own; `PayloadPreviewBytes` and `PacketInfo.Preview` stay because 2.4 names them | 2.4E, 2.4S |
| Codec contract | settled minimum: `parser.Packet`, `Arguments`, `BinaryValue` and `ArgumentCodec[T]` as value forms; values passed to a callee are borrowed, returned values owned | 2.1 relies on no `parser` type (`engineio` never imports it); 2.3P and `adapter/codec` need the value forms and nothing else | stream encoder and decoder, placeholder validation (2.3P); descriptor binding (2.3S) |
| `SocketID = Room` | settled: alias | every socket is in the room named by its ID; a defined type would need conversions in `To` and `Except` | none; it cannot be undone without a breaking change |
| `Endpoint.RequestAck` | settled: returns the raw `parser.Arguments` | the ack of Node is positional arguments; the error-first convention is applied by `AckEvent` above the endpoint | typed decoding (2.3S) |
| Result and reason domains | moved out: upgrade results, request-rejection reasons, disconnect reasons, adapter results | the roadmap does not enumerate them and the hook fields are strings; a constant added later is additive | 2.4 (`docs/OBSERVABILITY.md`) |
| `ChainHooks`, `LoggingHooks` | settled: signatures declared in both packages as in 2.4, returning nil | the signatures are fixed by 2.4; the behaviour needs the fire points | 2.4E, 2.4S |
| `Server.ServeHTTP` | settled: declared, the skeleton answers 501 | the signature is `http.Handler`, asserted by 2.1 | 2.1, 2.3S |
| Lifecycle context of `Socket` and `Namespace`, disconnect, connection callbacks, `Socket.Data`, descriptor codec binding | moved out | they need the runtime design; each is an added method | 2.3S |
| Namespace API for external adapters (local sockets, local delivery of a received broadcast) | settled: `LocalSockets` (`Deliver`, `Snapshot`) in `adapter.go` and `Namespace.LocalSockets()` in `namespace.go`, with a fixture and a negative fixture | the memory adapter is its first user and 2B writes it in parallel with the runtime of 2C, so the seam must exist before either starts; a broker adapter calls the same two methods for a received broadcast. 2A owns both declarations; 2.2 never edits `namespace.go`; 2.3S fills the body and may not change the signatures | body: 2.3S (2C) |
| Received `ServerSideEmit` (delivery to application handlers, acks of server-side emits) | moved out | no memory-adapter or 2B consumer: a single node has no peer to receive from | 4b, additively, with 2.3S |

The gate evidence is `make g2`: `make graph` (package graph), `make freeze` (`TestFrozenContract`:
none of `TODO`, `FIXME`, `proposed`, `unreviewed`, `not yet frozen`, `open before G2` or a
placeholder `any`/handler wording in API.md or in the comments of the frozen files, and no
bare `any` or `interface{}` in an exported function, method, func type, func-typed field, interface method
or struct field of the frozen files, other than the variadic payload of `ServerSideEmit`; `TestAnyUsesDetects` proves the
check fails on each of those shapes) and `go test -run
'^(TestCompileContracts|TestInventory.*)$' .`, which compiles the
positive fixtures and the negative fixtures with their recorded diagnostics, and checks that
API.md lists every exported declaration with its declared result types. CI runs `make
graph` and `make freeze` in the `lint` job and the fixtures in `go test ./...`, so `min-go`
runs them on Go 1.22 with `GOTOOLCHAIN=local`.

### 2.1 Engine.IO v4 and gobwas/ws

Entry: G2. Exit: Engine.IO suite, transport/client tests and idle-connection baseline.

Reuse the prepared work that landed in `master` (#54, from the baseline's
`codex/eio4-payload` revision). The 2.1 owner integrates it after G2, preserving tests
and updating imports to the v2 layout; do not repeat completed codec work.

| Prepared component | Evidence | Remaining 2.1 integration |
| --- | --- | --- |
| `engineio/payload` (moved from `internal/eio4`) | polling codec, bounded `DecodeReader`, exact-wire `EncodeBatch`, fixtures/fuzz tests and pinned Node oracles | integrated by the polling transport (PR B): POST limits and status mapping, client batching, cancellation and deadlines, pause/upgrade lifecycle. Open: the open packet does not advertise `maxPayload` and the `EIO=4` check is absent until the session and server work (PRs D1/D2) |
| `engineio/transport/websocket` (moved from `internal/eio4` and `_experiments/eio4-websocket`) | complete-message EIO4 codec with binary/text fixtures and parser oracle; gobwas framing (masking, fragments, UTF-8, control frames, message limits, close statuses) with a pinned `ws` peer oracle | integrated by the WebSocket transport (PR C): `FrameReader`/`FrameWriter` over the codec, `ws.Dialer`, deadlines and buffer options kept. Open: EIO handshake, heartbeat and upgrade lifecycle (PRs D1/D2); the `EIO=4` check; limits and timeouts as options (PR E) |

Preparation policy remains explicit: polling reads are byte-bounded before
buffering, then decoded as a complete batch without an additional packet-count cap.
Decoded heap can exceed wire bytes; retain `BenchmarkDecode/dense-records` and
measure amplification. Incremental decoding is deferred, not an integration gate.
Keep canonical base64/UTF-8 validation and fixtures for intentional differences from
Node: exact base64 batching, oversized-first-packet rejection and no partial decode
on invalid batches. The advertised `maxPayload` limits client POSTs, not server responses; the Go client reads a response up to `Transport.MaxPayload` and fails the session above it (docs/PROTOCOL.md). Reuse
locked reference versions, recording changes when refreshed; these oracles establish
component behaviour, not full Go-server conformance. The Node oracles are outside the
required `go test ./...` set: they run by hand (`engineio/payload/testdata/README.md`,
`engineio/transport/websocket/testdata/README.md`), and `TestNodeOracle` runs the `ws`
peer against the WebSocket transport when its dependencies are installed.

- Replace the legacy `engineio/payload`/`pauser` transport integration using these
  codecs; complete the session lifecycle work rather than rewriting codecs again.
- `engineio/session`: server ping ticker and `pingTimeout` to await each pong. Add
  `maxPayload` and noop on upgrade. `engineio/client` (`client.go`, `dialer.go` since
  1b): the client uses `pingInterval+pingTimeout` to detect a missing server ping, and
  `dialer.go` sends `EIO=4` instead of the fixed `EIO=3`. `engineio/server.go`: `EIO` check, JSON errors.
  The polling GET 500 and invalid-method 400 answers, unlogged in v1, log
  `engineio: request rejected` with reasons `flush` and `bad method`; `flush` logs
  DEBUG when the session or its payload has already closed or failed, WARN otherwise.
  `docs/OBSERVABILITY.md` (2.4) owns the full v2 `reason` list.
- `engineio/transport/websocket` rewritten on `gobwas/ws`: `ws.UpgradeHTTP` hijacks the
  connection (HTTP/1.1 only); frames read with `wsutil.Reader` and written with
  `wsutil.Writer` so the `FrameReader`/`FrameWriter` contract is preserved; control
  frames handled by `wsutil.ControlFrameHandler`; one write mutex per connection;
  `CheckOrigin`, `ReadBufferSize`, `WriteBufferSize` options kept. The client
  (`engineio/client`) uses `ws.Dialer`. `gorilla/websocket` removed from `go.mod`. Decisions: `permessage-deflate` is not
  negotiated; `Transport.Proxy` is kept as an `http` CONNECT wrapper around `NetDial` (other proxy
  schemes fail the dial); `Transport.MaxPayload` (default 1 MiB, as polling) bounds one message,
  fragments together, until the limits options of PR E; the connection's `ServeHTTP` returns at
  once, because the upgrade hijacked the connection. Both `engineio.Server` and
  `socketio.Server` assert `var _ http.Handler`; a wrapped `ResponseWriter` without
  `http.Hijacker` is answered with HTTP 501 and an `engineio: request rejected` line with
  `reason="no hijacker"`, never a panic.
- CI job runs `socketio/engine.io-protocol/test-suite` (Node) against the Go server.
- `BenchmarkIdleConnections` (10k websocket connections, RSS and goroutines) recorded
  in `CHANGELOG.md` before and after the swap (both recorded there, with the commands, from
  `engineio/idle_bench_test.go`).
- Bound decoded message size across polling and fragmented websocket frames, not
  just individual frame size. Initial configurable defaults: message limit 1 MiB,
  handshake and upgrade timeout 10 s each, write timeout 10 s. Preserve protocol
  control processing during handler load. Fuzz malformed payloads, frame boundaries
  and upgrade/close sequences; keep reproducing inputs as regression fixtures.

### 2.2 Adapter interface and memory implementation

Entry: G2. Exit: memory-adapter conformance and dependency-graph checks.

```go
type Adapter interface {
    AddAll(sid SocketID, rooms []Room)
    Del(sid SocketID, room Room)
    DelAll(sid SocketID)
    Broadcast(ctx context.Context, pkt parser.Packet, opts BroadcastOptions) (BroadcastResult, error)
    Sockets(ctx context.Context, rooms []Room) ([]SocketID, error)
    SocketRooms(sid SocketID) []Room
    FetchSockets(ctx context.Context, opts BroadcastOptions) ([]RemoteSocket, error)
    ServerSideEmit(ctx context.Context, event string, args ...any) error
    Close() error
}
type BroadcastOptions struct{ Rooms, Except []Room; Flags BroadcastFlags }
type BroadcastResult struct{ LocalRecipients int; Published bool }
type AdapterFactory func(ctx context.Context, nsp *Namespace) (Adapter, error)
func (s *Server) Namespace(ctx context.Context, name string) (*Namespace, error) // creating call
```

`Adapter`, related types and the in-memory implementation live in root `socketio`;
`adapter/codec` (created here; its first slice, the Node Redis adapter message format over
codec-local wire types, has landed) depends on parser/wire types, never on root `socketio`. External
adapters import the root; the root never imports them. This avoids a cycle through
the `*Namespace` parameter of `AdapterFactory`. The memory adapter is the v2 default; legacy removal belongs to 2.0. The v2.0
release and its example build job require only the memory adapter.

`Broadcast` success means local recipients were queued and, for a non-local cluster
broadcast, publication was accepted by the broker client. It does not mean remote
delivery or client ack. `LocalRecipients` counts successful local enqueues after
room union/deduplication and exclusions; `Published` records broker acceptance, not
remote recipient count. On partial enqueue/publish failure return the partial result
and an error; do not retry implicitly and risk duplicate delivery. `Local` never
publishes. Memory and broker adapters share these rules. Room operations are local;
`Sockets` is cluster-wide and `SocketRooms` is local. Empty room filters select all.
`FetchSockets`/`Sockets` may return deduplicated partial data plus a deadline error;
zero peers must not be confused with a proven empty cluster. `ServerSideEmit` accepts
publication without implying execution on peers. Order is per producer on a live
connection, not global across nodes; disconnect gaps have no replay guarantee.
`Close` releases adapter-owned subscriptions/workers, never injected broker clients.
Conformance tests cover these semantics and concurrent join/leave/broadcast.

*Snapshots and flags.* `RemoteSocket` follows Node's `fetchSockets` entry (`ID`, `Rooms`,
`Handshake`, `Data`). `Handshake` is a JSON object with the Node key names. The guarantee is
exactly this: it has no `auth` key and its `headers` omit `authorization`, `cookie` and
`proxy-authorization` (compared without case). Nothing else is redacted: `url`, `query`,
`address` and every other header (`set-cookie`, `x-api-key`, `x-auth-token`) pass through
and may contain credentials, for example a token in `query`. They are Node's own fields
that applications read back; a filter on values would guess, and omitting more keys later
changes observable snapshots, so it is a contract change. An application keeps secrets out
of the URL or removes them after `FetchSockets`. One helper does the omission,
`socketio.RedactHandshake(json.RawMessage) (json.RawMessage, error)`, declared in
`adapter.go` by 2A (the skeleton returns `ErrNotImplemented`) and written by this stage. It
lives in the root because the root may not import `adapter/...` while every adapter
already imports the root, so one location is reachable by all callers under the frozen
graph: the root calls it for local sockets (2.3S, in `Namespace.LocalSockets().Snapshot`) and
each adapter package calls it on a snapshot decoded from a peer, because a Node peer sends
the omitted values. `adapter/codec` decodes the wire fields only and does not call it,
since it never imports the root (`TestForbiddenEdge` pins both `. -> adapter/codec` and
`adapter/codec -> .` as forbidden, and `adapter/<name> -> .` as allowed; the
`externaladapter` fixture calls the helper from outside the root). The conformance suite (memory adapter
here, `adaptertest` in 4b for every broker adapter) decodes a peer snapshot that carries
`auth`, the three headers in mixed case, `x-api-key` and a `query` token, and asserts that
the first two groups are gone and the other two are unchanged. `Data` is the slot
of `socket.data`: nil, or valid JSON; binary values are not representable, and a producer
that cannot encode it returns the entries it can with an error. `BroadcastFlags` is `{Local bool}`; volatile, compress and timeout are later
additive fields. The adapter-facing seam is `LocalSockets`, frozen at G2: `Deliver(ctx, sid,
pkt)` queues a packet on one local socket (success counts in `LocalRecipients`) and
`Snapshot(sid)` returns its `RemoteSocket`; a namespace hands it out through
`Namespace.LocalSockets()`. Ownership: 2A declared both (`adapter.go`, `namespace.go`), this
stage writes the memory adapter in `adapter.go` and never edits `namespace.go`, 2.3S (2C)
implements the accessor body. Before the runtime exists the memory adapter is built from a
`LocalSockets` value, not from a `*Namespace`, and its conformance tests (room union,
exclusions, `LocalRecipients`, partial failure, the redacted `Snapshot` use) run against a
test double; the default factory only wraps `nsp.LocalSockets()`, so no 2B test needs a
socket. Sockets and snapshots that reach `Deliver` and `Snapshot` through a live
`Namespace` are covered by the 2C tests.

*Readiness.* These rules close the v1 Redis limitations recorded in 1.R and, from
`v1.5.0`, in the `CHANGELOG.md` *Known limitations*. They change two G2 signature lines:
`AdapterFactory` gains `ctx`, and the namespace-creating call is `Namespace(ctx, name)`
returning an error. The 2.0 skeleton declares both lines ([API.md](API.md)); the
experiment that held the earlier `Namespace(string) *Namespace` and `AdapterFactory
func(*Namespace)` was deleted by that PR. `Options.Adapter` carries the factory (default: the memory adapter). `NewServer`
creates no namespace, `/` included; a CONNECT to one the application has not created is
unknown. In tests *at once* means within 100 ms. 2.3S implements the server side and
owns its root test; 4b reproduces the broker cases.

- *Creation:* a namespace is created only by `Server.Namespace` and registered only when
  the factory returned it. A CONNECT never creates one: a CONNECT to a namespace that is
  not registered, including one being created, is rejected as unknown; dynamic
  namespaces (mentioned in 2.4) have no owner stage yet, and the stage that adds them defines how a
  CONNECT waits. One call decides in this order: (1) once shutdown has begun it returns
  an error matching `ErrNamespaceClosed`, also for a registered namespace, and calls no
  factory; (2) a registered namespace is returned without calling the factory; (3) a
  creation in progress is waited for, and its namespace or error returned; (4) otherwise
  the server calls `AdapterFactory` outside every lock that packet dispatch reads. Steps
  (1) and (2) ignore `ctx`; a `ctx` already done at (3) or (4) returns its error at
  once, and at (4) calls no factory. An error returned after shutdown began also matches
  `ctx.Err()` when the caller's `ctx` had ended. On a factory error nothing is
  registered, the call returns the error wrapped with `%w` and naming the namespace, and
  a later call calls the factory again. The factory context is the server's. The call's
  `ctx` bounds only its caller's wait: when it ends first, the call returns an error
  matching `ctx.Err()` and the creation goes on. A creation outlives its callers: when
  none is left and shutdown has not begun, a successful result is still registered (the
  next call returns it by step 2) and is closed by `Shutdown` or `Close` like any other,
  so an abandoned creation leaks nothing. A broker adapter returns only after the broker
  it is connected to confirmed its subscriptions, or with an error within a bound it
  documents as an option, leaving nothing of its own open. That is all the confirmation
  proves: one Redis master covers every peer, but a NATS flush does not show that other
  servers of a cluster, gateway or leafnode link have the interest, so a peer's
  broadcast right after construction can be missed there (4b `adapters/nats`).
- *Shutdown:* `Shutdown` and `Close` cancel the factory context when they begin;
  `Shutdown` waits for factory calls in progress, callers or not, until its deadline,
  `Close` does not wait. The server decides between registering and closing a returned
  adapter under the lock that sets the shutdown flag. A creation whose factory returns
  after either has begun returns an error
  matching `ErrNamespaceClosed` whatever the factory returns: the server closes a
  returned adapter exactly once (before the call returns when a caller waits) and wraps
  a factory error alongside with a second `%w`. The cancellation also reaches built
  adapters while the drain still broadcasts, so the factory context bounds only the
  factory call: an adapter derives no lifetime from it, stops any `context.AfterFunc` on
  it before returning, and lives until `Adapter.Close`.
- *Restoring:* a broker adapter is restoring from the moment it observes the loss of
  a subscription (a receive or connection error) until the broker confirms the new
  one; before it observes the loss, queries can undercount without an error, an
  accepted gap like the disconnect gaps above.
- *Queries:* cluster queries (`Sockets`, `FetchSockets`) count local sockets locally, never through the broker,
  and wait only for the peers expected to answer. With none expected they return the
  local data and a nil error at once; while restoring, or when the expected peers
  cannot be determined, the local data and an error at once.

### 2.3 Socket.IO v5 and the generic API

Parser work starts at G2; server/client runtime starts after 2B. Exit: typed
Go/Node interoperability, 2.0 lifecycle tests and dispatch benchmark baseline.

- `parser`: CONNECT payload, CONNECT_ERROR object, marker interface instead of
  `Type().Name()=="Buffer"`, `Packet` value type with lazily decoded args. The 2.0
  skeleton froze the value forms `Packet`, `Arguments`, `BinaryValue` and
  `ArgumentCodec[T]`; 2.3P added the bounded `Encode`, `Decode` and `Assembler`,
  placeholder validation, wire errors and the `JSON[T]` argument codec beside them, and
  2.3S binds a codec to a descriptor, both without changing the frozen declarations. A
  value passed to a callee is borrowed for the call; a returned value is owned by the
  caller. 2.3P is on `master` and unreleased (the M3 tags release it); the root calls none of it
  yet. What 2.3S still does with it: map `socketio.Options` to `parser.Limits`
  (`MaxEventBytes`, `MaxAttachments`, `AttachmentTimeout`), arm a timer on
  `Assembler.Deadline`, translate the parser errors into `ErrMessageTooLarge`,
  `ErrTooManyAttachments` and the `parse error` close, allocate acknowledgement IDs, and
  build `Args2` and the typed ack convention from `JSON[T]`, `Concat` and `Slice`.
- New model `Server → Namespace → Socket` replacing `conn`/`namespaceConn`. Explicit
  CONNECT for `/`. Each `Socket` owns a `context.Context` cancelled on disconnect.
- Generics-first public API; reflection-based `OnEvent(string, interface{})` is
  removed:

```go
var Message = socketio.NewEvent[ChatMessage]("message")
var Send    = socketio.NewAckEvent[ChatMessage, Receipt]("send")

Message.Handle(nsp, func(ctx context.Context, s *socketio.Socket, m ChatMessage) error { ... })
Send.Handle(nsp, func(ctx context.Context, s *socketio.Socket, m ChatMessage) (Receipt, error) { ... })

Message.Emit(ctx, s, ChatMessage{...}) // error; s implements Endpoint
r, err := Send.EmitWithAck(ctx, s, ChatMessage{...})
result, err := Message.EmitTo(ctx, nsp.To("room").Except(s.ID()), ChatMessage{...})

nsp.Use(socketio.Auth[Credentials](func(ctx context.Context, s *socketio.Socket, c Credentials) error { ... }))
nsp.OnRaw(func(ctx context.Context, s *socketio.Socket, e socketio.RawEvent) error { ... })
```

- Handler errors are returned, not panicked; `Handle` returns an error on duplicate
  registration; sentinel errors (`ErrNamespaceClosed`, `ErrAckTimeout`,
  `ErrWriteBufferFull`, `ErrSocketClosed`) work with `errors.Is`. `ErrAckTimeout` is
  driven by `socketio.Options.AckTimeout` (default 30 s); every pending ack ends on
  disconnect with `ErrSocketClosed`, so each emit-with-ack has exactly one outcome.
  Caller cancellation preserves `context.Canceled`/`context.DeadlineExceeded` through
  `errors.Is`; the library's own timer returns `ErrAckTimeout`.
- **Wire contract.** Ordinary `T` (including a struct or slice) is one positional
  JSON argument. `Args2[A, B]` expands to exactly two positional arguments; it is not
  one JSON array. Descriptors carry typed encode/decode functions; dispatch itself
  never reflects over handlers. `socketio.Binary` uses standard binary placeholders
  and attachments, including nested fields, slices and maps handled by `parser`.
  Encode/copy payload and binary bytes before `Emit` returns, so later application
  mutation cannot change a queued message. The raw escape hatch exposes positional
  `json.RawMessage` args, attachments and an explicit raw ack responder without
  imposing a typed schema; it supports standard Node/admin event contracts.
- **Typed ack errors.** `AckEvent` uses a documented application-level error-first
  convention: success `[null, ...resultArgs]`, failure `[{"code": "...", "message":
  "..."}]`. This is ordinary Socket.IO ACK data, not a new protocol packet. Node
  callbacks use `(err, result)` (or multiple result arguments for `Args2`). Raw ack
  handlers can interoperate with other application conventions. Known public errors
  have explicit codes; unexpected handler errors expose `internal_error` with a
  generic message and log the original error. Unknown events without a raw handler
  and typed decode failures produce `unknown_event`/`invalid_payload` when an ack
  was requested, otherwise only the error handler, if 2.0 defines one, or the log;
  malformed protocol envelopes
  close with `parse error`. For valid known events, server and client use this table:

  | Descriptor | Incoming ACK ID | Handler and response |
  | --- | --- | --- |
  | `Event[T]` | absent | run handler; errors go to the error handler, if 2.0 defines one, or the log |
  | `Event[T]` | present | run handler; reply `[null]` on success or the typed error envelope |
  | `AckEvent[T, R]` | absent | run handler, discard result; errors go to the error handler, if 2.0 defines one, or the log |
  | `AckEvent[T, R]` | present | run handler; reply using the typed ack convention above |

  Tests cover all four combinations with success/error, both Go/Node directions and
  binary ACKs; responses are queued at most once and only when an ACK ID is present.
- **Resource limits.** `socketio.Options` implements separate count and byte budgets:
  outbound queue 64 complete message groups / 8 MiB, handler queue 64 events / 8 MiB,
  128 pending acks per socket, at most 64 binary attachments and 1 MiB reconstructed
  event size, with 10 s attachment assembly timeout. All are configurable; queued
  attachments count toward byte budgets. Per Engine.IO session, allow at most four
  concurrent namespace CONNECT/auth tasks, no waiting queue and no concurrent CONNECT
  for the same namespace. Reject excess/duplicate attempts with CONNECT_ERROR without
  starting middleware. Each task has a 10 s deadline; retain its slot until middleware
  actually returns, even after cancellation, so non-cooperative handlers cannot cause
  unbounded task creation. Test ping/ACK progress during auth saturation.
  Pending-ack saturation rejects the new
  emit with `ErrTooManyPendingAcks`; queue overflow closes the affected session (outbound)
  or namespace socket (handler queue), completes pending operations and reports the
  reason once. Outbound limits apply across all namespaces sharing a transport.
  Add fuzz/regression tests for invalid placeholders, incomplete binary messages,
  argument arity and limits; verify memory remains bounded with a stalled peer.
  Returned errors distinguish oversized messages, too many attachments and queue
  saturation. Document every limit's option name, unit, default and zero-value
  semantics in the 2.0 API fixture; zero selects the bounded default, not unlimited. The
  option names are `OutboundQueueGroups`, `OutboundQueueBytes`, `HandlerQueueEvents`,
  `HandlerQueueBytes`, `MaxPendingAcks`, `MaxAttachments`, `MaxEventBytes`,
  `AttachmentTimeout`, `MaxConcurrentConnects` and `ConnectTimeout`, with `AckTimeout`
  above; G2 froze them.
- `BenchmarkEventDispatch` (root) is added with the new model, so stage 2.4 has a real
  baseline.
- Rewrite `Client` on the same generic API with websocket over `gobwas/ws`. The v1 line gets its own `client` package in Stage 7, executed inside Stage V1 (PR V1-9); this item delivers only the v2 one.
- 2.3S implements the server side of 2.2 *Readiness*. Its root test is
  `TestNamespaceReadiness`, with the subtests `R1` to `R9` below; the 2C join gate runs
  `go test -race -count=1 -json -run '^TestNamespaceReadiness$' .` and requires a pass
  event for each, none skipped. Each subtest (each variant of R2) builds its own server.
  The fake factory counts its calls, records its context and the time of each return, and
  returns a fake adapter that counts its `Close` calls. The *settled reading* is taken
  100 ms after the later of the factory's last return and the last `Close` or `Shutdown`
  return of the subtest; at it every adapter the factory returned has been closed exactly
  once, registered or not. A subtest states any earlier reading.
  - R1: a factory held until its context ends, then returning an adapter at once: `Close`
    during the call returns at once and the call's error matches `ErrNamespaceClosed`; the
    count is 1 when the call returns. A variant whose factory returns `ctx.Err()` instead
    returns an error that also matches `context.Canceled`.
  - R2: a factory returning an adapter 300 ms after its context ends, in two variants
    (the caller waits; every caller's `ctx` cancelled first), each ending the server in
    three runs of its own: `Shutdown` with a 1 s deadline does not return before the
    factory does, with a 20 ms deadline it returns before it, and `Close` returns before
    it. With the caller waiting, its call returns `ErrNamespaceClosed` only after the
    factory returned, and the count is then 1. With every caller gone, each call has
    returned an error matching its `ctx.Err()` at once, and the count read 100 ms after
    the end began is 0; the settled reading is 1.
  - R3: while `Shutdown` drains (a handler blocks it) and after `Close`, calls for a
    registered namespace, for an unregistered one and for one whose creation was started
    before and is still held (the test releases that factory last) return
    `ErrNamespaceClosed` at once with no further factory call; after `Close` a cancelled
    `ctx` also matches `context.Canceled`.
  - R4: a factory error comes back matching the error and naming the namespace; the next
    call calls the factory again.
  - R5: a done `ctx` returns the registered namespace; for an unregistered one it returns
    its error without calling the factory; while a creation is held, an already-cancelled
    `ctx` returns an error matching `context.Canceled` at once, the creation goes on and a
    later call with a live `ctx` returns its namespace after one factory call.
  - R6: eight concurrent creations of one namespace under `-race`, the factory held for
    100 ms, call it once and get the same namespace; the count is 0 before the test's
    `Close`.
  - R7: on a fresh server with no creating call, the factory count is 0 after `NewServer`
    and a CONNECT to `/` and to a never-created name is answered CONNECT_ERROR at once with
    the count still 0. While a creation is held, a CONNECT to its namespace is answered
    CONNECT_ERROR at once and calls no factory.
  - R8: with every caller's `ctx` cancelled while the factory is held, each call returns
    an error matching its `ctx.Err()` at once and the factory's context stays live; the
    factory then returns an adapter, the next call returns that namespace without a
    second factory call, and the count is 0 before the test's `Close`.
  - R9: a handler blocks the drain of `Shutdown` until the recorded factory context is
    done, then broadcasts to a room: the fake adapter receives that `Broadcast`, and its
    count, read in the handler right after the broadcast was delivered, is 0; the settled
    reading is 1.
- Example migration is owned by 2.5D after runtime and observability gates pass.

### 2.4 Observability

Depends on 2.1 (server ping ticker, close reasons), 2.2 (`Adapter`) and 2.3 (`Socket`
context, typed events, `AckTimeout`). Tracing and metrics go through two hook structs in
the root module with no external dependency; the OpenTelemetry bridge is the separate
module `contrib/otel`. The stage-1 boundary records are produced by
`LoggingHooks`. Every hook must have an exercised fire point and a corresponding log
record; the coverage test verifies both. The Engine.IO hook types are defined in
`engineio/hooks.go`:

```go
package engineio

type SessionInfo struct{ SID, Transport, RemoteAddr string }
type PacketInfo struct{ Type packet.Type; Frame frame.Type; Bytes int; Preview []byte }
type HandshakeResult struct{ Result string; Err error; Duration time.Duration }
type CloseReason string // "transport close", "transport error", "ping timeout", "forced close", "server shutting down", "parse error"

type Hooks struct {
    HandshakeStart  func(ctx context.Context, r *http.Request) context.Context
    HandshakeEnd    func(ctx context.Context, s SessionInfo, r HandshakeResult)
    RequestRejected func(ctx context.Context, r *http.Request, reason string, err error)
    SessionOpen     func(ctx context.Context, s SessionInfo) context.Context // becomes Session.Context()
    SessionClose    func(ctx context.Context, s SessionInfo, reason CloseReason, err error, d time.Duration)
    UpgradeStart    func(ctx context.Context, s SessionInfo, from, to string) context.Context
    UpgradeEnd      func(ctx context.Context, s SessionInfo, from, to string, err error)
    PacketRead      func(ctx context.Context, s SessionInfo, p PacketInfo)
    PacketWrite     func(ctx context.Context, s SessionInfo, p PacketInfo)
    PingSent        func(ctx context.Context, s SessionInfo)
    PongReceived    func(ctx context.Context, s SessionInfo, rtt time.Duration)
}

func ChainHooks(hs ...*Hooks) *Hooks // Start funcs thread ctx left to right; nil fields are skipped
func LoggingHooks(l *slog.Logger) *Hooks
```

The 2.0 skeleton declares `ChainHooks` and `LoggingHooks` of both packages with these
signatures and returns nil (no observer); 2.4 supplies the behaviour. Result and reason
strings the roadmap does not enumerate (upgrade results, request-rejection reasons,
disconnect reasons, adapter results) are defined by 2.4 in `docs/OBSERVABILITY.md` and
added as constants; the hook signatures stay strings.

```go
package socketio

type SocketInfo struct{ SID, SocketID, Namespace string }
type EventInfo struct{ Socket SocketInfo; Event string; AckID uint64; NeedAck, HandlerFound bool }
type EventResult struct{ Result string; Err error; AckQueued bool; Duration time.Duration }
type EmitInfo struct{ Socket SocketInfo; Event string; AckID uint64 }
type BroadcastInfo struct{ Namespace, Event string; Rooms, Except []Room; Local bool }
type AdapterMessage struct{ Namespace, Kind string; Bytes int } // Kind: broadcast, request, response

type Hooks struct {
    ConnectStart    func(ctx context.Context, s SocketInfo) context.Context
    ConnectEnd      func(ctx context.Context, s SocketInfo, err error, d time.Duration)
    Disconnect      func(ctx context.Context, s SocketInfo, reason string, d time.Duration)
    EventStart      func(ctx context.Context, e EventInfo) context.Context // result is the handler ctx
    EventEnd        func(ctx context.Context, e EventInfo, r EventResult)
    Emit            func(ctx context.Context, e EmitInfo)
    MessageQueued   func(ctx context.Context, e EmitInfo)
    EmitStart       func(ctx context.Context, e EmitInfo) context.Context
    EmitEnd         func(ctx context.Context, e EmitInfo, err error, rtt time.Duration)
    BroadcastStart  func(ctx context.Context, b BroadcastInfo) context.Context
    BroadcastEnd    func(ctx context.Context, b BroadcastInfo, result BroadcastResult, err error)
    WriteBufferFull func(ctx context.Context, s SocketInfo)
    AdapterPublishStart func(ctx context.Context, m AdapterMessage) context.Context
    AdapterPublishEnd   func(ctx context.Context, m AdapterMessage, err error, d time.Duration)
    AdapterReceiveStart func(ctx context.Context, m AdapterMessage) context.Context
    AdapterReceiveEnd   func(ctx context.Context, m AdapterMessage, err error, d time.Duration)
}

func ChainHooks(hs ...*Hooks) *Hooks
func LoggingHooks(l *slog.Logger) *Hooks
func (n *Namespace) Hooks() *Hooks // nil-safe wrapper methods for adapters in other modules
```

Hook lifecycle contract (result/reason values are documented closed enums):

| Operation | Start | Exactly one terminal notification | Data / failure handling |
| --- | --- | --- | --- |
| New-session handshake | HandshakeStart before checker/accept | HandshakeEnd on success or every failure path | result, duration, error; SID may be empty on rejection |
| Invalid request for an existing session | none | RequestRejected | log diagnostic only; never counted as a new handshake |
| Transport upgrade | UpgradeStart | UpgradeEnd on success, timeout, rejection or close | original/target transport; change current transport only on success |
| Namespace connect | ConnectStart | ConnectEnd | count socket only after CONNECT queued successfully |
| Inbound event | EventStart, including unknown/decode-error cases | EventEnd after handler/ack enqueue or rejection | ok, error, no_handler, decode_error, closed; AckQueued is not wire delivery |
| Emit with ack | EmitStart before enqueue | EmitEnd on reply, timeout, cancellation, enqueue failure or disconnect | one pending increment/decrement; result includes canceled and error |
| Broadcast | BroadcastStart | BroadcastEnd | partial local enqueue count and broker acceptance; never infer remote delivery |
| Adapter publish / receive | corresponding Start | corresponding End on success/error | byte size, duration; receive spans encompass decoding and local dispatch |

`RequestRejected` also logs failed initial requests but does not increment handshake
metrics: `HandshakeEnd` is their sole counter/duration source. `SessionOpen` fires
only after a successful handshake, and each open has one close. Hooks receive
borrowed metadata only for the duration of the call; observers retaining it must
copy. `PacketInfo.Preview` is capped at 256 bytes, excludes auth-bearing CONNECT
payloads and is empty by default. Add `engineio.Options.PayloadPreviewBytes` as an
explicit opt-in; capture only for an enabled consumer (TRACE for `LoggingHooks`),
apply redaction before delivery and never retain whole frame buffers for a preview.
The owning Socket.IO layer supplies protocol-aware redaction; Engine.IO does not
import the Socket.IO parser. Standalone Engine.IO applications supply their own
payload redactor. The redactor type and its `engineio.Options` field are defined and added
by 2.4E, because the boundary hands unredacted packet bytes to a trusted component and
follows from the fire points; the 2.0 skeleton declares only `PayloadPreviewBytes` (0 to
256) and `PacketInfo.Preview`. Callback retention rules and disabled-capture behaviour are tested.
Keep preview collection disabled when no redactor is configured; enabling TRACE
alone never enables v2 payload capture; v1 never logs payloads (1.L).

Start hooks thread context left to right; terminal hooks run right to left so
cleanup unwinds. Install `ChainHooks(opts.Hooks, LoggingHooks(log))`: tracing creates
the span before its start log, and terminal logging runs before span completion.
Session context is detached from HTTP cancellation per 2.0; the OTel bridge retains
the handshake span context for links while clearing the active parent for new event
spans. Test hook ordering and trace IDs on both start and terminal log records.
Serialize session open/upgrade/close notifications per session so gauge transfers
cannot race with close. Event contexts are retained with queued jobs and receive
their terminal hook even if shutdown discards the job before the handler runs.

Implementation split:

- **2.4E:** fire Engine.IO hooks from handshake, session, upgrade, packet and
  heartbeat paths according to the lifecycle table. Count packet bytes at the
  reader/writer boundary, including error paths.
- **2.4S:** fire Socket.IO hooks from namespace/event/ack/queue/broadcast paths;
  adapters call paired hooks through `Namespace.Hooks()`. Pass the context returned
  by `EventStart` to the handler.
- **2D:** propagate instance loggers before E/S instrumentation is merged.

Instance logger contract (wave 2D): resolve Socket.IO Logger > Engine Logger >
`logger.Log`; standalone engine servers and clients/dialers accept their own
logger. Nil fallback follows the current `slog.Default()`. Propagate the selected
logger through sessions, namespaces, codecs, transports and pre-session dial
failures. Stateless codecs may return errors for their owner to log once.
`Namespace.Logger()` supplies adapter/contrib diagnostics. Copy inherited options;
do not mutate caller options, shared transports or the application's default logger.

Every instance logger passes through `logger.Wrap`, preserving handler, attributes
and groups. `SOCKETIO_LOG_LEVEL` is read once at init; a valid value or runtime
`logger.Level.Set` overrides levels process-wide. `LevelUnset` delegates Enabled
to each handler. Invalid environment values warn once and act as unset. TRACE is
`slog.LevelDebug-4`, rendered by opt-in `logger.ReplaceAttr`. No logger backend
dependency is added; injected broker-client internal logs remain application-owned.

**Logging and overhead.** The inline stage 1 boundary records (1.L) are removed where a
hook now exists, and `LoggingHooks` emits the same messages and keys. It also emits the
records 1.L left out of v1, with these keys:

| Layer | Message | Level | Keys |
| --- | --- | --- | --- |
| engineio | `engineio: upgrade start`, `engineio: upgrade end` | DEBUG | `sid`, `from`, `to`, `err` |
| engineio | `engineio: packet` | TRACE | `sid`, `dir` (`in`, `out`), `pkt_type`, `frame`, `bytes`, `payload` |
| engineio | `engineio: ping`, `engineio: pong` | TRACE | `sid` |
| socketio | `socketio: connection accepted` | DEBUG | `sid`, `remote_addr` |
| socketio | `socketio: event` | TRACE | `sid`, `nsp`, `event`, `ack_id`, `handler_found`, `duration`, `err` |
| socketio | `socketio: ack` | TRACE | `sid`, `nsp`, `ack_id`, `dir` |
| socketio | `socketio: emit` | TRACE | `sid`, `nsp`, `event`, `ack_id` |
| socketio | `socketio: broadcast` | TRACE | `nsp`, `room`, `event`, `recipients` |
| socketio | `socketio: handler error` | WARN | `sid`, `nsp`, `event`, `err` |

In v2, `socketio: handler error` (WARN) replaces v1's `socketio: unhandled error` for
handler errors. Every other v1 trigger (decode errors, a marshal error, a CONNECT to an
unknown namespace, an overflow) keeps `socketio: unhandled error`. v2 has no `OnError`;
both records log WARN unless the API frozen in 2.0 adds an error handler, which then
lowers them to at most DEBUG when registered. No `Hooks` field, chained or not
(including `LoggingHooks`, the OTel bridge and `contrib/admin`), counts as a registered
error handler.
On top of these it adds `rtt` on pong, `rooms`, `except` and `local` on broadcast, and
`socketio: adapter publish` / `socketio: adapter receive` start/end lines at `TRACE`.
Broadcast `recipients` means successful local enqueues; also log `published`.
`TestHooksCoverEveryHookPoint` (root) reflects over both structs, runs one scenario
(handshake, upgrade, ping, connect with auth, event with ack, emit with ack,
broadcast, buffer overflow, disconnect) with a recording hook chained before
`LoggingHooks` at `trace`, including rejection, decode failure and adapter error
sub-scenarios, and fails if any field was not called or has no log record
with its message; adding a hook field without a log line therefore fails the build.
`BenchmarkEventDispatch` gets the sub-benchmarks `no-hooks`,
`logging-hooks-at-error` and `recording-hooks`.

**2.4O: `contrib/otel`.** Own `go.mod` (`.../contrib/otel/v2`), CI job and
`README.md`. `otelsocketio.NewHooks(opts ...Option) (*engineio.Hooks,
*socketio.Hooks)` with `WithTracerProvider`, `WithMeterProvider`,
`WithPropagators`, `WithoutTraces`, `WithoutMetrics`, `WithEventAllowList`;
`otelsocketio.NewSlogHandler(next slog.Handler) slog.Handler` adds `trace_id` and
`span_id` from the record context while preserving the supplied handler's sink,
attributes, groups and `Enabled` behaviour; it composes with `logger.Wrap` and
never changes the global logger. Trace context is extracted from the
`*http.Request` in `HandshakeStart`. The ctx returned from `SessionOpen` carries a
span context only (not a recording span), so later spans link to the handshake
instead of parenting under an ended span. Tests use `sdktrace/tracetest` and the
`sdkmetric` `ManualReader`; `BenchmarkEventDispatchOtel/noop` measures overhead.

Spans:

| Span | Kind | Parent / links | Attributes |
| --- | --- | --- | --- |
| `engineio.handshake` | Server | parent: incoming `traceparent` or the middleware span in `r.Context()` | `sid`, `transport`, `remote_addr`, `http.request.method`, `result` |
| `engineio.upgrade` | Internal | link: handshake | `sid`, `from`, `to`, `error` |
| `socketio.connect {nsp}` | Server | link: handshake | `sid`, `socket_id`, `nsp`, `error` |
| `socketio.event {nsp} {event}` | Consumer | link: handshake; ends at `EventEnd` | `sid`, `socket_id`, `nsp`, `event`, `ack_id`, `handler_found`, `error` |
| `socketio.emit {nsp} {event}` | Producer | child of caller ctx; emit-with-ack only; ends at `EmitEnd` | `sid`, `socket_id`, `nsp`, `event`, `ack_id`, `result` |
| `socketio.broadcast {nsp} {event}` | Producer | child of caller ctx | `nsp`, `event`, `rooms.count`, `except.count`, `local`, `recipients` (local), `published` |
| `socketio.adapter.publish {nsp}` | Producer | child of broadcast | `nsp`, `kind`, `bytes`, `error` |
| `socketio.adapter.receive {nsp}` | Consumer | root: the Redis message format is fixed for Node compatibility and carries no trace context | `nsp`, `kind`, `bytes`, `error` |

Instruments (Prometheus names replace dots with `_` and add unit suffixes):

| Instrument | Kind, unit | Attributes | Hook |
| --- | --- | --- | --- |
| `engineio.handshakes` | Counter | `transport`, `result` (`ok`, `bad_transport`, `checker`, `accept`, `no_hijacker`, `init`, `timeout`, `closed`) | HandshakeEnd only |
| `engineio.handshake.duration` | Histogram, s | `transport`, `result` | HandshakeEnd |
| `engineio.sessions` | UpDownCounter | `transport` | SessionOpen, successful UpgradeEnd (old -1, new +1), SessionClose |
| `engineio.session.duration` | Histogram, s | `transport`, `reason` | SessionClose |
| `engineio.upgrades` | Counter | `from`, `to`, `result` | UpgradeEnd |
| `engineio.packets` | Counter | `direction`, `type`, `transport` | PacketRead, PacketWrite |
| `engineio.packet.size` | Histogram, By | `direction`, `transport` | PacketRead, PacketWrite |
| `engineio.ping.rtt` | Histogram, s | `transport` | PongReceived |
| `socketio.sockets` | UpDownCounter | `nsp` | ConnectEnd (ok), Disconnect |
| `socketio.connects` | Counter | `nsp`, `result` (`ok`, `error`, `unknown_namespace`) | ConnectEnd |
| `socketio.socket.duration` | Histogram, s | `nsp`, `reason` | Disconnect |
| `socketio.events.received` | Counter | `nsp`, `event`, `result` (`ok`, `error`, `no_handler`, `decode_error`, `closed`) | EventEnd |
| `socketio.event.duration` | Histogram, s | `nsp`, `event` | EventEnd |
| `socketio.events.sent` | Counter | `nsp`, `event` | MessageQueued once per successful local enqueue, including adapter receive |
| `socketio.acks.pending` | UpDownCounter | `nsp` | EmitStart, EmitEnd |
| `socketio.ack.rtt` | Histogram, s | `nsp`, `event`, `result` (`ok`, `timeout`, `closed`, `canceled`, `error`) | EmitEnd |
| `socketio.broadcasts` | Counter | `nsp`, `local` | BroadcastEnd |
| `socketio.broadcast.recipients` | Histogram, {socket} | `nsp` | BroadcastEnd, LocalRecipients only |
| `socketio.write_buffer.overflows` | Counter | `nsp` | WriteBufferFull |
| `socketio.adapter.messages` | Counter | `nsp`, `direction`, `kind`, `result` | AdapterPublishEnd, AdapterReceiveEnd |
| `socketio.adapter.message.size` | Histogram, By | `nsp`, `direction` | AdapterPublishEnd, AdapterReceiveEnd |

`MessageQueued` has a TRACE log and is the sole source of sent-event counts;
broadcasts do not add another count on top of per-recipient enqueues. `Emit`
describes a fire-and-forget attempt, not delivery. Normalize raw/unregistered
event names to `_unknown` and dynamic namespaces to their registered pattern
before creating metric attributes; raw names may remain in logs/spans. Session
duration uses final transport; it does not represent duration per transport.

**Documentation.** `docs/OBSERVABILITY.md` owns `SOCKETIO_LOG_LEVEL`, the levels, the
log keys, the full `request rejected` reason list, the hook contract (goroutine, non-blocking, no `Emit`, no panic recovery),
the cardinality rule and the span and instrument catalogue. `CLAUDE.md` gets the
ownership row and the `contrib/otel/` layout row; `README.md` gets one line linking
to it. `TestObservabilityDocLists` (root) reflects over both `Hooks` structs and
fails if a field name is missing from the doc.

DoD for 2.4: `TestHooksCoverEveryHookPoint`, `TestChainHooksOrder`, `TestNilHooks`,
`TestEmitWithAckEndsOnDisconnect`, `TestEmitWithAckTimeout`,
`TestSessionContextPropagates` and `TestObservabilityDocLists` pass under `-race`;
`benchstat` of `BenchmarkEventDispatch/no-hooks` between the last 2.3 commit and the
merge of the 2.4 logging implementation shows 0 added allocs/op and at most 2% more time. Use the same
dedicated host, pinned Go/build settings and GOMAXPROCS, at least ten alternating
baseline/candidate runs and recorded benchstat confidence intervals. An inconclusive
comparison is rerun on that host, not accepted/rejected from a noisy shared CI run;
`BenchmarkEventDispatchOtel/noop` is at most 4 allocs/op and 1 µs/op above `no-hooks`;
both results are recorded in `CHANGELOG.md`; `go mod graph` of the root module has no
`go.opentelemetry.io`. In `contrib/otel`: `TestSpans` asserts one span per row of the
span table with the listed attributes and terminal conditions from the lifecycle
table, including events without ACK, rejection and failed ACK enqueue; `TestInstruments` asserts one series per
instrument; `TestMetricAttributesBounded` fails on `sid`, `socket_id`, `ack_id`,
`remote_addr` or an unregistered event in any metric attribute;
`TestHandshakeParentsUnderMiddlewareSpan` puts a recording span in the request context
and asserts the parent relation; `TestSlogHandlerAddsTraceID` passes.
`TestNoBadKeyAttrs` from stage 1 is ported to the v2 scenario and passes at `trace` with the 1.L message pattern and key list extended by the keys of the 2.4 records table and the keys added on top of it; `docs/OBSERVABILITY.md` owns the v2 list.
Add `TestHandshakeEndsExactlyOnce`, `TestSessionGaugeAcrossUpgrade`,
`TestMessageQueuedCountsOnce`, `TestAdapterSpanLifecycle` and
`TestPreviewDisabledNoAlloc`; assert no negative or stranded active-session/pending-ack
series after failures, cancellation and close. Stage 2.4 uses the memory adapter and
a recording adapter fixture for publish/receive failures; no Redis/NATS dependency.

Logger gate: under `-race`, run two servers and two clients with distinct recording
handlers plus a separate global handler. Cover rejected handshake, malformed codec
input, polling, upgrade, dial failure, events and disconnect. Explicit instance
records reach only their handler; nil fallback follows later `slog.SetDefault`.
Check precedence, independent thresholds without override, common threshold with
override, unchanged caller options/transports and preserved handler attrs/groups.
Repeat handler composition through the OTel wrapper and assert trace/span IDs.

Acceptance for 2.4: add `_examples/observability` (own module), a minimal single-server
`/chat` event fixture with memory adapter, OTLP collector and Jaeger compose stack.
Owner runs it with `SOCKETIO_LOG_LEVEL=trace`. A browser message appears as a
`socketio.event /chat message` span linked to that client's `engineio.handshake` span;
the log line for the same event carries the span's `trace_id`; `/metrics` shows
`socketio_events_received_total{nsp="/chat",event="message"}` incrementing and
the sum of `engineio_sessions` across transports equals the number of open tabs.
An explicit client disconnect logs its close reason and decrements the gauge;
abrupt tab termination is separately tested through timeout/transport failure.
Owner also runs two server instances in one process with separate application
handlers and confirms each receives only its instance's diagnostics, including
transport and codec errors, without calling `slog.SetDefault`.

### 2.5 Docs and release

`docs/MIGRATION.md` (including how v1 `Conn.Close` draining maps to v2 socket and
session close, the breaking changes recorded in Stage 1b, and the `v1.5.0` features v2 does not
carry: permessage-deflate (V1-15, D11; *Out of scope*), and `httpCompression`, the `io` cookie
and the per-emit `compress` flag of V1-15, which no v2 stage plans (`BroadcastFlags` is
`{Local bool}`); leaving those three out of v2 is a plan choice of this stage, not an owner
decision, and the owner has not been asked: 2.5 puts the question before it starts, and a
yes adds them to a v2 stage first, with the migration item rewritten), `docs/PROTOCOL.md` update, `docs/OBSERVABILITY.md`,
`contrib/otel/README.md`, tags `v2.0.0` and `v2/contrib/otel/v2.0.0` from the same commit, only
after the owner's declaration and after `v1.5.0` (*Repository layout*). The module path `.../v2`, the
`go.mod` and imports of every `v2/_examples/*` (B3) and the `v2/README.md` badge and links (B5) are
set by the restructure and only verified here (the grep below and the Acceptance), not redone. 2.5
writes the links in `engineio/README.md` and the import paths of `contrib/otel` and `adapters/*`. Task 2.5D migrates all non-Redis examples to
`socket.io-client@4` and the generic API, pinning maintained framework versions
compatible with Go 1.22; Redis examples remain deferred to 4b.

DoD: both official suites (`engine.io-protocol/test-suite`,
`socket.io-protocol/test-suite`) pass in CI against the Go server; a CI Node script with
real `socket.io-client@4` covers connect, namespace with auth, ack both ways, binary,
disconnect, reconnect after server restart; parser and payload unit tests cover every
example in the two specs; `go mod graph` shows no `gorilla/websocket`, and a lint rule
forbids direct `reflect` imports in production code outside `parser` (test reflection
is allowed); all stage-2 examples build and run against
`socket.io-client@4`; `go vet`, lint, `-race` green; idle-connection benchmark numbers
recorded; `docs/PROTOCOL.md` lists every unimplemented item; the stage 2.4 DoD holds at
the commit to be tagged. Router integration: the `examples` CI job starts `_examples/default-http`,
`gin-gonic`, `go-echo`, `iris` and `gf`, and `TestFrameworkSmoke` in `_examples/smoke`
(own `go.mod`) completes a websocket handshake and an `add user` event answered by `login` through
each with the Go client; a test in `engineio` with a `ResponseWriter` that hides `http.Hijacker`
gets HTTP 501. Links: `pkg.go.dev/github.com/sshaplygin/go-socket.io/v2` renders the
module (the tagged version at tag time), and the grep below has no active v2 code/module imports of the old
path; historical v1 decisions, migration examples and changelog entries are allowed:

```sh
grep -rn 'googollee' --include='*.md' --include='go.mod' --include='*.go' v2 docs
```

Acceptance: a browser page on `socket.io-client@4` from CDN connects to
`_examples/default-http`, adds a user, receives `login`, and the server logs a
clean disconnect on an explicit client disconnect. `docs/MIGRATION.md` is enough to port
`_examples/gin-gonic` without reading library code. A handler with a wrong payload type
fails at compile time. Every badge and link in `v2/README.md` resolves to the v2 module.
Pin Node, protocol-suite commits and client versions in CI. At tag time: test source
consumers against released root/contrib module versions without workspace `replace` directives,
publish the root tag before dependent module tags, and verify each module's minimum Go version. Redis examples and cluster acceptance are explicitly deferred to 4b.

## Stage 3. Realtime chat example (`_examples/chat/`, own go.mod)

- **Server**: namespace `/chat`; `Auth[Credentials]` middleware reading the nickname
  from the auth payload; rooms; typed events `Message` (ack returns id and timestamp),
  `Typing`, `History` (ring buffer of 50 per room), `Presence` on join/leave; direct
  messages via the socket-id room; binary image attachment; graceful shutdown;
  `/metrics` through `contrib/otel` and `otel/exporters/prometheus`.
- **Upstream parity**: the upstream chat connects with `io()`, so the parity check runs
  against the default namespace `/` and coexists with the richer `/chat` namespace on
  the same server; the two share no state (the default namespace has its own user
  counter, no rooms, no history, no `Auth`). Pinned source: `socketio/socket.io`
  commit `1eaa582d3b453e3e6f522300ed05b10da0a0799b`, directory
  `examples/chat/public` (`index.html`, `main.js`, `style.css`), vendored in
  `_examples/chat/upstream/`.
- **Clients**: `index.html` with no build step on a pinned `socket.io-client@4` from
  CDN; Go CLI on `client/` and generic event descriptors; `cmd/load` (N Go clients,
  p50/p99 ack latency), with the documented error-first typed ack convention.
- **Deployment**: single server with the memory adapter, OTLP collector and Jaeger
  in `docker-compose.yml`; reuse the stage 2.4 observability configuration. The
  two-node Redis/NATS profile is implemented and accepted in stage 4b.
- **Test**: integration test starting the server and two Go clients, checking history
  and presence, run in the CI examples job.

DoD: `docker compose up` in `_examples/chat` starts in one command; a message sent by
one client reaches another in the same room; `cmd/load` with 500 clients at 10 msg/s
reports p99 ack latency and no write-buffer overflow; integration test green in CI;
`_examples/chat/README.md` documents only how to run it.

DoD (upstream chat parity): the vendored upstream client is
byte-identical to the pinned commit except the one `socket.io-client` script tag, which
points to a pinned `socket.io-client` 4.x build, and it works against the Stage 3
server on the default namespace with the upstream names and payloads: client to server
`add user`, `new message`, `typing`, `stop typing`; server to client `login`
`{numUsers}`, `user joined` and `user left` `{username,numUsers}`, `new message`
`{username,message}`, `typing` and `stop typing` `{username}`; the sender is excluded
from every broadcast. A CI test in the `examples` job (two Go clients, or a Node
`socket.io-client` 4.x script) asserts this and diffs the vendored files against the
pinned commit.

Acceptance: owner runs the single-server compose stack, opens two browser tabs,
exchanges messages, sees typing and presence, uploads an image and verifies graceful
shutdown; the owner also opens the vendored upstream client on the default namespace
in two tabs and sees login count, join/leave, messages and typing as the upstream demo
shows them, and confirms the parity CI test is green (the parity DoD line). Define the load rate as per-client (5000 messages/s total), fix message
size, room/fan-out distribution and run duration, and record host/Go settings with
latency, errors, queue peaks and memory; do not use an unspecified workload as a gate.

## Stage 4b. Independent adapters: Redis and NATS

Each adapter has its own `go.mod` and CI job and is tagged independently
(`adapters/redis/v2.0.0`, `adapters/nats/v2.0.0`). The root `go.mod` has no Redis or
NATS dependency; it does carry `vmihailenco/msgpack/v5` (with its `tagparser/v2`), which
`adapter/codec` imports since 2.2. Both depend on the root module as a normal versioned dependency and on
`adapter/codec` for the message format. At tag time (M5 is accepted before it): release root `v2.2.0`, containing
`adaptertest` and any shared-codec additions, before tagging adapter modules, and verify
root and adapter consumers/tests against published versions without local replacements.
In this stage's tests (`adaptertest` and both backend suites) the adapter's request
timeout is 5 s. A *recovery poll* calls `Sockets` every 50 ms and passes at the first nil
error within 2 s (it fails at 2 s); every earlier call returns the local data and an
error, because restoring ends only when a flush or confirmation read completes, which a
single call cannot be required to hit.

- **`adapters/redis`**: compatibility with the non-sharded
  [`@socket.io/redis-adapter@8.3.0` wire format](https://github.com/socketio/socket.io-redis-adapter/blob/8.3.0/lib/index.ts)
  for stage 2.2 operations; cluster broadcast-with-ack is excluded. Channels:
  `<prefix>#<nsp>#`, `<prefix>#<nsp>#<room>#`, `<prefix>-request#<nsp>#`,
  `<prefix>-response#<nsp>#` and targeted `<prefix>-response#<nsp>#<uid>#`.
  Broadcast messages are msgpack `[uid, packet, opts]` via `vmihailenco/msgpack/v5`
  matching notepack output; supported request/response messages use Node's JSON
  encoding. Freeze fixtures for every supported operation, including
  `publishOnSpecificResponseChannel=true` and false (the Node corpus and its oracle are
  in `adapter/codec/testdata`, landed with 2.2). The injected
  `redis.UniversalClient` (the type `redis.NewUniversalClient` returns, which for one
  address holds a `*redis.Client`) must send every command to one Redis master, so only
  a `*redis.Client` (from `redis.NewClient` or `redis.NewFailoverClient`) is accepted;
  any other implementation, such as `*redis.ClusterClient` or `*redis.Ring`, is
  rejected at construction with the exported `ErrUnsupportedRedisClient`: counting
  peers on a Cluster needs PUBSUB NUMSUB summed over every master, as redis-adapter
  8.3.0 `lib/util.ts` does, and a Ring sends subscriptions and keyless PUBLISH and
  PUBSUB to different shards. A `*redis.Client` that routes to a replica
  (`FailoverOptions.ReplicaOnly`, or `NewClient` addressed to a replica) is
  unsupported, and the constructor godoc says so: a PUBLISH or PUBSUB NUMSUB on a
  replica reaches only that replica's subscribers, and go-redis v9 exposes no
  client-side way to detect it (the read-only flag of `redis.Options` is unexported;
  `ReplicaOnly` only selects the failover dialer; checked against v9.7.0, and the
  adapter's PR re-checks them against its pinned version). The adapter does not ask the
  server for its role: miniredis has no ROLE command. Bound request time and
  reconnect subscriptions with backoff; the backoff's initial and maximum delays are
  documented options.
  For 2.2 *Readiness*, construction and every resubscribe read the PSUBSCRIBE and
  SUBSCRIBE confirmations within the `SubscribeTimeout` option (default 10 s). Every read
  is bounded by the time left, and construction also stops when the factory context ends:
  a go-redis `ReceiveTimeout` reads without that context, so the adapter closes the
  attempt's `PubSub` from `context.AfterFunc`, stopped once the attempt is confirmed; when the stop reports that
  the function already started, construction fails with the context error instead of
  returning the adapter. An
  unconfirmed resubscribe is a failed attempt that closes its connection and grows the
  backoff. Each attempt uses a new `PubSub`, and the adapter closes a failed one as soon
  as it observes the error: after a connection error a go-redis `PubSub` redials and
  resends its subscriptions inside `Receive`, outside the backoff. The adapter becomes
  restoring before it logs `socketio: adapter subscriber lost`. The expected peers are
  PUBSUB NUMSUB of the request channel minus this instance, floored at 0; when NUMSUB
  fails they cannot be determined. Deterministic tests run on miniredis with a pre-hook,
  installed on the running server, that holds PSUBSCRIBE and SUBSCRIBE and releases the
  held commands all at once or one at a time, in the order they reached it (the v1 helper
  `delayRedisSubscriptions`, PR #18, holds PSUBSCRIBE). The *live values* are miniredis
  NUMPAT, NUMSUB of the request and response channels and `CurrentConnectionCount`. They
  are *settled* when they are equal on two reads 50 ms apart; each case first PINGs the
  injected client and waits for that, at most 5 s, and records them as the baseline.
  *Nothing left* means they are settled within 5 s of releasing the hold with NUMPAT and
  NUMSUB equal to the baseline and the connection count not above it, and the client still
  answers PING. A *peer* is another adapter with a socket, on its own client of the same
  miniredis; each case below says whether one exists, and the baseline is recorded after
  it is constructed.
  - 4R-T1: a peer exists. The test installs the hold, starts construction and, once the
    hook holds its first command, releases that one only. 200 ms later construction has
    not returned and the hook holds a second command (an adapter reading one confirmation
    returns earlier); releasing that one returns construction within 500 ms. The test adds
    a socket to the new adapter immediately after construction returns and only then
    issues a peer's broadcast, with no wait: the socket receives it within 1 s. The peer's
    `Sockets`, issued after the add, lists it.
  - 4R-T2: no peer. With `SubscribeTimeout` 200 ms and a 2 s hold, construction returns
    an error within 1 s, leaving nothing.
  - 4R-T3: no peer. Server `Close`, called after the hook has held its first command of a
    namespace creation, returns at once; with the hold still in place, the creating call
    returns at once after `Close` with an error matching `ErrNamespaceClosed`. The hold
    is released only then, leaving nothing.
  - 4R-T4: no peer. `Sockets` and `FetchSockets` return the local data and nil at once;
    with NUMSUB failing (pre-hook error), the local data and an error at once.
  - 4R-T5: no peer until the last step. The injected client's `Dialer` records every
    connection it dials. With a live adapter that has a socket and the baseline recorded, the test installs the hold first (only PSUBSCRIBE
    and SUBSCRIBE are held, and an established subscription sends neither until its
    connection closes, so no resubscribe reaches miniredis before it), then closes every
    recorded connection. Once the `subscriber lost` record is logged, `Sockets` returns
    the local sockets and an error at once. miniredis is not restarted: `Restart` builds
    a server without the pre-hook. With `SubscribeTimeout` 200 ms and the backoff's documented
    initial and maximum delay options set to 50 ms and 200 ms, the hold stays until it has held the first command
    (PSUBSCRIBE or SUBSCRIBE) of three distinct server-side connections within 10 s, or
    the case fails; an adapter attempt and the go-redis redial inside `Receive` are each
    a new connection. When the third has held its first command, `Sockets` returns the
    local sockets and an error at once, nothing being confirmed (an adapter that stops
    restoring when it creates or dials the new connection returns nil). The test then
    releases the held commands in arrival order up to the third connection's first one
    only; 50 ms later `Sockets` still returns the local sockets and an error (one of two
    confirmations), and the rest is released. Leaks are checked on the server only, after
    the release (a held connection stays counted until its command returns): once the live values are settled, a recovery poll passes, and
    nothing is left. A leaked attempt is an open
    connection miniredis counts whatever the client believes; the PR that adds the test
    also shows the check failing on a variant that leaves a failed attempt's connection
    open (a read with a timeout the library does not treat as fatal, and no `Close`). A
    peer, constructed only then, broadcasts, and the broadcast reaches the adapter's socket
    within 1 s.
  - 4R-T6: construction with a `*redis.ClusterClient` or a two-shard `*redis.Ring`
    returns an error matching `ErrUnsupportedRedisClient` (`errors.Is`).
- **`adapters/nats`**: subjects `<prefix>.<encoded-nsp>.broadcast` and
  `<prefix>.<encoded-nsp>.room.<encoded-room>`. Encode each arbitrary UTF-8 name as
  `b` plus unpadded base64url of its bytes (empty name becomes `b`); dots, wildcards
  and whitespace never become subject syntax. Validate the configured prefix and
  test empty, Unicode, dotted and wildcard-containing names. Broadcasts use the
  shared msgpack body. `Sockets`/`FetchSockets` use an explicit reply-inbox
  subscription for multiple responses, unique request IDs and a bounded deadline;
  do not use a first-response-only request helper. Snapshot expected peers from
  heartbeat membership, deduplicate replies by server ID and return partial data
  plus an error for missing peers. Document membership staleness and distinguish
  known zero remote peers from unavailable discovery. `ServerSideEmit` follows the
  publication-only contract in 2.2. Inject `*nats.Conn`; reconnect is handled by the
  client. Heartbeats are sent every `HeartbeatInterval` (option, default 5 s, 200 ms in
  tests). For 2.2 *Readiness*, construction flushes after subscribing with a context
  derived by `context.WithTimeout` from the factory context and the documented
  `SubscribeTimeout` option (default 10 s; `FlushWithContext` needs a deadline). The
  flush confirms only the connected server (the limit stated in 2.2 *Readiness*); the
  adapter README says so and no case covers the rest. The adapter is restoring while
  `nc.IsConnected()` is false and from any reconnect until a flush succeeds after it. It
  sees a reconnect, even one shorter than a status poll, by comparing `nc.Stats().Reconnects`
  with the value read before its last successful flush; it polls every 100 ms and checks at each query,
  and never replaces the application's handlers. The expected peers are undetermined
  until one `HeartbeatInterval` after construction, so for that long every cluster query
  returns the local data and an error, also with no peer; the adapter README says so.
  Test with an embedded `nats-server/v2`, no Docker, and a test TCP proxy between client
  and server. The proxy listens on one fixed address, dials the server anew for each
  accepted client, closes the client side when the upstream closes, and has three controls.
  `Stall()` stops forwarding client bytes on live connections and buffers them;
  `StallNext()` does the same for the next connection that completes its handshake: the
  proxy engages the stall before it forwards the server's first PONG, so nothing the
  client sends after the handshake passes, and a connection whose upstream dial fails
  never completes one; `Release()` forwards the buffered bytes in order and ends the stalls. *Nothing
  left* is read after `Release()` and a following `nc.FlushTimeout(2 s)`, which returns
  once the server has processed everything the client sent before it: the server's
  subscription count and `nc.NumSubscriptions()` equal their values before construction.
  A leaked SUB reaches the server on release and raises the count; a SUB followed by its
  UNSUB leaves it unchanged. Every `Test4N_T<n>` builds its own server and repeats the
  steps it names.
  - 4N-T1: no peer. `HeartbeatInterval` plus 100 ms after construction the test shuts the
    embedded server down; `nc.IsConnected()` becomes false within 10 s (the test fails
    beyond it); `Sockets` and `FetchSockets` then return the local data and an error at
    once.
  - 4N-T2: with `Stall()` applied, construction with `SubscribeTimeout` 200 ms returns an
    error within 1 s; with `SubscribeTimeout` 10 s and the factory context cancelled
    after the proxy buffered the SUB, it returns an error at once. `Release()` comes only
    after the error, then each leaves nothing. The PR that adds the case shows it failing
    on a variant that skips the unsubscribe.
  - 4N-T3: `HeartbeatInterval` 2 s, a peer adapter running. `Sockets` issued as soon as
    construction returns returns the local sockets and an error at once; 2.1 s after
    construction it returns the peer's sockets and nil within 1 s.
  - 4N-T4: no peer; the connection has `MaxReconnects` -1 and `ReconnectWait` 100 ms.
    The test calls `StallNext()` right after construction returns, then does the 4N-T1 shutdown with
    its assertions, restarts the server on the same address and waits for
    `nc.IsConnected()` to turn true within 10 s. The resent subscriptions and the
    adapter's flush then sit in the proxy, so `Sockets` returns the local sockets and an
    error at once although `nc.IsConnected()` is true. After `Release()` a recovery
    poll passes.
  - 4N-T5: the application's `DisconnectedErrHandler` and `ReconnectHandler` count
    their calls; the test repeats the 4N-T1 and 4N-T4 steps and then closes the adapter;
    the first handler was called once and the second once.
- Both adapters report through `Namespace.Hooks()` (paired `AdapterPublishStart/End`
  and `AdapterReceiveStart/End`) and use `Namespace.Logger()` for other library-owned diagnostics.
  The Redis adapter logs WARN `socketio: adapter publish failed` on a PUBLISH error,
  `socketio: adapter bad message` on a message it cannot decode, and
  `socketio: adapter subscriber lost` once per lost subscription before the backoff
  reconnect, with `nsp`, `channel` and `err`; `channel` joins the
  `docs/OBSERVABILITY.md` key list and `TestRedisAdapterLogs` checks the three records;
  add isolation tests proving that two adapters attached to differently configured
  servers do not send logs to each other's handlers or the global fallback.
- **`adaptertest`** package in the root module: conformance suite any adapter runs
  against itself (like `fstest.TestFS`): join/leave, broadcast to room, except, local
  flag, fetch across two adapters, server-side emit, peer loss with timeout, both
  adapter hooks firing, and the generic 2.2 *Readiness* cases: for a backend with
  peers, a socket added to the new adapter immediately after construction returns, then
  a peer's broadcast issued with no wait, gets a nil error with `Published` true and
  reaches that socket within 1 s; a query
  with no peer, issued after the harness's `DiscoveryDelay` (0 for Redis, the
  `HeartbeatInterval` plus 100 ms for NATS), returns the local data and nil at once;
  with a socket added and the factory context cancelled after construction returns, and
  200 ms waited for every backend whatever its `DiscoveryDelay` (longer than any
  in-process cancellation), a peer's broadcast still reaches the socket and, after
  `DiscoveryDelay`, a query with a peer still answers until `Close`. The harness calls `AdapterFactory` itself with a context
  it cancels once the call returns, because `Server` cancels that context only in
  `Shutdown` or `Close` (2.3S R9 covers that path). They run as the subtests `Readiness/PeerBroadcast`,
  `Readiness/NoPeerQuery` and `Readiness/CtxAfterReturn` of `adaptertest.Run`, which
  each backend module calls from its wrapper test `TestAdapterConformance`. Held
  subscriptions and subscriber loss need a backend harness and stay in the backend
  suites (4R, 4N).
- `docs/ADAPTERS.md`; `adapters/<name>/README.md` for backend options; chat example
  supports both backends.
- Restore the migrated Redis examples and add the chat's two-server compose
  profile, selected with `ADAPTER=redis|nats`, with nginx affinity for polling.
  Acceptance routes clients to explicit backends (direct URLs or distinct test
  client addresses); two tabs behind `ip_hash` alone do not prove cross-node traffic.
  History remains an explicitly documented per-node in-memory demo buffer; this
  stage does not introduce durable or globally ordered chat history.
- Adapters close their subscriptions and workers while leaving injected broker
  connections usable. Tests cover peer loss, reconnect gaps, late/duplicate replies,
  partial results, `Local` isolation and repeated close. Redis multi-peer queries
  obey the same partial-result contract, using its protocol's peer-count discovery.

DoD: `go mod graph` of the root module contains no redis or nats module; `adaptertest`
passes for in-memory, Redis and NATS; Redis suite (testcontainers, two servers) and NATS
suite (embedded server, two servers) pass under `-race`; every 4R and 4N case
passes under `-race` as the test `Test4R_T<n>` or `Test4N_T<n>` (the 4R cases on
miniredis), and so does each `adaptertest` `Readiness/` subtest of the Redis and NATS
suites. In each adapter module `go test -race -count=1 -json -run
'^(Test4R_T[1-6]|TestAdapterConformance)$' ./...` (`Test4N_T[1-5]` in `adapters/nats`)
must report a pass for each of its ids and no skip, fail or missing id. The ids are
the test names and `TestAdapterConformance/Readiness/PeerBroadcast`, `.../NoPeerQuery`
and `.../CtxAfterReturn`: a skipped subtest does not fail its parent, so a CI script
(added with 4b) reads the `-json` events for every id and fails otherwise, and a PR
that adds a case adds its id there;
cross-language CI test:
one Go server and one Node `socket.io@4` server with `@socket.io/redis-adapter` share
Redis, a room broadcast from each side reaches a client on the other, and
`fetchSockets` from Node lists the Go socket. Msgpack fixtures captured from notepack cross-decode in
both directions with equal semantics; require exact bytes only for canonical
fixtures where map ordering and numeric representation are fixed.

Acceptance: the chat cluster runs with one Go and one Node instance behind the same
nginx on Redis, and with two Go instances on NATS, and both tabs see each other's
messages in each configuration.
The Go/Go profile additionally verifies that terminating node A leaves node B's
existing sessions alive and observable. Connection recovery to the failed node,
replay of missed messages and shared history remain outside this acceptance.

## Stage 5. Socket.IO Admin UI (required final product stage)

Entry: M5. Output: module `github.com/sshaplygin/go-socket.io/v2/contrib/admin/v2`
with its own CI/release and direct connection from the unchanged official UI.
Support hosted and self-hosted static UI. Pin an upstream UI release and matching
instrumentation commit in 5A; record them in the module README. Reference contracts:
[typed events](https://github.com/socketio/socket.io-admin-ui/blob/develop/lib/typed-events.ts),
[instrumentation](https://github.com/socketio/socket.io-admin-ui/blob/develop/lib/index.ts).
Node is a compatibility-test dependency, not a Go server runtime dependency.

1. **Observation API and lifecycle.** Add concurrency-safe snapshots of namespaces,
   sockets, rooms and serializable handshake/data, plus notifications for namespace
   creation, room join/leave and explicit socket-data updates. Reuse stage 2.4 hooks
   for connection, transport, packet and event metadata. Add opt-in observation of
   incoming and outgoing event arguments, including broadcasts, for the detailed UI;
   payload capture is disabled when unused. Define an explicit data-update API rather
   than trying to observe arbitrary mutations of application-owned Go values. Keep
   additions compatible with the v2 API of Stage 2. Hook additions also update
   `LoggingHooks`, hook coverage tests and `docs/OBSERVABILITY.md`.
   Admin instrumentation inherits the owning server's logger through its namespace;
   authentication, queue overflow and shutdown diagnostics obey the stage 2.4
   instance isolation contract, verified with distinct handlers on two servers.
   Define snapshot/update synchronization before implementing UI handlers. For each
   Go node, atomically register an observer and capture a state snapshot with a
   monotonic local watermark under the same state synchronization boundary. Buffer
   subsequent updates, send one combined `all_sockets`, then replay only updates
   after each node's watermark through the same ordered admin queue. Internal
   sequence/server IDs stay internal; the official UI wire format is unchanged.
   No global atomic cluster snapshot is promised. Extend the optional admin adapter
   capability to carry node snapshots/watermarks; ordinary `FetchSockets` alone
   cannot supply this synchronization boundary. Overflow or peer loss during initial
   sync aborts it and forces a fresh snapshot rather than presenting partial state
   as complete. Node instrumentation has no such watermark contract: mixed Go/Node
   observation is best-effort during concurrent mutations, with reconciliation
   after quiescence via a fresh snapshot/reconnect; validate convergence against the
   pinned UI and Node package. Node failure detection marks its sockets stale and
   removes them through supported UI events; do not rely only on missing stats.
2. **Admin namespace and authentication.** Provide `admin.Instrument` with a
   configurable namespace (default `/admin`), unique `serverId`, authentication,
   session store, mode and read-only option. Accept `username`/`password` or
   `sessionId` in the namespace handshake auth; emit `session` for session reuse.
   Require configured authentication or explicit opt-out. Default to read-only;
   enforce it on the server as well as in advertised features. Support a shared
   session store for clustered deployments. Document the UI origin, CORS credentials
   and WebSocket origin settings needed for hosted and self-hosted UI connections.
3. **Statistics and detailed observation.** Emit `config` with only implemented
   `supportedFeatures`, and `server_stats` every two seconds with server identity,
   uptime, connection counts, namespace counts and `aggregatedEvents`. Production
   mode sends aggregate statistics; development mode additionally sends `all_sockets`
   and live `socket_connected`, `socket_updated`, `socket_disconnected`, `room_joined`,
   `room_left`, `event_received` and `event_sent` updates. Match upstream argument
   order, timestamps, binary handling and serialized socket fields; distinguish an
   Engine.IO session from its namespace sockets. Exclude admin traffic from detailed
   event observation to prevent recursion. Never serialize handshake auth; provide
   redaction for headers, socket data and event arguments.
4. **Administrative commands and cluster support.** Implement `emit`, `join`,
   `leave` and `_disconnect`, including namespace/room filters and the distinction
   between namespace disconnect and transport close. Add an optional adapter
   capability interface for `SocketsJoin`, `SocketsLeave` and `DisconnectSockets`
   rather than breaking the stage 2.2 `Adapter` interface. Implement it for memory,
   Redis and NATS; extend `adaptertest` with remote operations and peer-loss deadlines.
   Freeze result/error semantics in 5A: success means local application plus broker
   acceptance for remote commands, not confirmed execution by peers. Return partial
   local results and publication errors without implicit retries. Match Node's
   unacknowledged bulk join/leave/disconnect encoding; deadlines bound publication
   and snapshot queries, not remote execution. Tests separately observe remote state.
   Use the synchronized snapshot
   capability for Go nodes and `FetchSockets` for the documented Node fallback;
   deliver each server's statistics and live updates to admin clients
   across the cluster, and avoid duplicate observations. Advertise bulk command
   features only when the active adapter supports them.
5. **Bounded delivery and shutdown.** Hooks only record or enqueue observations;
   they never emit or block on the UI. Separate workers send updates through queues
   bounded by both record count and bytes per admin consumer, including initial-sync
   buffering. Define and test overflow handling: count dropped updates and
   terminate the slow admin connection so reconnect obtains a fresh snapshot rather
   than silently retaining stale state. Cap payload capture size, stop workers and
   tickers on close, and release observers. Benchmark disabled instrumentation,
   production mode and detailed mode against the same server workload; disabled
   payload observation adds no allocations to event dispatch.
6. **Documentation, example and release.** `docs/ADMIN_UI.md` owns the event contract,
   supported features, modes, lifecycle, buffering and cluster behaviour;
   `contrib/admin/README.md` owns installation and configuration. Update the
   `CLAUDE.md` ownership/layout tables and add a README link. Extend the chat compose
   example with a pinned static Admin UI service and authenticated instrumentation
   on each server. At tag time, release additive core changes as `v2.3.0`, the module as
   `contrib/admin/v2.0.0`, and compatible adapter updates as minor releases.

DoD: event fixtures from the pinned upstream instrumentation match the Go output,
including positional arguments and date encoding. Browser tests against the actual
pinned UI cover authentication, reconnect/session reuse, statistics, socket and room
lists, transport upgrades, binary event display, live updates and administrative
commands. Tests prove that read-only mode rejects commands, payload redaction holds,
admin events do not recurse, slow admin consumers do not stall application sockets,
and shutdown leaks no library-owned goroutines. During initial sync and reconnect,
continuously connect/disconnect sockets and change rooms on two nodes; assert that
the UI converges without resurrected sockets, missed removals or duplicate entries.
Exercise snapshot queue overflow and actual UI reconnect, plus the documented
mixed-Node fallback after quiescence. Race tests and the expanded `adaptertest` pass for
all three adapters. Cluster tests cover two Go nodes with Redis and NATS and a mixed
Go/Node Redis cluster with instrumentation on both nodes: an admin client connected
to one node sees remote sockets and server statistics and can administer remote
sockets. Benchmark results and tested upstream versions are recorded in the release
notes; earlier protocol and observability checks remain green.

Acceptance: owner starts the chat compose stack and opens the unchanged Admin UI.
The UI shows both servers and their live connections; sending messages, joining and
leaving rooms and closing a tab update the corresponding views. With administrative
commands enabled, a remote socket can join/leave a room and be disconnected. Killing
one server is visible in the UI while the surviving server remains observable;
reconnecting the UI refreshes its state. Repeat with Redis and NATS. Passing this
acceptance completes product scope and opens the final benchmark stage.

## Stage 6. Final comparative benchmarks

Entry: M6, including Admin UI and the adapter updates. Earlier microbenchmarks
remain regression gates; this stage compares complete servers after product work.
Owner freezes the matrix in 6A before measurements; runner implementation can then
proceed independently in 6B. Reserved measurement hosts run one candidate at a time.

**Candidates and compatibility.** Compare our final v2, `zishang520/socket.io` and
the official `socket.io` JS/TS server running on Node.js. Pin exact commits/tags,
Go/Node versions, dependency lockfiles and build flags. Also record this fork's
v1 line (the repository-root module `github.com/sshaplygin/go-socket.io` at a commit pinned in 6A; `v1.5.0` once tagged) as a legacy baseline for shared scenarios, with a compatible EIO3 client;
report it separately from EIO4 comparisons. A feature support matrix must mark
unsupported cases, never score them as zero throughput. Benchmark wrappers implement
the same application logic and wire payload/ACK contract; TS source is built before
timing, with no development server or runtime transpilation.

**Workload matrix.** Freeze exact payload sizes, room topology, connection counts,
offered rates and run lengths in a checked-in manifest. Required scenarios:

| Scenario | Controlled dimensions | Primary measurements |
| --- | --- | --- |
| Idle and connection churn | 1k/10k sessions; websocket-only, polling-only, polling-to-websocket upgrade | RSS per session, CPU, connect latency, failures, cleanup |
| Events and ACKs | one-to-one; text and binary at 64 B, 1 KiB and 64 KiB; fixed-rate load and saturation sweep | delivered msg/s and bytes/s; p50/p95/p99 latency, timeout/error/drop rate |
| Room broadcast | fixed room membership/fan-out; memory and two-node Redis | recipient deliveries/s, end-to-end latency, broker load |
| Overload and recovery | stalled receivers, bounded queues, disconnect/reconnect and server drain | healthy-client latency, memory/queue peaks, rejected work, recovery time |
| Product observability | disabled; comparable aggregate metrics/logging; Admin UI production and detailed modes with one real connected UI | throughput/latency/memory cost relative to each candidate's disabled mode |

For unsupported observability/adapter combinations, report our feature overhead
separately; do not compare it with an uninstrumented competitor. NATS is an own-v2
extension unless a candidate supports equivalent semantics. Retain the prepared
dense-polling microbenchmark alongside server RSS measurements; wire-byte limits
alone do not establish decoded-memory parity.

**Measurement protocol.** Use identical server CPU/memory limits, network/TLS and
compression settings, heartbeat/size/queue policies where configurable, and broker
versions/topology. Record differences that cannot be aligned. Publish separate
single-core and fixed multicore-budget results, including process/worker topology;
do not compare unrestricted Go scheduling against a CPU-capped Node process.
Keep load generators and brokers off server measurement cores; verify generator
headroom. Use the same pinned client generator for EIO4 candidates and validate
traffic/counts before timing. Use an open-loop fixed-rate phase with latency from
scheduled send time, so slow replies do not silently reduce offered load; report
scheduled, sent, acknowledged and delivered totals, including failed/time-out work.
Freeze saturation criteria in 6A: maximum sustainable rate requires p99 <= 100 ms,
errors/timeouts <= 0.1%, and no sustained queue/RSS growth in the measured window.
Report other operating points too; this definition is not a promise that v2 wins.

Use at least 30 s warm-up and 120 s measurement, ten independent runs per published
comparison, randomized candidate order and a fresh process per run. Record host/OS,
CPU affinity, runtime/GC settings, resource counters and confidence intervals.
Keep runtime-specific GC/heap/alloc/goroutine or event-loop diagnostics separate
from cross-runtime metrics; archive raw samples and analysis scripts. Correctness
checks run before and after load; missing deliveries are counted, not discarded.

**Deliverables and gate.** Add an isolated `benchmarks/` harness with independent
runner modules/manifests, one documented command per scenario, and
`docs/BENCHMARKS.md` owning methodology, support matrix and results. Store raw data,
configs, checksums and charts as versioned downloadable artifacts; update the
CLAUDE.md ownership map and link the report from README. CI validates runners and a
short smoke scenario; full results come from reserved hosts. Acceptance requires
complete comparable results, recorded unsupported cases and an independent rerun
of a representative scenario within the published uncertainty, not a preselected
performance ranking. Investigate mismatches and limitations in the report. If this
stage triggers code changes, rerun affected correctness gates and comparisons with
new revision IDs; release them separately. M7 closes the benchmark scope.

## Stage 7. Go client as a separate package (v1, root module; executed in Stage V1 as PR V1-9)

Owner decision of 2026-10-09: the Go client is supported as a separate package for both
lines; the v2 half is 2.3C. Owner decision of 2026-10-10 (D6) moves the v1 half before the
tag: it is PR V1-9 of Stage V1, so it replaces "after everything else". Entry: V1-8 merged
(the `conformance` job exists). Work happens on `master` in the root module (v1 at the
repository root, paths relative to it). The compatibility base is the `master` commit `$TIP`,
taken after V1-7 and V1-8; the release is `v1.5.0` (`$REL`), on the owner's order at MV1, with
the same Milestones tag rule. V1-10 and V1-11 then finish the client on top of this stage; this
stage stays a behaviour-preserving extraction. The `api` freeze below is the contract at the
time it was written: the DoD compares with `$TIP`, which includes what V1-5 to V1-7 added to
`Conn` and `Namespace` (D10; this stage implements them on the client side as
*D10 methods and the alias* below says), so the listing there is the method sets minus those additions.

**Contract (v1).** Additive: `client` behaves as today's root `socketio.Client`. The root
`Client` and `NewClient` stay as a wrapper over it, their godoc starting a paragraph
`Deprecated: use client.Client.` and `Deprecated: use client.NewClient.` (full stop
included); no root export is removed, renamed or changed in behaviour, so the release is
a MINOR one. All PRs target `master` and change root-module files only. The tag is
created only on the owner's order; `CHANGELOG.md` entries (`### Added` naming the import
path `go-socket.io/client`, `### Deprecated` naming `Client` and `NewClient`) and the
release commit follow [`CONTRIBUTING.md`](../CONTRIBUTING.md#releases) when `v1.5.0` is cut.

**Layering and import rule.** The root imports `client` for the wrapper, so `client` must
not import the root. The connection types therefore move to the leaf: `client` defines
`Conn`, `Namespace`, `ErrEmptyAddr` and `ErrWriteBufferFull`, and the root declares them
as aliases (`type Conn = client.Conn`, `var ErrEmptyAddr = client.ErrEmptyAddr`, ...), so
handlers, `errors.Is` and the `ft.In(0).Name() == "Conn"` check in `handler.go` behave as
before. apidiff and `gorelease` report the alias move as incompatible although consumers
compile unchanged, so the DoD proves compatibility with a consumer program instead.
`client` may import any `engineio/...` package (`engineio/session` included), `parser` and
`logger`, and never the root package, adapters or Redis (a DoD line checks the closure);
only the root and the examples import `client`. It carries its own copy, taken from `$TIP`, of the
closure the old `Client` reaches (write queue, packet handlers, namespace connection,
handler dispatch, an unexported in-memory broadcast behind `Namespace.Join`, `Leave`,
`LeaveAll` and `Rooms`), without Redis and server-only code. The only server-path
logic edit deletes `clientConnectPacketHandler` and `clientDisconnectPacketHandler` (`make
lint` rejects unused code); the other root edits are the aliases, and `fmtNS`, used by the
server, stays in the root `client.go`. Cost: a root fix to a shared file names in its PR whether
`client/` needs the same change and makes it there.

**D10 methods and the alias.** Through the alias, every method D10 adds to `Conn` or `Namespace`
(V1-5 to V1-7 before this stage, V1-10, V1-11 and V1-15 after it) is in both method sets, so the
server's and the client's connection types implement all of them. A method with no client
meaning (request, handshake data, engine connection, broadcast except the sender, `Use`) is a
client-side stub returning the zero value, or, if it returns an error, an error V1-0 names; its
godoc says what a client connection returns, and so does V1-14's `### Changed` entry. A stub
without an error result is documented as a no-op (`Use` registers nothing) or as returning nil
on a client connection (the request and engine-connection accessors), and V1-0 lists each
stub's behaviour. Stubs, not a smaller `client.Conn` embedded by a larger root `Conn`: the
alias keeps one `Conn` type, so a handler written as `func(socketio.Conn, ...)` for today's
`Client` still receives the client's connection, which a split would make a different type.
This stub is the only server-only code `client` carries. A client feature with no server meaning
(reconnection, C3; the options of C4, C6) goes on `*client.Client`, never on `Conn`; one with a
meaning on both sides (`connected` of C8, the flags of C11) is a D10 method both implement. The
types in these signatures are moved by this stage from the root into `client` (or exist in
`engineio`, `parser`) and aliased in the root like `Conn`; V1-0 adds their names to the `go
doc -short ./client` line of the DoD below and to the freeze listing. At `$TIP` the root
declares `AckFunc` (K26, V1-7, in `connection.go`), `Emitter` (K14, K16, V1-6, in
`namespace_conn.go`; an interface, so the client's own implementation returns
`ErrServerOnly` where the server's broadcasts), `Handshake` (K7, K10, V1-7, in
`connection.go`) and the error `ErrServerOnly` (V1-6, root `errors.go`, the one declaration
of it in the plan), the files their contracts name, all four in the allow-list of the DoD
below; this stage moves the four into `client` and the root keeps each name as
an alias (`type AckFunc = client.AckFunc`, `type Emitter = client.Emitter`, `type Handshake
= client.Handshake`, `var ErrServerOnly = client.ErrServerOnly`), so the root's exported
var and type names at the stage head are those of `$TIP` and the `diff` line of the DoD
below holds by construction; `ServerNamespace`, `BroadcastOptions` and `AdapterFunc` appear
in no `Conn` or `Namespace` signature and stay in the root. `go doc -short` prints vars and
types in name order, so the DoD line below lists `ErrServerOnly` between `ErrEmptyAddr`
and `ErrWriteBufferFull`, and the three types in name order among `Client`, `Conn` and
`Namespace`. V1-10 and V1-11 then add `Options`, `New`, `Socket` and `ErrConnectTimeout`
to the package (the `### V1-10 contract` and `### V1-11 contract` of Stage V1), after this
DoD has run.

**`api` freeze (additive exported API of `client`).** Derived from `client.go`, `connection.go`,
`namespace_conn.go` and `errors.go` at `$TIP`; deliberately not an `api` fence, which the
Stage 1b allow-list reads. A difference from the root signature at `$TIP`, receiver and
parameter names included (`go doc -short` compares them), blocks the stage. The three
`var`s (`ErrEmptyAddr`, `ErrServerOnly`, `ErrWriteBufferFull`) are separate statements,
as at `$TIP`: `go doc -short` prints only the first of a grouped `var ( ... )` block, and
the export check would lose the others.

```text
var ErrEmptyAddr, var ErrServerOnly, var ErrWriteBufferFull
func NewClient(addr string, opts *engineio.Options) (*Client, error)
type Client struct (unexported fields), methods on *Client:
  Connect() error; Close() error; Emit(event string, args ...interface{})
  OnConnect(f func(Conn) error); OnDisconnect(f func(Conn, string))
  OnError(f func(Conn, error)); OnEvent(event string, f interface{})
type Conn interface and type Namespace interface: method sets exactly as at $TIP
type AckFunc, type Emitter (interface), type Handshake (struct): as the V1-0 contracts declare them
not exported: EmptyAddrErr (the root keeps it), Server, ServerNamespace, Broadcast, BroadcastOptions, AdapterFunc, EachFunc, RedisAdapterOptions
```

**Tests.** A test that reaches the unexported `dial`, `conn` or `createNamespace` of a
client can live only in package `client`, whose tests cannot import the root. Such a test
is split by side: its C half moves to `client/` as an in-package test with the same
function and subtest names (`TestX/C`) and `// Covers <id> (C)`; the root keeps the S half,
marked `(S)`. At `$TIP` that is `backpressure_test.go` and `integration_test.go`, with
their helpers copied into a `client/` test file. The exported-API-only `lifecycle_test.go`
and `lifecycle_errors_test.go` stay in the root and run through the wrapper; `client/`
gets copies in the external package `client_test` (it may import the root). The root adds `TestDeprecatedClientRoundTrip` (wrapper against a root
`Server`: connect, event, ACK, `Close` runs the server's `OnDisconnect`),
`TestConnAliasHandlers` (handlers written with `socketio.Conn` and `client.Conn` register
on `client.Client` and `Server`) and `TestClientErrorIdentity` (`errors.Is` across the root
and `client` errors; `EmptyAddrErr` still matches).

DoD, run with `bash` and `set -e` on the head of the Stage 7 PR (rules as in the Stage 1b
DoD; the consumer check needs network). `$TIP` is the `master` commit recorded in its body;
stage commits are titled `<type>(7.<n>): ...`, so later commits are not judged:

```sh
TIP=${TIP:?the master commit recorded in the Stage 7 PR}; MOD=github.com/sshaplygin/go-socket.io; T=$(mktemp -d); BASE=$T/base; R=$PWD; git worktree add -q --detach $BASE $TIP; trap 'git worktree remove --force $BASE; rm -rf $T' EXIT
make lint test-race
D=$(go list -deps ./client); test -z "$(echo "$D" | grep -E "redigo|^$MOD(/|$)" | grep -vE "^$MOD/(client|engineio|parser|logger)(/|$)")"   # D is not piped inside test -z: a failing go list (import cycle) must stop the script
test -n "$(go list -deps . | grep -x "$MOD/client")"; for X in Client NewClient; do test -n "$(go doc . $X | grep -E "^ *Deprecated: use client\.$X\.")"; done   # fails before the stage: no notices
cl() { awk '/^## Unreleased/{u=1;next} /^## /{u=0} u && /^### /{s=$2} u && s=="'$1'"' CHANGELOG.md; }; test -n "$(cl Added | grep 'go-socket.io/client')"; test -n "$(cl Deprecated | grep NewClient)"
test "$(go doc -short ./client | sed -E 's/^ +//; s/^(func [A-Za-z]+)\(.*/\1/; s/^((var|type) [A-Za-z]+).*/\1/' | paste -sd, -)" = "var ErrEmptyAddr,var ErrServerOnly,var ErrWriteBufferFull,type AckFunc,type Client,func NewClient,type Conn,type Emitter,type Handshake,type Namespace"
test "$(go doc -short ./client Client | grep '^func')" = "$(cd $BASE && go doc -short . Client | grep '^func')"; for X in Conn Namespace; do test "$(go doc ./client $X | grep -vE '^(package|    )|^\s*(//|$)')" = "$(cd $BASE && go doc . $X | grep -vE '^(package|    )|^\s*(//|$)')"; done
test -z "$(diff <(cd $BASE && go doc -short . | grep -oE '^(var|type) [A-Za-z]+' | sort) <(go doc -short . | grep -oE '^(var|type) [A-Za-z]+' | sort))"
S7=$(git log -E --no-merges --grep='^[a-z]+\(7\.' --format=%H $TIP..HEAD); test -n "$S7"
test -z "$(git show --format= --name-only $S7 | sort -u | grep -vE '^(client/|_examples/client/|(client|connection|connection_handlers|namespace_conn|errors)\.go$|CHANGELOG\.md$|CLAUDE\.md$)|_test\.go$')"
names() { grep -rhoE '^func (Test|Benchmark)[A-Za-z0-9_]+' --include='*_test.go' --exclude-dir=_examples $1 | sort -u; }; test -z "$(comm -23 <(names $BASE) <(names .))"
res() { (cd $1 && go test -count=1 -v ./... | awk '$1=="---" && $2=="PASS:"{gsub(/0x[0-9a-f]+/,"0x"); print $3}') | sort -u; }; test -z "$(comm -23 <(res $BASE) <(res .))"   # every passing test and subtest, TestX/C included, still passes (322 at 39f06fc)
test "$(go test -race -count=1 -v ./client | grep -c -- '--- PASS: .*/C ')" -ge "$(res $BASE | grep -c '^Test.*/C$')"   # the C halves run in client/ (15 at 39f06fc)
pairs() { (cd $1 && find . -name '*_test.go' -not -path './_examples/*' | xargs grep -hoE '^// Covers [0-9A-Za-z.-]+ \([SC, ]+\)' | awk '{s=$0; sub(/^[^(]*\(/,"",s); gsub(/[^SC]/,"",s); for(i=1;i<=length(s);i++)print $3, substr(s,i,1)}' | sort -u); }; test -n "$(pairs $BASE)"; test -z "$(comm -23 <(pairs $BASE) <(pairs .))"   # no (case, side) pair loses its marker (70 at 39f06fc)
go test -race -count=1 -json -run '^(TestDeprecatedClientRoundTrip|TestConnAliasHandlers|TestClientErrorIdentity)$' . >$T/new.json; test "$(jq -rs '[.[]|select(.Action=="pass" and .Test!=null and (.Test|contains("/")|not))|.Test]|sort|join(",")' $T/new.json)" = TestClientErrorIdentity,TestConnAliasHandlers,TestDeprecatedClientRoundTrip
grep -q '^| `client/` |' CLAUDE.md; grep -q "\"$MOD/client\"" _examples/client/main.go; test -z "$(grep -F "\"$MOD\"" _examples/client/main.go)"   # CLAUDE.md layout row (Stage 1b rule); the example imports client, not the root
# a root-API consumer must compile and run unchanged: `cclient .` builds it against this checkout, `cclient <tag>` against a tag
cclient() ( d=$(mktemp -d $T/c.XXXXXX); cd $d; go mod init example.com/consumer
cat >main.go <<'GO'
package main
import (
	"errors"
	"os"
	socketio "github.com/sshaplygin/go-socket.io"
	"github.com/sshaplygin/go-socket.io/client"
	"github.com/sshaplygin/go-socket.io/engineio"
)
var (
	_ func(string, *engineio.Options) (*socketio.Client, error) = socketio.NewClient
	_ func(*socketio.Client, func(socketio.Conn) error)         = (*socketio.Client).OnConnect
	_ func(*socketio.Client, func(socketio.Conn, string))       = (*socketio.Client).OnDisconnect
	_ func(*socketio.Client, func(socketio.Conn, error))        = (*socketio.Client).OnError
	_ func(*socketio.Client, string, interface{})               = (*socketio.Client).OnEvent
	_ func(*socketio.Client, string, ...interface{})            = (*socketio.Client).Emit
	_ func(*socketio.Client) error                              = (*socketio.Client).Connect
	_ func(*socketio.Client) error                              = (*socketio.Client).Close
	_ func(client.Conn, client.Namespace)                       = func(socketio.Conn, socketio.Namespace) {}
)
func main() {
	_, err := socketio.NewClient("", nil)
	if !errors.Is(err, socketio.ErrEmptyAddr) || !errors.Is(err, socketio.EmptyAddrErr) || !errors.Is(err, client.ErrEmptyAddr) ||
		!errors.Is(socketio.ErrWriteBufferFull, client.ErrWriteBufferFull) {
		os.Exit(1)
	}
}
GO
if [ "$1" = . ]; then go mod edit -replace $MOD=$R && go mod tidy; else GOPROXY=direct go get $MOD@$1; fi
go vet ./... && go run . )
cclient .
```

Acceptance: `_examples/client` imports `client` instead of the deprecated root `Client`
and `make examples` builds it; the owner runs it against `_examples/default-http` (the
login event is received); the tag is ordered at MV1. Tag-time gate, after that order and
the release commit: `cclient $REL` (the DoD function, tag in place of `.`) succeeds and
`git merge-base --is-ancestor $REL origin/master` holds; the release commit and the
package are both on `master`, so nothing is forward-ported. Out of scope: a
separate `go.mod`, removing the root `Client` (the v1 runtime stays at the root), any
change to the v1 server.

## Milestones

| Milestone | Content | Tag |
| --- | --- | --- |
| M0 | Stage 0 docs baseline | none |
| M1 | Stage 1 complete and the 1.D link-form commit merged | none |
| M1b | Stage 1b closed: step 0 done (branch `v1.x` cut) and steps 1–3 merged, with the Stage 1b DoD, `v1.x` gates and Acceptance blocks passing on `master` | none (first commits after the cut commit `$CUT`) |
| MV1 | Stage V1: PRs V1-0 to V1-20 merged on `master`, the MV1 DoD and Acceptance passing; includes the `client` package (Stage 7), the engine layer (D16), the examples and the D17 gates | `v1.5.0` (v1 line complete), on the owner's explicit order after the declaration of *Repository layout* |
| MV1R | Stage V1R: Redis parity on the v1 line | next v1 minor after `v1.5.0` (`v1.6.0` when that is the latest tag), on the owner's order |
| M2 | 2.0 generic API/lifecycle contract + 2.1 Engine.IO v4 on gobwas/ws + conformance; accepted at the 2B join, which also puts the 2.2 and 2.3P code on `master` unreleased (below) | branch `v2-next` (a snapshot of `master`; v2 paths under `v2/`) |
| M3 | 2.2 + 2.3 + 2.4 + 2.5 (2.2 and 2.3P are already on `master` at M2 acceptance; the M3 tags release them) | `v2.0.0`, `v2/contrib/otel/v2.0.0`, only after the owner's declaration and `v1.5.0` (*Repository layout*) |
| M4 | Stage 3: single-server chat | `v2.1.0`; the Stage 3 upstream-chat parity DoD line is no longer the `v1.5.0` trigger (*Repository layout*) |
| M5 | Stage 4b: adapters and cluster chat acceptance | `v2.2.0` first, then `v2/adapters/redis/v2.0.0`, `v2/adapters/nats/v2.0.0` |
| M6 | Stage 5: Admin UI observation and cluster administration | `v2.3.0`, `v2/contrib/admin/v2.0.0`; adapter minor releases |
| M7 | Stage 6: final comparative benchmark report and reproducible artifacts | report/artifact revision; no runtime release required |
| M8 | Stage 7: `client` package in the repository-root (v1) module on `master`, its root `Client` deprecated; merged as PR V1-9 inside Stage V1, so MV1 closes it and nothing waits for M7 | none of its own: released with MV1 as `v1.5.0` |

Every tag in the Tag column is created only on the owner's explicit order, after the
declaration of *Repository layout* and after `v1.5.0`; the column gives full git tag names
(forms in *Repository layout*), so the path convention does not shift them again.

**Tag-time checks.** No milestone waits for a tag. M3 and M5 are accepted on `master` before any
tag exists, with child modules built through `replace .../v2 => ../..` (as in `v2/_examples/*`). The
checks that need a created tag or a published module version run on the owner's order after the
tag, and each is labelled "at tag time" where its section states it: Stage 2.5 (`pkg.go.dev`
renders the tagged version; consumers against released versions without `replace`; the root tag
before dependent module tags), row 2F (publication verification), Stage 4b (root `v2.2.0` before
the adapters; consumers against published versions) and Stage 5 item 6 (the releases).

M2 is accepted when G2, the 2B join gate and the 2.1 exit have passed on one
reviewed `master` commit and `git ls-files 'v2/_experiments/*/go.mod'` prints nothing there
(every consumer in *Prepared components* has landed, so no leftover experiment is
allowed); the owner may not declare M2 otherwise. The 2B join gate also covers the 2.2 memory adapter and the
2.3P codec, so at acceptance that code is on `master` but unreleased until the M3 tags.
The owner's declaration is what makes the commit reviewed: the owner records its SHA
in the transfer ledger of [issue #2](https://github.com/sshaplygin/go-socket.io/issues/2)
and only then creates branch `v2-next` once from that SHA as a frozen snapshot. It
receives no PRs (the owner does not merge into it); topic PRs keep targeting `master`.
Check: `git ls-remote origin refs/heads/v2-next refs/heads/v2-dev` prints the ledger
SHA for `v2-next` and `063debc5f9f0b1658743d746ec323be3ee6b649d` for `v2-dev`.
No CI or Dependabot trigger is wired for it, so it is not a working branch unless a
roadmap step first adds the triggers. The
legacy `v2-dev` branch (2021) is unrelated and stays unchanged.

Re-estimate stage 2 after G2 and adapters after the shared codec/conformance fixtures.
The earlier 15–25 working-day estimate for stage 5 is provisional; measure the
snapshot/Node interoperability prototype before treating it as a delivery commitment.

## Out of scope

EIO=3 in v2; in v1: JSONP and a mixed Go/Node Redis cluster on the `socket.io-redis` wire format (Stage V1, D3 and D4); connection state recovery; WebTransport; permessage-deflate in v2 (v1 has it: V1-15, D11; a user moving from `v1.5.0` loses it, which `docs/MIGRATION.md` says, 2.5); sharded Redis
adapter (Redis 7 sharded pub/sub); Redis Cluster, Ring and replica-routed clients for
`v2/adapters/redis` (see 4b `adapters/redis`); cluster broadcast-with-ack; NATS JetStream persistence;
framework-specific integration packages (gin, echo, iris, gf use `http.Handler`); trace
context propagation through the Redis adapter.

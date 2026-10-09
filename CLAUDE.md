# Working on go-socket.io

Go implementation of a Socket.IO server. `master` holds the v2 work: the root package is an
API skeleton without runtime (stage 2.0), and the v1 server and client live on branch
`v1.x`. Module path is
`github.com/sshaplygin/go-socket.io` for v1; v2 adds `/v2` (see
[docs/ROADMAP.md](docs/ROADMAP.md)).

## Layout

| Path | Purpose |
| --- | --- |
| `*.go` (root, package `socketio`) | v2 API skeleton: typed events, `Server`/`Namespace`/`Socket`, `Adapter`, hooks, options; declarations only, signatures in [docs/API.md](docs/API.md) |
| `parser/` | Socket.IO packet encoder/decoder, binary attachments, the `Packet`/`Arguments` value types of the v2 API |
| `engineio/` | Engine.IO server: `Server`, `Conn`, options, observer hook types (`hooks.go`) |
| `engineio/client/` | Engine.IO client: `Dialer`, `Opener` |
| `engineio/session/` | sessions, session manager, ID generator |
| `engineio/frame/` | frame type (`frame.Type`, `frame.String`, `frame.Binary`) |
| `engineio/packet/` | Engine.IO packet encoder/decoder and the exported test fakes in `fake.go` |
| `engineio/payload/` | polling payload codec |
| `engineio/transport/` | transport interfaces and manager |
| `engineio/transport/polling/` | long-polling transport |
| `engineio/transport/websocket/` | websocket transport |
| `engineio/transport/utils/` | timestamp helper shared by the transports |
| `engineio/internal/` | what the engineio packages share without exporting it (the shutdown hook) |
| `engineio/internal/logtest/` | log recorder shared by the `engineio` and `engineio/client` tests |
| `logger/` | package-level `slog` fallback (`logger.Log`) for packages that cannot reach `engineio.Options.Logger`: parser, transports, `engineio/packet`, `engineio/client` |
| `internal/fixtures/` | compile-only packages built by the root fixtures: positive usage, an external adapter and a client stand-in; `testdata/negative/` holds the programs that must not compile |
| `_examples/` | runnable examples, each with its own `go.mod`; excluded from the root build by the `_` prefix |
| `_experiments/` | standalone prototypes, each with its own `go.mod`, never imported by the root module; built by `make experiments` |
| `docs/` | protocol notes, roadmap, v2 API signature inventory |

Runtime model: `engineio.Server` accepts HTTP requests and emits `engineio.Conn`
sessions. The root package has no runtime yet: every operation that needs one returns
`ErrNotImplemented`, and stages 2.1 to 2.4 of the roadmap add it.

## Commands

```sh
make lint       # gofmt -s check, go vet, golangci-lint (v2 config)
make test       # go test -count=1 ./...
make test-race  # the same with -race; what CI runs
make bench      # benchmarks only, no tests
make vuln       # govulncheck ./... in the root and in every _experiments module
make graph      # package graph: no import cycle, the root imports only engineio and parser (docs/API.md)
make cover      # coverage profile + HTML report
make examples   # check that every _examples/*/chat.go is identical; the legacy examples are not built until 2.5D
make experiments # check that _experiments stays standalone, then vet, gofmt -s, golangci-lint and race tests in every _experiments/*/ module
make all        # go install ./...
```

Requires Go 1.22+, golangci-lint v2 and govulncheck (`go install
golang.org/x/vuln/cmd/govulncheck@latest`), built with the newest stable Go: a
standard-library finding is cleared by upgrading the toolchain, not by code. Tests need
no external services.

CI (`.github/workflows/ci.yaml`) has five jobs: `lint` (tidy diff, mod verify, gofmt,
vet, `make graph`, golangci-lint, `make vuln` on ubuntu with the newest Go release from go.dev,
because `setup-go` lags behind it), `test` (race tests on
ubuntu, macos and windows with `stable` and `oldstable` Go), `min-go` (build and
race tests of the root module on ubuntu with the latest Go 1.22.x and
`GOTOOLCHAIN=local`, so it fails if `go.mod` or a dependency requires a newer Go),
`examples` (`make examples`, the identical-copy check only) and `experiments` (`make experiments` on ubuntu with `stable`;
installs golangci-lint with `go install`). Dependabot groups Go minor/patch and Actions updates weekly, also for the v1.x branch (target-branch).

Benchmarks (`.github/workflows/benchmarks.yml`) compare the PR base and head on
one Ubuntu runner with the same stable Go toolchain. Each benchmark runs ten times;
pinned `benchstat` reports timing and allocation deltas in the job summary, a
14-day artifact and one updated PR comment (same-repository PRs except Dependabot).
Forks and Dependabot retain the summary and artifact. Performance deltas are
advisory; build and benchmark failures fail the check.

The Go report formatter produces separate Markdown timing tables per package,
with median values, percentage changes and advisory markers at ±20% (using the
displayed, rounded percentage). The full
`benchstat` output, including allocations and statistical comparisons, is in a
collapsible section. Added/removed benchmarks and changes from a zero baseline
are marked not comparable. Validate rendering with
`go test ./.github/benchmarks/report`.

A small detection job runs on every PR update. It compares the previous head on
pushes, or the merge base on opening/reopening a PR (also the fallback if the
previous head is unavailable). Ordinary Go comments and formatting, documentation,
and `_examples/` changes skip the benchmark job. Go tokens, compiler/build/line
directives, cgo comments, dependencies, native sources, test fixtures and benchmark
automation changes trigger it. Cgo comments are retained conservatively because
they can contain C code. Validate the detector with `go test ./.github/benchmarks`.

## Conventions

- Go: `gofmt -s`, errors wrapped with `%w`, no panics in library code.
- Every fix carries a test that fails without it. Concurrency fixes are verified under
  `-race`.
- Public API changes go through `docs/ROADMAP.md` first.
- All text in the repository (docs, comments, commit messages, identifiers) is English.

## Roadmap validation

After editing `docs/ROADMAP.md`, launch three independent sub-agents against the
same saved revision: **QA** (testability, coverage and release gates), **Critic**
(feasibility, failure modes and unsupported assumptions), and **Reviewer**
(consistency, dependency/parallel-work graph, scope and duplication). They review
read-only and do not see each other's findings before submitting their own.

Resolve findings in the owning section; each requirement has one source of truth.
Ask affected reviewers to recheck fixes. If a fix changes scope, public contracts or
dependencies, all three recheck the final saved revision independently. Report
unresolved findings and each role's verdict; do not describe the plan as validated
while a blocking finding remains. If sub-agents are unavailable, state that this
validation step is incomplete.

## Documentation

One fact lives in exactly one file; other files link to it. Before adding text, find its
owner below and put it there.

| File | Owns | Must not contain |
| --- | --- | --- |
| `README.md` | what the library is, install, quick start, supported protocol/client versions, links | dev workflow, architecture, roadmap |
| `engineio/README.md` | what the engineio package is; links to README.md, docs/PROTOCOL.md and its godoc | install, examples, API usage |
| `CLAUDE.md` | repo layout, commands, conventions, this map | user-facing API docs, protocol details |
| `CONTRIBUTING.md` | PR process, review rules, release and tagging procedure | commands (link here) |
| `CHANGELOG.md` | released changes per tag | plans |
| `docs/ROADMAP.md` | planned stages, DoD, acceptance criteria, milestones, decisions | anything already released |
| `docs/API.md` | method-signature inventory of the v2 skeleton, package graph and its rules, compile fixtures | behaviour (ROADMAP, godoc) |
| `docs/PROTOCOL.md` | which parts of Engine.IO / Socket.IO protocols are implemented, deviations, upgrade sequence | API usage |
| `docs/MIGRATION.md` (from v2) | v1 → v2 API mapping | protocol |
| `docs/ADAPTERS.md` (from v2) | `Adapter` contract, thread-safety rules, conformance suite, shared message format | backend-specific options (`adapters/<name>/README.md`) |
| `_examples/README.md`, `_examples/*/README.md` | how to run the examples | library docs |
| godoc comments | public API reference, including ack and broadcast semantics | anything above |

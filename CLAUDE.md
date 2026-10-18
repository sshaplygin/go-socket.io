# Working on go-socket.io

Go implementation of a Socket.IO server. The repository holds two independent Go modules
(neither imports nor requires the other, and there is no `go.work`):

- the repository root is v1, `github.com/sshaplygin/go-socket.io`: the working server and
  experimental client for Socket.IO v4 over Engine.IO v3;
- `v2/` is v2, `github.com/sshaplygin/go-socket.io/v2`: the API skeleton without runtime
  (stage 2.0) and everything of stage 2 and later.

The branch `v1.x` is frozen at the tree the root was restored from and receives no new
work. Layout decisions, tag forms and the path convention of the roadmap:
[docs/ROADMAP.md](docs/ROADMAP.md#repository-layout).

## Layout

| Path | Purpose |
| --- | --- |
| `*.go` (root, package `socketio`) | v1 public Socket.IO Server lifecycle, namespace views, adapter factories, options and active role aliases |
| `internal/connection/` | private server transport connection, concrete namespace sockets, queue/drain, ACK/packet dispatch and emitters |
| `internal/namespace/` | canonical server role contracts, namespace handlers/listeners and registration/socket registries |
| `adapter/` | v1 adapter/socket contracts and local in-memory room index; standard-library dependencies only |
| `adapter/redis/` | v1 Redis factory/options, publication, requests and reconnect; composes the memory adapter |
| `client/` | v1 experimental Socket.IO Go client construction/options and active role/socket entry points |
| `client/internal/connection/` | private client manager/generations/reconnect, queues/drain, concrete sockets, ACK/packet dispatch and observers |
| `client/internal/namespace/` | canonical independent client-role contracts, reflected handlers/listeners, registry and local room index |
| `parser/` | v1 Socket.IO v4 packet encoder/decoder, binary attachments and independent frame types |
| `engineio/` | v1 Engine.IO v3 acceptance/server lifecycle, options/hooks and active connection contracts |
| `engineio/internal/session/` | private server session I/O, registry/IDs, heartbeat, close/upgrade and polling response observation |
| `engineio/client/` | independent v1 Engine.IO client, dialer, heartbeat/upgrade and client connection state/errors |
| `engineio/parser/` | shared Engine.IO v3 codec entry points, active wire-type aliases, OPEN parameters and fakes |
| `engineio/parser/internal/packet/` | canonical frame/packet types and packet codec |
| `engineio/parser/internal/payload/` | streaming payload codec, length framing and source/destination leases |
| `engineio/transport/` | transport contracts, registry and timestamp helper; polling owns HTTP feed/flush, pause and deadline state |
| `logger/` | v1 package-level `slog` fallback (`logger.Log`) for packages that do not receive per-instance `WithLogger` configuration: parser, transports, `engineio/parser`, client dialer |
| `internal/conformance/` | interoperability and raw protocol tests behind the `conformance` build tag; Node fixtures under `testdata/node/` |
| `internal/parity/` | portable PARITY marker and executable-test checks, including tagged tests without running Node; library/example process-termination source gate |
| `_examples/` | v1 runnable examples, each with its own `go.mod` except the Go client in `_examples/client`, which belongs to the root module; excluded from `./...` of the root build by the `_` prefix |
| `v2/*.go` (package `socketio`) | v2 API skeleton: typed events, `Server`/`Namespace`/`Socket`, `Adapter`, hooks, options; declarations only, signatures in [docs/API.md](docs/API.md) |
| `v2/parser/` | Socket.IO v5 wire codec (bounded `Encode`/`Decode`, `Assembler`, binary attachments, `JSON[T]` argument codec), the `Packet`/`Arguments` value types of the v2 API; `testdata/oracle` is the Node check (`README.md` there) |
| `v2/adapter/codec/` | message format of the broker adapters (Node Redis adapter 8.3.0): MessagePack broadcast, JSON requests and responses over codec-local wire types; imports `parser` and `vmihailenco/msgpack`, never the v2 root; Node-captured fixtures and oracle in `testdata/` |
| `v2/engineio/` | Engine.IO server: `Server`, `Conn`, options, observer hook types (`hooks.go`) |
| `v2/engineio/client/` | Engine.IO client: `Dialer`, `Opener` |
| `v2/engineio/session/` | sessions, session manager, ID generator |
| `v2/engineio/frame/` | frame type (`frame.Type`, `frame.String`, `frame.Binary`) |
| `v2/engineio/packet/` | Engine.IO packet encoder/decoder and the exported test fakes in `fake.go` |
| `v2/engineio/payload/` | Engine.IO v4 polling payload codec (`Decode`, `EncodeBatch`) and `Payload`, the pause/upgrade lifecycle between HTTP requests and a session |
| `v2/engineio/transport/` | transport interfaces and manager |
| `v2/engineio/transport/polling/` | long-polling transport |
| `v2/engineio/transport/websocket/` | websocket transport |
| `v2/engineio/transport/utils/` | timestamp helper shared by the transports |
| `v2/engineio/internal/` | what the engineio packages share without exporting it (the shutdown hook) |
| `v2/engineio/internal/logtest/` | log recorder shared by the `engineio` and `engineio/client` tests |
| `v2/logger/` | package-level `slog` fallback (`logger.Log`) for packages that cannot reach `engineio.Options.Logger`: transports, `engineio/packet`, `engineio/client` |
| `v2/internal/fixtures/` | compile-only packages built by the v2 root fixtures: positive usage, an external adapter and a client stand-in; `v2/testdata/negative/` holds the programs that must not compile |
| `v2/_examples/` | the examples written against the v1 API, each with its own `go.mod` requiring the v2 module; they do not build until 2.5D migrates them |
| `v2/_experiments/` | standalone prototypes, each with its own `go.mod`, never imported by the v2 module; built by `make -C v2 experiments` |
| `docs/` | shared by both modules: protocol notes, roadmap, v1 parity matrix (`PARITY.md`), v2 API signature inventory |
| `.github/` | shared: one CI workflow with jobs per module, the benchmark workflow and its tools, Dependabot |

Runtime model of v1: `engineio.Server` accepts HTTP requests and emits `engineio.Conn`
sessions; `socketio.Server.Serve` takes each session and starts three goroutines
(read, write, error) through `internal/connection`. Handlers registered with `OnEvent` are called on
the read goroutine of that connection, so a blocking handler blocks that client only.
The v2 root package has no runtime yet: every operation that needs one returns
`ErrNotImplemented`, and stages 2.1 to 2.4 of the roadmap add it.

V1 source files are grouped under their owning components:

| Files | Responsibility |
| --- | --- |
| Root `server.go`, `namespace.go`, `types.go`, `options.go`, `errors.go` | public server lifecycle/active tracking, stable namespace views/factories, active role/error aliases and configuration |
| `internal/connection/connection.go`, `io.go`, `queue.go`, `close.go` | concrete transport connection, startup/completion, I/O loops and atomic queue/drain/close lifecycle |
| `internal/connection/socket.go`, `emitter.go`, `packets.go`, `frames.go`, `errors.go` | concrete namespace sockets, pending emitters, packet/ACK dispatch, frame adaptation and connection errors |
| `internal/namespace/contracts.go`, `handlers.go`, `registry.go`, `listeners.go` | canonical server role contracts, reflected callbacks, namespace/connected registries and listener snapshots |
| `client/types.go`, `options.go`, `client.go`, `errors.go` | active client role/socket aliases, public construction/defaults/copies, functional options and error identities |
| `client/internal/connection/manager.go`, `reconnect.go`, `observers.go` | required resolved manager inputs, connection generations, retries and callback observers |
| `client/internal/connection/connection.go`, `io.go`, `queue.go`, `close.go`, `socket.go`, `emitter.go`, `packets.go`, `errors.go` | transport I/O, atomic queue/drain/close/ACK lifecycle, concrete sockets and emitters |
| `client/internal/namespace/contracts.go`, `handlers.go`, `registry.go`, `listeners.go`, `rooms.go` | canonical independent client contracts, reflected callbacks/listeners, synchronized registrations and local rooms |
| `adapter/adapter.go`, `memory.go` | minimal socket/factory contracts and local room/index/delivery ownership |
| `adapter/redis/options.go`, `adapter.go`, `codec.go`, `requests.go`, `subscription.go` | Redis configuration/construction, legacy wire format, cluster queries and subscription lifecycle |
| `engineio/conn.go`, `id.go`, `options.go`, `hooks.go`, `headers.go` | active connection/state/error/ID contract aliases, configuration, public hook adaptation and websocket header observation |
| `engineio/internal/session/session.go`, `close.go`, `upgrade.go`, `headers.go`, `hooks.go` | private session I/O, close/upgrade state machines and response observation |
| `engineio/internal/session/contracts.go`, `registry.go`, `id.go` | canonical server connection/state/error contracts, private registry and ID providers |
| `engineio/client/client.go`, `dialer.go`, `conn.go` | client Engine.IO I/O, dialing, heartbeat/upgrade and role-specific contracts |
| `engineio/parser/packet.go`, `payload.go`, `parameters.go`, `fake.go` | active codec aliases/constructors/options, OPEN JSON and frame/packet fakes |
| `engineio/parser/internal/packet/types.go`, `encoder.go`, `decoder.go` | canonical frame/packet values, framing interfaces and packet wire codec |
| `engineio/parser/internal/payload/encoder.go`, `decoder.go`, `length.go` | streaming payload encoding/decoding, leases and text/binary length framing |
| `engineio/transport/polling/payload.go`, `pauser.go`, `errors.go` | polling feed/flush ownership, pause/deadline state and operation errors |

The Engine.IO server installs observers through private session hooks and owns shutdown.
Root forced close calls `engineio.Abort`; custom engines receive their ordinary `Close`. There are no private init-time bridge
packages. Codec and transport import/API migration is documented in
[docs/V1_MIGRATION.md](docs/V1_MIGRATION.md); test-only `internal/conformance` and
`internal/parity` own their gates.

## Commands

Each module has its own `Makefile` and `.golangci.yml`. In the repository root (v1):

```sh
make lint        # gofmt -s check, go vet, golangci-lint (v2 config)
make sync-check  # compare shared v1/v2 files after the module import-path rewrite
make test        # go test -count=1 ./...
make test-race   # the same with -race; what CI runs
make conformance # tagged JS interoperability and raw transport tests; requires Node and the installs below
make test-stress # race tests of root, adapters, private server/client owners, parser, engineio/...: -count=5 -shuffle=on -cpu=1,4
make bench       # benchmarks only, no tests
make vuln        # govulncheck ./... in the root and in every _examples module
make cover       # coverage profile + HTML report
make parity-check # validate DONE rows and Covers markers; PARITY_STRICT=1 also checks open planned rows
make cover-check # cross-package statement floor and exported API zero-coverage ceiling
make examples    # build/vet example modules and Engine.IO echo, compare chat/lifecycle copies, race-test cleanup and chat
make examples-node # run each example client.js on a free port; requires CONFORMANCE_NODE_MODULES
make all         # go install ./...
```

In `v2/` (`make -C v2 <target>` from the root):

```sh
make lint       # gofmt -s check, go vet, golangci-lint (v2 config)
make test       # go test -count=1 ./...
make test-race  # the same with -race; what CI runs
make bench      # benchmarks only, no tests
make vuln       # govulncheck ./... in v2/ and in every _experiments module
make graph      # package graph: no import cycle, the v2 root imports only engineio and parser (docs/API.md)
make freeze     # G2 check: no unresolved marker in docs/API.md or the frozen declarations, no bare any in an exported frozen declaration
make g2         # the gate evidence of ROADMAP row 2A: make graph, make freeze and the compile fixtures
make cover      # coverage profile + HTML report
make examples   # check that every _examples/*/chat.go is identical; the legacy examples are not built until 2.5D
make experiments # check that _experiments stays standalone, then vet, gofmt -s, golangci-lint and race tests in every _experiments/*/ module
make all        # go install ./...
```

Requires Go 1.22+, golangci-lint v2 and govulncheck (`go install
golang.org/x/vuln/cmd/govulncheck@latest`), built with the newest stable Go: a
standard-library finding is cleared by upgrading the toolchain, not by code. Default tests
need no external services. The optional `conformance` tag needs Node, npm and the reference
packages, installed separately so the reference server's client cannot shadow the selected
test client:

```sh
npm install --no-save --prefix internal/conformance/testdata/node socket.io-client@1.7.4
npm install --no-save --prefix internal/conformance/testdata/node/server socket.io@2.5.0
export CONFORMANCE_NODE_MODULES="$PWD/internal/conformance/testdata/node/node_modules"
make conformance
```

Repeat with `socket.io-client@2.5.0` for the other required client. The module directory
must be absolute; tagged tests pass it unchanged as `NODE_PATH`. Missing Node or an absent
or relative `CONFORMANCE_NODE_MODULES` fails the scenarios. Listing tests with `-list`
does not require either.

CI (`.github/workflows/ci.yaml`) runs `sync-check` on every event without a module path
filter, comparing the pairs in `.github/sync-manifest`, and the required `conformance`
matrix with Node LTS and exact clients 1.7.4 and 2.5.0. The other jobs run per module. A `changes` job is the path
filter: on a pull request the v1 jobs run when a file outside `v2/` changed, the v2 jobs
when a file in `v2/`, `.github/` or `docs/API.md` (read by the v2 tests) changed; push and
schedule run both. v1 jobs, in the root: `lint-v1` (tidy diff, mod verify, gofmt, vet,
`make parity-check cover-check`,
golangci-lint, `make vuln` on ubuntu with the newest Go release from go.dev, because
`setup-go` lags behind it), `test-v1` (race tests on ubuntu, macos and windows with
`stable` and `oldstable` Go), `stress-v1` (`make test-stress`, pull requests only),
`min-go-v1` (build and race tests on ubuntu with the latest Go 1.22.x and
`GOTOOLCHAIN=local`, so it fails if `go.mod` or a dependency requires a newer Go) and
`examples-v1` (`make examples`). v2 jobs, with `working-directory: v2`: `lint-v2` (as
`lint-v1`, plus `make graph` and `make freeze`), `test-v2`, `min-go-v2`, `examples-v2`
(`make examples`, the identical-copy check only) and `experiments-v2` (`make experiments`;
installs golangci-lint with `go install`). Dependabot groups Go minor/patch updates of every
module (`/`, `/v2`, `/_examples/*`, `/v2/_examples/*`, `/v2/_experiments/*`) and Actions
updates weekly.

Benchmarks (`.github/workflows/benchmarks.yml`) compare the PR base and head on
one Ubuntu runner with the same stable Go toolchain. Each side runs every benchmark of
the root module and of `v2/` ten times into one file (the base `v2/` run is skipped when
the base has no `v2/go.mod`);
pinned `benchstat` reports timing and allocation deltas in the job summary, a
14-day artifact and one updated PR comment (same-repository PRs except Dependabot).
Forks and Dependabot retain the summary and artifact. Performance deltas are
advisory; build and benchmark failures fail the check. The summary and the comment are
rendered by the pinned `sshaplygin/benchmark-report` action with
`.github/benchmarks/benchmark-report.json`, one table per package headed by its full import
path (`github.com/sshaplygin/go-socket.io/v2/<pkg>` for the v2 module).

The in-repo Go report formatter (`.github/benchmarks/report`; the workflow runs only its
tests) produces separate Markdown timing tables per package
(a package of the v2 module is headed `v2/<pkg>`),
with median values, percentage changes and advisory markers at ±20% (using the
displayed, rounded percentage). The full
`benchstat` output, including allocations and statistical comparisons, is in a
collapsible section. Added/removed benchmarks and changes from a zero baseline
are marked not comparable. Validate rendering with
`go test ./.github/benchmarks/report`.

A small detection job runs on every PR update. It compares the previous head on
pushes, or the merge base on opening/reopening a PR (also the fallback if the
previous head is unavailable). Ordinary Go comments and formatting, documentation,
and `_examples/` and `_experiments/` changes (in either module) skip the benchmark job. Go tokens, compiler/build/line
directives, cgo comments, dependencies (`go.mod`, `go.sum` of either module), native sources, test fixtures and benchmark
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
| `README.md` | v1: what the library is, install, quick start, supported protocol/client versions, links; one link to `v2/README.md` | dev workflow, architecture, roadmap |
| `v2/README.md` | the same for the v2 module (what pkg.go.dev renders for it) | dev workflow, architecture, roadmap, the v1 quick start |
| `engineio/README.md`, `v2/engineio/README.md` | what the engineio package of that module is; links to its README.md, docs/PROTOCOL.md and its godoc | install, examples, API usage |
| `CLAUDE.md` | repo layout, commands, conventions, this map | user-facing API docs, protocol details |
| `CONTRIBUTING.md` | PR process, review rules, release and tagging procedure | commands (link here) |
| `CHANGELOG.md`, `v2/CHANGELOG.md` | released changes per tag of that module (v1 at the root, v2 in `v2/`); an entry is never repeated in the other | plans |
| `docs/ROADMAP.md` | planned stages, DoD, acceptance criteria, milestones, decisions | anything already released |
| `docs/API.md` | method-signature inventory of the v2 skeleton, the frozen-contract record, package graph and its rules, compile fixtures | behaviour (ROADMAP, godoc) |
| `docs/PARITY.md` | the v1 parity matrix against the Node reference (socket.io 2.5.0 / engine.io 3.6.2): per-row status, evidence, verification mark and plan (kind, PR, owner decision) | PR order, PR scope and release gates (ROADMAP), protocol description (PROTOCOL.md), API usage |
| `docs/PROTOCOL.md` | which parts of Engine.IO / Socket.IO protocols are implemented, deviations, upgrade sequence | API usage |
| `docs/V1_MIGRATION.md` | v1 component import/API/config/error migration mappings and examples | protocol, v2 migration |
| `docs/MIGRATION.md` (from v2) | v1 → v2 API mapping | protocol |
| `docs/ADAPTERS.md` (from v2) | `Adapter` contract, thread-safety rules, conformance suite, shared message format | backend-specific options (`v2/adapters/<name>/README.md`) |
| `v2/adapter/codec/testdata/README.md` | provenance of the Node adapter fixtures and the Go and Node commands that reproduce them | codec format (godoc) |
| `_examples/README.md`, `_examples/*/README.md`, the same under `v2/` | how to run the examples of that module | library docs |
| godoc comments | public API reference, including ack and broadcast semantics | anything above |

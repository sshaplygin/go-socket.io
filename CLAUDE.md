# Working on go-socket.io

The repository contains two independent Go modules: v1 at the root and v2 in
`v2/`. Neither imports or requires the other; there is no `go.work`. Both target
`master`. The `v1.x` branch receives no new work.

## Layout

The v1 package graph, responsibilities and runtime model are documented in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Public tests exercise consumers;
internal tests live beside the implementation they verify.

| Path | Purpose |
| --- | --- |
| `_examples/` | Runnable v1 applications; each has its own module except `_examples/client` |
| `engineio/_examples/` | Engine.IO echo application in the root module |
| `internal/conformance/` | Tagged interoperability tests and pinned Node fixtures |
| `internal/parity/` | Consumer compilation, role boundaries and library lifecycle policy |
| `.github/architecture/` | Runtime dependency and package-privacy checks |
| `.github/changes/` | CI and benchmark change classification |
| `.github/coverage/` | Cross-package coverage attribution |
| `.github/benchmarks/` | Benchmark comparison and reporting |
| `v2/` | Independent API and runtime work; contracts in `docs/API.md` |
| `v2/_experiments/` | Standalone prototypes, excluded from both runtime modules |

## Commands

Go 1.22+ is required. Linting uses golangci-lint v2; vulnerability checks use
`govulncheck`. Build those tools with a current Go toolchain.

```sh
make test               # root module tests, no external services
make test-race          # tests with the race detector
make test-stress        # concurrent packages, shuffled and repeated
make lint               # formatting, vet and golangci-lint
make vet                # Linux/macOS/Windows, conformance and example packages
make gates              # architecture, lifecycle and public consumer fixtures
make cover-check        # statement coverage floor and uncovered public functions
make sync-check         # compare the explicit pairs in .github/sync-manifest
make examples           # build/vet applications and test shared chat/lifecycle code
make bench              # benchmarks with allocation counts
make vuln               # scan the root and example modules
```

`PKGS=<patterns>` narrows `test`, `test-race`, `test-stress` and `bench`.
For v2, use `make -C v2 <target>`; `graph`, `freeze` and `fixtures` check its API
contract, and `experiments` validates its standalone modules. The v2 examples
are not built by its current `examples` target.

### Interoperability

`make conformance` runs the tagged suite with `-race`. Node, npm and a Redis
server are required. Install the client separately from the reference server
so the server's dependency cannot shadow the selected client:

```sh
npm install --no-save --prefix internal/conformance/testdata/node socket.io-client@1.7.4
npm install --no-save --prefix internal/conformance/testdata/node/server socket.io@2.5.0 socket.io-redis@5.4.0
export CONFORMANCE_NODE_MODULES="$PWD/internal/conformance/testdata/node/node_modules"
export CONFORMANCE_REDIS_ADDR=127.0.0.1:6379
make conformance
```

Repeat with `socket.io-client@2.5.0`. The module directory must be absolute.
Missing dependencies fail these tests. `make examples-node` also needs a Redis
Unix socket; see [_examples/README.md](_examples/README.md).

## CI

`.github/workflows/ci.yaml` tests the change classifier before selecting jobs.
Pull requests select affected packages and their reverse dependencies across
Linux, macOS, Windows and the conformance build tag. Source changes run tests;
ordinary comment/formatting changes run lint. Example changes select their
build and interoperability jobs. Module, workflow and unclassified changes
select a full run. Pushes to `master` and scheduled runs test both modules fully.

Markdown files consumed by checks select their gate jobs: the v1 architecture
contract and v2 API inventory. Other Markdown does not select runtime tests.
Gate tests run in their own job when selected package tests do not already cover
them. The architecture check runs both in the gate job and alongside selected
v1 package tests. Validate selection with `go test ./.github/changes`.

The v1 matrix includes stable/oldstable Go on Linux, macOS and Windows, race
tests, Go 1.22, repeated concurrency tests, coverage, examples and Node clients
1.7.4/2.5.0. Vulnerability checks use the newest Go release available to CI.
`.github/sync-manifest` is the complete list of files deliberately shared across
modules; other implementations evolve independently.

Benchmarks compare the PR base and head on the same runner and Go toolchain,
with ten samples per side. The pinned `sshaplygin/benchmark-report/report` action
validates the manifests and generates the report and offline replay bundle using
`.github/benchmarks/benchmark-report.json`. Results go to the job summary and an
artifact; same-repository PRs also receive an updated comment. Benchmark changes
do not fail CI based on timing thresholds. The workflow exercises the report
action with the collected benchmark samples.

## Conventions

- Use `gofmt -s`, wrap errors with `%w`, and return errors to callers. Library
  packages must not terminate the process. Executable entry points own exit status.
- Root, client and Engine.IO test packages fail when library goroutines remain
  after cleanup; fixtures must close clients and unblock callbacks.
- Reproduce a bug with a behavioral test; verify concurrency changes with `-race`.
  Test observable contracts, failure paths and boundaries. Avoid tests that only
  repeat implementation details or count documentation markers.
- Public API changes update the owning reference and consumer fixtures. Client
  APIs contain client operations; server-only operations belong to server types.
- Repository text is English. Commit and PR descriptions state the problem,
  resulting behavior and validation without generated footers or trailers.
- V1 documentation describes its current contract, defaults and limitations.
  Keep implementation chronology, completed stages and change logs in Git history.

## Documentation

Each fact has one owner; other documents link to it.

| File | Owns |
| --- | --- |
| `README.md` | V1 purpose, install, version compatibility and links to examples |
| `docs/V1.md` | V1 usage, lifecycle, adapters and typed events |
| `docs/PROTOCOL.md` | V1 wire behavior and interoperability limits |
| `docs/PARITY.md` | Reference implementations and behavioral test coverage |
| `docs/ARCHITECTURE.md` | V1 package boundaries and runtime model |
| `docs/V1_RELEASE_REVIEW.md` | Findings of the v1.5.0 release-readiness review, its work plan and progress |
| Godoc | Public signatures and detailed method contracts |
| `_examples/README.md` | Running and testing v1 examples |
| `CONTRIBUTING.md` | Contribution and release procedure |
| `CLAUDE.md` | Development commands and repository conventions |
| `v2/README.md`, `docs/V2_PROTOCOL.md` | V2 status and protocol behavior |
| `docs/API.md` | V2 API inventory and compile fixtures |
| `docs/ROADMAP.md` | Future v2 work and its acceptance criteria |
| `v2/CHANGELOG.md` | V2 changes |

# Working on go-socket.io

Go implementation of a Socket.IO server (and an experimental client). Module path is
`github.com/googollee/go-socket.io` until v2 (see [docs/ROADMAP.md](docs/ROADMAP.md)).

## Layout

| Path | Purpose |
| --- | --- |
| `*.go` (root, package `socketio`) | Socket.IO server, client, namespaces, rooms, in-memory and Redis broadcast |
| `parser/` | Socket.IO packet encoder/decoder, binary attachments |
| `engineio/` | Engine.IO server and client: sessions, polling and websocket transports, payload codec |
| `logger/` | package-level `slog` fallback (`logger.Log`) for packages that cannot reach `engineio.Options.Logger`: parser, transports, `engineio/packet`, client dialer |
| `_examples/` | runnable examples, each with its own `go.mod`; excluded from the root build by the `_` prefix |
| `docs/` | protocol notes and roadmap |

Runtime model: `engineio.Server` accepts HTTP requests and emits `engineio.Conn`
sessions; `socketio.Server.Serve` takes each session and starts three goroutines
(read, write, error) in `server.go`. Handlers registered with `OnEvent` are called on
the read goroutine of that connection, so a blocking handler blocks that client only.

## Commands

```sh
make lint       # gofmt -s check, go vet, golangci-lint (v2 config)
make test       # go test -count=1 ./...
make test-race  # the same with -race; what CI runs
make bench      # benchmarks only, no tests
make vuln       # govulncheck ./...
make cover      # coverage profile + HTML report
make examples   # go build in every _examples/*/ module
make all        # go install ./...
```

Requires Go 1.22+, golangci-lint v2 and govulncheck (`go install
golang.org/x/vuln/cmd/govulncheck@latest`). Tests need no external services.

CI (`.github/workflows/ci.yaml`) has three jobs: `lint` (tidy diff, mod verify, gofmt,
vet, golangci-lint, govulncheck on ubuntu), `test` (race tests and benchmarks on
ubuntu, macos and windows with `stable` and `oldstable` Go) and `examples`
(`make examples`). Dependabot groups Go minor/patch and Actions updates weekly.

## Conventions

- Go: `gofmt -s`, errors wrapped with `%w`, no panics in library code except handler
  registration with an invalid signature (`handler.go`).
- Every fix carries a test that fails without it. Concurrency fixes are verified under
  `-race`.
- Public API changes go through `docs/ROADMAP.md` first.
- All text in the repository (docs, comments, commit messages, identifiers) is English.

## Documentation

One fact lives in exactly one file; other files link to it. Before adding text, find its
owner below and put it there.

| File | Owns | Must not contain |
| --- | --- | --- |
| `README.md` | what the library is, install, quick start, supported protocol/client versions, links | dev workflow, architecture, roadmap |
| `CLAUDE.md` | repo layout, commands, conventions, this map | user-facing API docs, protocol details |
| `CONTRIBUTING.md` | PR process, review rules, release and tagging procedure | commands (link here) |
| `CHANGELOG.md` | released changes per tag | plans |
| `docs/ROADMAP.md` | planned stages, DoD, acceptance criteria, milestones, decisions | anything already released |
| `docs/PROTOCOL.md` | which parts of Engine.IO / Socket.IO protocols are implemented, deviations, upgrade sequence | API usage |
| `docs/MIGRATION.md` (from v2) | v1 → v2 API mapping | protocol |
| `docs/ADAPTERS.md` (from v2) | `Adapter` contract, thread-safety rules, conformance suite, shared message format | backend-specific options (`adapters/<name>/README.md`) |
| `_examples/README.md`, `_examples/*/README.md` | how to run the examples | library docs |
| godoc comments | public API reference, including ack and broadcast semantics | anything above |

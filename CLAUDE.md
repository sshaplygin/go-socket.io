# Working on go-socket.io

Go implementation of a Socket.IO server (and an experimental client). Module path is
`github.com/googollee/go-socket.io` until v2 (see [docs/ROADMAP.md](docs/ROADMAP.md)).

## Layout

| Path | Purpose |
| --- | --- |
| `*.go` (root, package `socketio`) | Socket.IO server, client, namespaces, rooms, in-memory and Redis broadcast |
| `parser/` | Socket.IO packet encoder/decoder, binary attachments |
| `engineio/` | Engine.IO server and client: sessions, polling and websocket transports, payload codec |
| `logger/` | package-level `slog` logger used by all packages |
| `_examples/` | runnable examples, each with its own `go.mod`; excluded from the root build by the `_` prefix |
| `docs/` | protocol notes and roadmap |

Runtime model: `engineio.Server` accepts HTTP requests and emits `engineio.Conn`
sessions; `socketio.Server.Serve` takes each session and starts three goroutines
(read, write, error) in `server.go`. Handlers registered with `OnEvent` are called on
the read goroutine of that connection, so a blocking handler blocks that client only.

## Commands

```sh
make lint      # golangci-lint run (requires golangci-lint v1.x, config is v1 format)
make test      # go test -v -race -count=1 ./...
make bench     # go test -bench . -benchmem ./...
make cover     # coverage profile + HTML report
make all       # go install ./...
```

Tests need no external services. Examples are built separately:

```sh
cd _examples/<name> && go build ./...
```

CI (`.github/workflows/ci.yaml`) runs gofmt, `go mod tidy` diff, `go mod verify`,
golangci-lint, `make test` and `make bench` on ubuntu, macos and windows with `stable`
and `oldstable` Go.

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

# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow SemVer.

## Unreleased

### Fixed

- engineio: a new session is registered before its OPEN packet is written, so a
  client that reuses the sid immediately no longer gets HTTP 400 "invalid sid";
  a session whose handshake fails is removed again (`engineio/server.go:173` at
  `61a7927`, roadmap task 1.S).
- `session.Manager.Count` takes the read lock instead of the write lock
  (`engineio/session/session_manager.go:50` at `61a7927`).
- `socketio.Server.Serve` returns nil instead of `io.EOF` after `Close`
  (`server.go:126` at `61a7927`).
- engineio: a request whose transport is earlier in the configured order than the
  session's current transport (for example polling after an upgrade to websocket) is
  answered with HTTP 400 instead of starting a second upgrade that held the request
  open until `pingTimeout`. This also removes a 60 s wait in `go test ./engineio`.

### Added

- `engineio.Options.Logger` (`*slog.Logger`): the Engine.IO server, its sessions, the
  socket.io `Server`, `Client` and every connection log through it; nil means
  `logger.Log`. The parser, the transports, `engineio/packet` and the client dialer still
  use the package-level `logger.Log` (roadmap stage 1.2).
- `SOCKETIO_LOG_LEVEL` (`error`, `warn`, `info`, `debug`, `trace`), read once at start
  into `logger.Level`: while set, it decides which library records are enabled,
  whatever the level of the application's handler. Applications can also call
  `logger.Level.Set` at runtime; `logger.LevelUnset` hands the decision back to the
  handler. `logger.Wrap`, `logger.LevelTrace` and `logger.ReplaceAttr` (renders
  `TRACE`) are exported. Connection and session records carry `sid`; session records
  also carry the current `transport`, updated on upgrade (roadmap stage 1.2a).
- `logger.Log` writes to whatever `slog.Default()` is at log time, so an application's
  `slog.SetDefault` in `main` applies to library records.

### Changed

- `session.New` takes a trailing `*slog.Logger` parameter (nil accepted).
- `logger.Error` accepts a nil error instead of panicking.
- Connection-level errors that were printed with `log.Println` are now `slog` Error
  records carrying the namespace. Messages logged by the Engine.IO session and the
  socket.io server, client and connection code lose their trailing colons; messages
  from the parser, the transports and the dialer are unchanged.
- Toolchain: `go 1.22` in `go.mod`; `golang.org/x/exp/slog` replaced by `log/slog`;
  `gofrs/uuid` replaced by `google/uuid`; `gorilla/websocket` 1.5.3; `testify` 1.12.1;
  `io/ioutil` replaced by `io` (roadmap stage 1.1).
- Lint: `.golangci.yml` migrated to the golangci-lint v2 schema with the standard
  linter set; `EmptyAddrErr` renamed to `ErrEmptyAddr` with the old name kept
  as a deprecated alias.
- Build: Makefile targets `test`, `test-race`, `bench`, `lint`, `vuln`, `cover`,
  `examples`; CI split into `lint`, `test` (3 OS × 2 Go) and `examples` jobs on
  current GitHub Actions; Dependabot updates grouped weekly.
- Examples: all `_examples/*` modules tidied; nine of them did not build before.
- Documentation baseline: English-only docs with a single owner per topic
  (`CLAUDE.md`, `docs/ROADMAP.md`, `docs/PROTOCOL.md`); `README.md` trimmed to
  purpose, compatibility, install and quick start; `upgrade workflow.md` merged into
  `docs/PROTOCOL.md`.

## v1.4.2 and earlier

See the upstream release notes at
<https://github.com/googollee/go-socket.io/releases>.

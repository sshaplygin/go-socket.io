# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow SemVer.

## Unreleased

### Fixed

- engineio: a request whose transport is earlier in the configured order than the
  session's current transport (for example polling after an upgrade to websocket) is
  answered with HTTP 400 instead of starting a second upgrade that held the request
  open until `pingTimeout`. This also removes a 60 s wait in `go test ./engineio`.

### Added

- `engineio.Options.Logger` (`*slog.Logger`): the Engine.IO server, its sessions, the
  socket.io `Server`, `Client` and every connection log through it; nil means
  `slog.Default()`. The parser, the transports and the client dialer still use the
  package-level `logger.Log` (roadmap stage 1.2).

### Changed

- `session.New` takes a trailing `*slog.Logger` parameter (nil accepted).
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

# go-socket.io v2

The v2 module of go-socket.io, `github.com/sshaplygin/go-socket.io/v2`: a
[Socket.IO](https://socket.io) server for Go with a typed event API, for Socket.IO
protocol v5 over Engine.IO protocol v4. Engine.IO is included as the `engineio`
sub-package.

**Status.** The v2 module is in development and has no runtime yet. Its root package
declares the typed v2 API (signatures in [docs/API.md](../docs/API.md)); operations
return `ErrNotImplemented`. The working v1 server and client are the module at the
repository root, `github.com/sshaplygin/go-socket.io`: see the [v1 README](../README.md).

This is a maintained fork of the archived `googollee/go-socket.io`. The plan for v2 is in
[docs/ROADMAP.md](../docs/ROADMAP.md).

![Build Status](https://github.com/sshaplygin/go-socket.io/workflows/CI/badge.svg)
[![GoDoc](https://pkg.go.dev/badge/github.com/sshaplygin/go-socket.io/v2.svg)](https://pkg.go.dev/github.com/sshaplygin/go-socket.io/v2)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue.svg)](../LICENSE)

## Compatibility

| Server | Socket.IO protocol | Engine.IO protocol | JavaScript client |
| --- | --- | --- | --- |
| v1 (repository root) | v4 | v3 | `socket.io-client` 1.x and 2.x |
| v2 (this module, in development, no runtime yet) | v5 | v4 | `socket.io-client` 3.x and 4.x |

Details and deviations: [docs/PROTOCOL.md](../docs/PROTOCOL.md).

## Install

No v2 release is tagged yet; follow `master`:

```sh
go get github.com/sshaplygin/go-socket.io/v2@master
```

The application examples in [_examples/](_examples/README.md) are written against the v1
API and do not build against this module until they are migrated. API reference:
[pkg.go.dev](https://pkg.go.dev/github.com/sshaplygin/go-socket.io/v2).

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for the PR process and [CLAUDE.md](../CLAUDE.md) for
how to build and test.

## License

BSD 3-Clause, see [LICENSE](../LICENSE).

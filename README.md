# go-socket.io

A [Socket.IO](https://socket.io) server and Go client with namespaces, rooms, acknowledgements,
broadcast and a Redis adapter for multi-instance deployments. Engine.IO is included as
the `engineio` sub-package and can be used on its own.

This is a maintained fork of `googollee/go-socket.io`.

![Build Status](https://github.com/sshaplygin/go-socket.io/workflows/CI/badge.svg)
[![GoDoc](https://pkg.go.dev/badge/github.com/sshaplygin/go-socket.io.svg)](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@v1.5.0)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue.svg)](LICENSE)

## Compatibility

| Server | Socket.IO protocol | Engine.IO protocol | JavaScript client |
| --- | --- | --- | --- |
| v1 (this module, repository root) | v4 | v3 | `socket.io-client` 1.x and 2.x |
| v2 (in development in [`v2/`](v2/README.md), no runtime yet) | v5 | v4 | `socket.io-client` 3.x and 4.x |

The v1 interoperability checks use `socket.io-client` **1.7.4** and **2.5.0**;
the reference server is Socket.IO 2.5.0 with Engine.IO 3.6.2.
V1 protocol details and deviations: [docs/PROTOCOL.md](docs/PROTOCOL.md).

## Install

Install the maintained fork directly:

```sh
go get github.com/sshaplygin/go-socket.io@v1.5.0
```

Go 1.22 or later is required.

## Examples

Runnable applications and launch instructions are in [_examples/](_examples/README.md).
Start with the [net/http server](_examples/default-http/) or the
[Go client](_examples/client/). The directory also includes ACKs, binary events,
namespaces, middleware, framework integrations, Redis and graceful shutdown.

## Usage

The Go client is available in
[`github.com/sshaplygin/go-socket.io/client`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@v1.5.0/client);
server and client use separate connection types and constructors.

Configuration, namespace lifecycle, adapters and typed events: [V1 usage](docs/V1.md).
API reference: [Socket.IO](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@v1.5.0)
and [Engine.IO](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@v1.5.0/engineio).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the PR process and [CLAUDE.md](CLAUDE.md) for
how to build and test.

## License

BSD 3-Clause, see [LICENSE](LICENSE).

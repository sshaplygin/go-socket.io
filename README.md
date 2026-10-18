# go-socket.io

A [Socket.IO](https://socket.io) server for Go with namespaces, rooms, acknowledgements,
broadcast and a Redis adapter for multi-instance deployments. Engine.IO is included as
the `engineio` sub-package and can be used on its own.

This is a maintained fork of the archived `googollee/go-socket.io`. The modernisation
plan, including Socket.IO protocol v5 support, is in [docs/ROADMAP.md](docs/ROADMAP.md).

![Build Status](https://github.com/sshaplygin/go-socket.io/workflows/CI/badge.svg)
[![GoDoc](https://pkg.go.dev/badge/github.com/sshaplygin/go-socket.io.svg)](https://pkg.go.dev/github.com/sshaplygin/go-socket.io)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue.svg)](LICENSE)

## Compatibility

| Server | Socket.IO protocol | Engine.IO protocol | JavaScript client |
| --- | --- | --- | --- |
| v1 (this module, repository root) | v4 | v3 | `socket.io-client` 1.x and 2.x |
| v2 (in development in [`v2/`](v2/README.md), no runtime yet) | v5 | v4 | `socket.io-client` 3.x and 4.x |

The v1 interoperability checks use `socket.io-client` **1.7.4** and **2.5.0**;
the reference server is Socket.IO 2.5.0 with Engine.IO 3.6.2.
Details and deviations: [docs/PROTOCOL.md](docs/PROTOCOL.md).

## Install

Install the maintained fork directly:

```sh
go get github.com/sshaplygin/go-socket.io@master
```

Earlier tags use the upstream module path; use `master` until a release with the
fork's module path is tagged. Existing consumers must update their imports from
`github.com/googollee/go-socket.io` to `github.com/sshaplygin/go-socket.io` and remove
the former upstream-path `replace` directive, then run `go mod tidy`.
Changes to v1 component imports and API: [migration guide](docs/V1_MIGRATION.md).

## Quick start

```go
package main

import (
    "log"
    "net/http"

    socketio "github.com/sshaplygin/go-socket.io"
)

func main() {
    server := socketio.NewServer(nil)

    server.OnConnect("/", func(s socketio.Conn) error {
        s.Join("lobby")
        return nil
    })
    // Return values are sent back to the client as the acknowledgement.
    server.OnEvent("/", "msg", func(s socketio.Conn, msg string) string {
        server.BroadcastToRoom("/", "lobby", "msg", msg)
        return "ok"
    })
    server.OnDisconnect("/", func(s socketio.Conn, reason string) {
        log.Println("closed:", reason)
    })

    go func() { log.Fatal(server.Serve()) }()
    defer server.Close()

    http.Handle("/socket.io/", server)
    log.Fatal(http.ListenAndServe(":8000", nil))
}
```

Runnable examples for gin, echo, iris, gf, CORS, Redis and graceful shutdown are in
[_examples/](_examples/README.md). API reference: [pkg.go.dev](https://pkg.go.dev/github.com/sshaplygin/go-socket.io).

## Go client and v1 migration

The Go client is available in
[`github.com/sshaplygin/go-socket.io/client`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io/client);
see the [client example](_examples/client/main.go). Server and client use separate
connection types and constructors.

Existing v1 applications, custom adapters and mocks should read the
[v1 component migration guide](docs/V1_MIGRATION.md) and
[Unreleased behavior changes](CHANGELOG.md#changed) before upgrading: public
interfaces gained methods, and heartbeat, namespace identity, disconnect, CORS,
polling compression and cookie defaults changed.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the PR process and [CLAUDE.md](CLAUDE.md) for
how to build and test.

## License

BSD 3-Clause, see [LICENSE](LICENSE).

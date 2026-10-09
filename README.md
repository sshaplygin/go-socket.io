# go-socket.io

A [Socket.IO](https://socket.io) server for Go with namespaces, rooms, acknowledgements,
broadcast and a Redis adapter for multi-instance deployments. Engine.IO is included as
the `engineio` sub-package and can be used on its own.

**Status of `master`.** This branch is the v2 work. Its root package declares the typed
v2 API (signatures in [docs/API.md](docs/API.md)) and has no runtime yet: operations
return `ErrNotImplemented`. The working v1 server and client, described below, are on the
branch `v1.x`.

This is a maintained fork of the archived `googollee/go-socket.io`. The modernisation
plan, including Socket.IO protocol v5 support, is in [docs/ROADMAP.md](docs/ROADMAP.md).

![Build Status](https://github.com/sshaplygin/go-socket.io/workflows/CI/badge.svg)
[![GoDoc](https://pkg.go.dev/badge/github.com/sshaplygin/go-socket.io.svg)](https://pkg.go.dev/github.com/sshaplygin/go-socket.io)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue.svg)](LICENSE)

## Compatibility

| Server | Socket.IO protocol | Engine.IO protocol | JavaScript client |
| --- | --- | --- | --- |
| v1.x (branch `v1.x`) | v4 | v3 | `socket.io-client` 1.x and 2.x |
| v2 (in development on `master`, no runtime yet) | v5 | v4 | `socket.io-client` 3.x and 4.x |

Details and deviations: [docs/PROTOCOL.md](docs/PROTOCOL.md).

## Install

Install the maintained fork directly:

```sh
go get github.com/sshaplygin/go-socket.io@v1.x
```

Earlier tags use the upstream module path; use the `v1.x` branch until a release with the
fork's module path is tagged. Existing consumers must update their imports from
`github.com/googollee/go-socket.io` to `github.com/sshaplygin/go-socket.io` and remove
the former upstream-path `replace` directive, then run `go mod tidy`.

## Quick start

The v1 API, on the branch `v1.x`:

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

Runnable v1 examples for gin, echo, iris, gf, CORS, Redis and graceful shutdown are in
[_examples/](_examples/README.md); on `master` they no longer build until they are migrated. API reference: [pkg.go.dev](https://pkg.go.dev/github.com/sshaplygin/go-socket.io).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the PR process and [CLAUDE.md](CLAUDE.md) for
how to build and test.

## License

BSD 3-Clause, see [LICENSE](LICENSE).

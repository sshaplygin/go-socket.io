# go-socket.io

A [Socket.IO](https://socket.io) server for Go with namespaces, rooms, acknowledgements,
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

The v1 interoperability checks use `socket.io-client` **1.7.4** and **2.5.0**;
the reference server is Socket.IO 2.5.0 with Engine.IO 3.6.2.
Details and deviations: [docs/PROTOCOL.md](docs/PROTOCOL.md).

## Install

Install the maintained fork directly:

```sh
go get github.com/sshaplygin/go-socket.io@v1.5.0
```

Go 1.22 or later is required.

## Quick start

```go
package main

import (
    "context"
    "errors"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    socketio "github.com/sshaplygin/go-socket.io"
)

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    if err := run(ctx); err != nil {
        log.Print(err)
        stop()
        os.Exit(1)
    }
}

func run(ctx context.Context) error {
    server := socketio.NewServer()

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

    mux := http.NewServeMux()
    mux.Handle("/socket.io/", server)
    httpServer := &http.Server{Addr: ":8000", Handler: mux}
    results := make(chan error, 2)
    go func() { results <- server.Serve() }()
    go func() {
        err := httpServer.ListenAndServe()
        if errors.Is(err, http.ErrServerClosed) {
            err = nil
        }
        results <- err
    }()
    var err error
    remaining := 2
    select {
    case err = <-results:
        remaining--
    case <-ctx.Done():
    }
    err = errors.Join(err, server.Close())
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    err = errors.Join(err, httpServer.Shutdown(shutdownCtx), httpServer.Close())
    for ; remaining > 0; remaining-- {
        err = errors.Join(err, <-results)
    }
    return err
}
```

Runnable examples for gin, echo, iris, gf, CORS, Redis and graceful shutdown are in
[_examples/](_examples/README.md). API reference: [pkg.go.dev](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@v1.5.0).

## Usage

The Go client is available in
[`github.com/sshaplygin/go-socket.io/client`](https://pkg.go.dev/github.com/sshaplygin/go-socket.io@v1.5.0/client);
see the [client example](_examples/client/main.go). Server and client use separate
connection types and constructors.

Configuration, namespace lifecycle, adapters and typed events: [V1 usage](docs/V1.md).
For Socket.IO protocol v5, see the independent [v2 module](v2/README.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the PR process and [CLAUDE.md](CLAUDE.md) for
how to build and test.

## License

BSD 3-Clause, see [LICENSE](LICENSE).

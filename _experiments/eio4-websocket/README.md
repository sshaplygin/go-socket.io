# Gobwas WebSocket framing experiment

This standalone Go module prepares the RFC 6455 message layer for roadmap stage
2.1 without changing the v1 server, its module dependencies or public API. The
root `go test ./...` does not discover this module; run its checks explicitly:

```sh
cd _experiments/eio4-websocket
go test -race -count=1 -cover ./...
go vet ./...
npm ci --ignore-scripts --no-audit --no-fund --prefix reference
npm test --prefix reference
```

Go tests use in-memory connections and need no Node process or external service.
The optional Node oracle builds `cmd/echo`, starts it on an ephemeral loopback
port, exercises it with pinned `ws@8.18.3`, then removes the binary and stops the
process. Its dependency integrity is recorded in `reference/package-lock.json`.

`Conn` uses `gobwas/ws@v1.4.0` with `wsutil.Reader` and `wsutil.Writer`. It enforces
client/server masking, reassembles fragments, validates UTF-8 across fragments,
handles ping/pong/close and serializes data and control writes with one mutex.
The byte limit applies to the complete data message, before buffering each
fragment; control-frame bytes do not count towards it. Reads return owned data
and discard partial messages on failure. Callers set network deadlines; `Close`
interrupts blocked reads and writes. A pending control reply can wait behind a
data write, so callers need a finite write deadline.

The [gobwas reader](https://github.com/gobwas/ws/blob/v1.4.0/wsutil/reader.go)
provides the fragmentation callbacks; the
[Node ws API](https://github.com/websockets/ws/blob/8.18.3/doc/ws.md) supplies an
independent peer for handshake, framing, Unicode, empty messages, interleaved
ping/pong, size limits, invalid input and normal close tests. Go tests also cover
both endpoint roles, truncated data, concurrent writers and interrupted reads.

This is a complete-message prototype, not the live Engine.IO transport. It does
not yet adapt `FrameReader`/`FrameWriter`, use `ws.Dialer`, preserve transport
options, implement Engine.IO handshake/heartbeat/upgrade or run the Engine.IO
conformance suite. On local protocol violations it closes TCP without a close
status frame (Node observes 1006); a received valid close is acknowledged with
its code. Unlike Node `ws`, `wsutil.ControlFrameHandler` omits the reason from
that reply, while returning it locally in `wsutil.ClosedError`. The oracle checks
this distinction explicitly. No compression is negotiated. The echo server's 64-byte limit and ten-second
connection deadline are test settings. Follow the stage 1b prerequisite before
integrating this into the v2 server.

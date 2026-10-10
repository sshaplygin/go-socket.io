# Examples

**Status in the v2 module.** These examples are written against the v1 server, which
stage 2.0 removed from the v2 root package. The v1 server is the module at the repository
root, and its examples, which build and run, are in the root
[`_examples/`](../../_examples/README.md). The copies here do not build against the v2
module and no CI job builds or tests them; `make examples` in `v2/` only checks that every
`chat.go` is identical. Stage 2.5D migrates them to the v2 API, and the Redis examples
return with the Redis adapter in stage 4b.

Directories with a `go.mod` are standalone Go modules. Each carries a `replace`
directive pointing at `v2/`, the v2 module root, so it builds against the checked-out
fork. The `client` example uses the v2 module.

```sh
cd _examples/<name>
go run .
```

Every example serves the same chat, a port of the
[Socket.IO chat example](https://github.com/socketio/socket.io/tree/main/examples/chat)
(MIT license, pinned commit in the file headers), so an example differs from the others
only in what it demonstrates: mounting, CORS, pprof, shutdown, Redis or Docker.

- **Server.** `chat.go` registers the chat on namespace `/` through `registerChat(server)`
  with the events and payloads of upstream `index.js`. The modules are separate, so the
  file is copied into each; `make examples` fails when a copy differs from
  `default-http/chat.go`. Change all copies together.
- **Page.** `asset/` holds the upstream `index.html`, `main.js` and `style.css`
  (license: `asset/LICENSE-socket.io-chat`), which the browser examples mount as
  `../asset`; the Docker images copy it from the repository root. The page loads
  `socket.io-client` 2.5.0 from jsDelivr with an integrity hash (this server speaks
  Engine.IO v3; cdn.socket.io has no 2.5.0 file), so it needs network access. Open <http://localhost:8000> in two tabs after starting a server.
- **Test.** `default-http/chat_test.go` checks the chat logic with the Go client;
  in the v1 copy at the repository root, `make examples` runs it with `-race`.

| Directory | Shows |
| --- | --- |
| `default-http` | `net/http` server and CORS-permissive transports |
| `dockerize-default-http` | the same server in a Docker image (`make -i`) |
| `gin-gonic`, `gin-cors` | mounting on gin, with and without CORS middleware; page at `/public/` |
| `go-echo` | mounting on echo |
| `iris` | mounting on iris |
| `gf` | mounting on GoFrame; it serves only the socket endpoint, not the page |
| `graceful-shutdown` | closing the server on SIGTERM |
| `pprof` | exposing `net/http/pprof` next to the socket server |
| `redis-adapter` | the Redis adapter over TCP; page at `/public/` |
| `redis-adapter-unix-socket` | the same through a Unix socket, in Docker; port 8080, page at `/public/` |
| `client` | the experimental Go client joining the chat of another example |

The Redis examples expect a Redis server on `127.0.0.1:6379` or the Unix socket path set
in `main.go`. `Server.ForEach`, which the chat uses to reach the other users, visits only
the connections of its own instance, so with two instances behind Redis, users on
different instances do not see each other's events and each instance counts only its own
users. The chat works inside one instance.

`client` joins the chat of a running example and prints what it receives, for a second:

```sh
go run ./_examples/client -addr http://127.0.0.1:8000 -name gopher   # from v2/
```

The Go client sends no close packet, so the server drops its session, and announces
`user left`, only after the ping timeout.

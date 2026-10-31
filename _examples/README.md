# Examples

Directories with a `go.mod` are standalone Go modules. Each carries a `replace`
directive pointing at the repository root, so it builds against the checked-out
fork. The `client` example uses the root module. Framework examples (`gf`,
`gin-cors`, `gin-gonic`, `go-echo`, `iris` and both Redis examples) require Go
1.26 or newer for their current dependencies; the root library remains compatible
with Go 1.22. The GoFrame example intentionally stays on v1.16.5 to exercise the
Redis adapter with redigo v2.0.0+incompatible.

```sh
cd _examples/<name>
go run .
```

The mounting examples serve the same chat, a port of the
[Socket.IO chat example](https://github.com/socketio/socket.io/tree/main/examples/chat)
(MIT license, pinned commit in the file headers), so an example differs from the others
only in what it demonstrates: mounting, CORS, pprof, shutdown, Redis or Docker.
The `ack`, `binary`, `namespaces` and `middleware` directories demonstrate
individual protocol features.

- **Server.** `chat.go` registers the chat on namespace `/` through `registerChat(server)`
  with the events and payloads of upstream `index.js`. The modules are separate, so the
  file is copied into each chat example; `make examples` compares all copies
  with `default-http/chat.go`.
- **Page.** `asset/` holds the upstream `index.html`, `main.js` and `style.css`
  (license: `asset/LICENSE-socket.io-chat`), which the browser examples mount as
  `../asset`; the Docker images copy it from the repository root. The page loads
  `socket.io-client` 2.5.0 from jsDelivr with an integrity hash (this server speaks
  Engine.IO v3; cdn.socket.io has no 2.5.0 file), so it needs network access. Open <http://localhost:8000> in two tabs after starting a server.
  For client 1.7.4 open <http://localhost:8000/index-1.7.html>; the 2.5.0 page is
  `index.html`. Under a mount prefix such as `/public/`, use
  `/public/index-1.7.html` and `/public/index.html`.
- **Test.** `default-http/chat_test.go` checks the chat logic with the Go client;
  `make examples` runs it with `-race`.

| Directory | Shows |
| --- | --- |
| `ack` | acknowledgements through return values and `AckFunc` |
| `binary` | binary events and ACKs in both directions |
| `namespaces` | namespace authentication, room isolation and broadcasts excluding the sender |
| `middleware` | connection and event rejection while other sockets remain usable |
| `default-http` | `net/http` server with default (permissive) CORS |
| `dockerize-default-http` | the same server in a Docker image (`make -i`) |
| `gin-gonic`, `gin-cors` | mounting on gin, with and without CORS middleware; page at `/public/` |
| `go-echo` | mounting on echo |
| `iris` | mounting on iris |
| `gf` | mounting on GoFrame; it serves only the socket endpoint, not the page |
| `graceful-shutdown` | closing the server on SIGTERM |
| `pprof` | exposing `net/http/pprof` next to the socket server |
| `redis-adapter` | the Redis adapter over TCP; page at `/public/` |
| `redis-adapter-unix-socket` | the same through a Unix socket, in Docker; port 8080, page at `/public/` |
| `client` | the Go client joining the chat of another example |

`client` joins the chat of a running example and prints what it receives, for a second:

```sh
go run ./_examples/client -addr http://127.0.0.1:8000 -name gopher   # from the repository root
```

`Close` returns without waiting, and the example exits right after it, so the
namespace DISCONNECT can be lost. Other chat users still receive `user left`: the
server runs the namespace's disconnect handler when the client's DISCONNECT
arrives, or when the Engine.IO session ends on the client's CLOSE packet, on a
closed connection, or at the latest at the server's ping timeout. A program that
must deliver its last events waits for their ACKs before it exits.

All server examples accept `-addr` (default `:8000`, except the Unix Redis example
at `:8080`); pprof also accepts `-debug-addr` (default `:8001`). The examples own their HTTP servers, report service failures back to
`run`, close Socket.IO, drain active HTTP requests with a five-second shutdown
bound, and wait for serving goroutines before returning. Startup or serving
errors produce a nonzero exit status after cleanup.
SIGINT/SIGTERM follows
the same cleanup; `graceful-shutdown` keeps its three-second signal delay but stops
immediately on a service failure. GoFrame is mounted as an HTTP handler because
its built-in runner can terminate the process. Each standalone module carries the
same private `lifecycle.go`; `make examples` compares these copies.

`make examples` builds and vets every standalone module and the Engine.IO echo
example (`engineio/_examples`), checks the shared chat/lifecycle files and race-tests
chat, service-failure cleanup and echo I/O cleanup. The executable Node clients check ACKs, binary buffers, namespace
authentication, room isolation, event middleware, and chat broadcasts excluding
the sender. Start Redis with TCP and Unix socket listeners, then install either
supported client into a temporary prefix and run from the repository root:

```sh
d=$(mktemp -d)
npm install --no-save --prefix "$d" socket.io-client@2.5.0
CONFORMANCE_NODE_MODULES="$d/node_modules" \
CONFORMANCE_REDIS_ADDR=127.0.0.1:6379 \
CONFORMANCE_REDIS_SOCKET=/path/to/redis.sock make examples-node
```

Use `socket.io-client@1.7.4` in the same command for the other supported client.
The target requires `CONFORMANCE_NODE_MODULES`, starts each example with a
random local port, and fails on a missing dependency or any client failure.
Both pinned clients run in CI.

### Redis cluster examples

The TCP and Unix-socket Redis examples use the same chat handlers as the other
examples. Registered-user counts come from the adapter's cluster room snapshot;
concurrent joins/leaves are not an atomic cluster-wide counter. Both processes
must use the same Redis address and channel prefix. For example, run these from
`_examples/redis-adapter` in separate terminals:

```sh
go run . -addr :8000 -redis 127.0.0.1:6379 -prefix chat-demo
go run . -addr :8001 -redis 127.0.0.1:6379 -prefix chat-demo
```

Open `/public/` on each HTTP port. For `_examples/redis-adapter-unix-socket`, pass
`-redis /path/to/redis.sock` instead. Redis Pub/Sub is shared across database
numbers; use a distinct prefix to isolate unrelated applications.

The Node example gate also runs clients against two processes for each Redis
transport. Set `CONFORMANCE_REDIS_ADDR` and `CONFORMANCE_REDIS_SOCKET` to a running
Redis TCP endpoint and Unix socket before `make examples-node`. CI supplies both.
The mixed Go/Node adapter gate is `make conformance`, using the same TCP endpoint
and `socket.io-redis@5.4.0` installed beside `socket.io@2.5.0` in
`internal/conformance/testdata/node/server`.

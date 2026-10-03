# Examples

Directories with a `go.mod` are standalone Go modules. Each carries a `replace`
directive pointing at the repository root, so it builds against the checked-out
fork. The `client` example uses the root module.

```sh
cd _examples/<name>
go run .
```

Browser examples serve `_examples/asset/index.html`, which bundles `socket.io-client`
1.2. Open <http://localhost:8000> after starting the server.

| Directory | Shows |
| --- | --- |
| `default-http` | `net/http` server, namespaces, acks, CORS-permissive transports |
| `dockerize-default-http` | the same server in a Docker image |
| `gin-gonic`, `gin-cors` | mounting on gin, with and without CORS middleware |
| `go-echo` | mounting on echo |
| `iris` | mounting on iris |
| `gf` | mounting on GoFrame |
| `graceful-shutdown` | closing the server on SIGTERM |
| `pprof` | exposing `net/http/pprof` next to the socket server |
| `redis-adapter` | two instances sharing rooms through Redis over TCP |
| `redis-adapter-unix-socket` | the same through a Unix socket |
| `client` | the experimental Go client talking to `default-http` |

Redis examples expect a Redis server on `127.0.0.1:6379` or the Unix socket path set
in `main.go`.

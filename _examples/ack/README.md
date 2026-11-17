# Ack example

Acknowledgements from a return value and a `socketio.AckFunc` called from another goroutine.

`sum` takes two integers and acknowledges their sum. `deferred` takes a string
and acknowledges `received: <value>` from another goroutine.

Run the server from this directory:

```sh
go run . -addr 127.0.0.1:8000
```

The module uses the repository checkout through its `replace` directive.
`client.js` checks the example over polling and websocket with either supported
Node client, `socket.io-client@1.7.4` or `socket.io-client@2.5.0`. With one of these
installed in an external prefix, run:

```sh
NODE_PATH=/path/to/node_modules node client.js http://127.0.0.1:8000
```

A successful run exits zero; a failed assertion, connection error or timeout
exits nonzero. With Redis listening on TCP and a Unix socket, run this from the
repository root to build and check every executable example on a free local port:

```sh
CONFORMANCE_NODE_MODULES=/path/to/node_modules \
CONFORMANCE_REDIS_ADDR=127.0.0.1:6379 \
CONFORMANCE_REDIS_SOCKET=/path/to/redis.sock make examples-node
```
 See [the examples index](../README.md) for the other examples.

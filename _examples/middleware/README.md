# Namespace and event middleware

`/secure` registers two namespace middleware functions with `Use`: the first
checks `query.token` against `demo-secret`, and the second stores `query.name`
in the connection context. They run before `OnConnect`. A rejected connection
receives `error: unauthorized`; the root namespace remains usable.

`OnConnect` adds per-socket event middleware with `Conn.Use`. It rejects the
`blocked` event with `error: event blocked`, leaving the socket connected.
The `echo` handler acknowledges `<name>: <message>` using the saved context.
The root namespace's `health` event acknowledges `pong`.

Run from this directory:

```sh
go run . -addr 127.0.0.1:8000
```

The module uses the repository checkout through `replace`. `client.js` checks
connection rejection, the unaffected root namespace, event rejection without
calling its handler, and a subsequent successful event, over polling and
websocket. With `socket.io-client@1.7.4` or `socket.io-client@2.5.0` installed
in an external prefix:

```sh
NODE_PATH=/path/to/node_modules node client.js http://127.0.0.1:8000
```

Success exits zero; connection failures, assertions and the 10 second deadline
exit nonzero. Run every executable example on a free port from the root:

```sh
CONFORMANCE_NODE_MODULES=/path/to/node_modules make examples-node
```

See [the examples index](../README.md).

# Namespaces, rooms and authentication

The `/chat` namespace authenticates `token=demo-secret` through namespace
middleware and stores the query's `name` in the socket context. `join(room)`
joins a room; `say(room, message)` sends `message(name, message)` to the other
sockets in that room. Its acknowledgement is `sent`. `inspect` acknowledges
namespace ID and rooms. The public `/news` namespace has an `echo` event and
keeps its own sockets and handlers. Server IDs have the form `/chat#<engine ID>`.
The 1.7.4 client exposes only the engine ID as `socket.id`; the 2.5.0 client
exposes the namespace ID.

Run from this directory:

```sh
go run . -addr 127.0.0.1:8000
```

The module uses the repository checkout through `replace`. The demo token is
checked in `main.go`; pass it as the client's `query.token`, together with
`query.name`. A client without that token receives `error: unauthorized` for
`/chat` while its Engine.IO connection remains open.

`client.js` checks authentication, two namespaces on one engine connection,
room isolation and exclusion of the sender over polling and websocket. With
`socket.io-client@1.7.4` or `socket.io-client@2.5.0` installed externally:

```sh
NODE_PATH=/path/to/node_modules node client.js http://127.0.0.1:8000
```

A successful run exits zero; connection failures, assertion failures and the
10 second deadline exit nonzero. From the repository root, the shared runner
builds each example and starts it on a free local port:

```sh
CONFORMANCE_NODE_MODULES=/path/to/node_modules make examples-node
```

See [the examples index](../README.md).

# Binary example

Binary events and acknowledgements in both directions using `*parser.Buffer` and Node `Buffer`.

`binary` acknowledges its binary argument unchanged. On connect, the server sends
`binary-question` with a buffer; the client acknowledges with another buffer,
which the server sends back as `binary-confirmed`.

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
exits nonzero. From the repository root, `CONFORMANCE_NODE_MODULES=/path/to/node_modules
make examples-node` builds and checks every executable example on a free local
port. See [the examples index](../README.md) for the other examples.

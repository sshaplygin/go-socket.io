# Node oracle for the Socket.IO v5 codec

`verify.mjs` checks the codec in `parser/` against the official
[`socket.io-parser@4.2.7`](https://www.npmjs.com/package/socket.io-parser/v/4.2.7), pinned with
its dependency hashes in `package-lock.json`. It drives the real Node `Encoder` and
`Decoder` (protocol version asserted to be 5) and a Go bridge, `main.go`, that calls
`parser.Encode` and `parser.Decode`:

- fourteen Node-origin messages (auth, namespaces, Unicode, numeric event names, ACK
  success and error arrays, IDs zero and 2^53-1, nested binary values, empty buffers,
  binary ACKs) are decoded and re-encoded by Go and must equal Node's bytes, then decode
  again in Node to the same value;
- three Go-origin packets are decoded by Node and compared with Node's encoding;
- eleven malformed messages must be rejected by both, seven are accepted by Node and
  rejected by Go on purpose (the list is in the package documentation), and six cover the
  byte, attachment-count and depth limits.

CI does not run it: it needs Node and npm. Run it from the repository root after a change
to the wire format:

```sh
npm ci --ignore-scripts --no-audit --no-fund --prefix parser/testdata/oracle
npm test --prefix parser/testdata/oracle
```

`node_modules/` is ignored. `go test ./parser` builds and runs the bridge once
(`TestOracleBridge`) so it cannot rot unnoticed.

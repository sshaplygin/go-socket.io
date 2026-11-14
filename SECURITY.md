# Security policy

This module has one maintainer. This file states which versions receive security fixes and
how to report a vulnerability privately.

## Supported versions

| Version | Security fixes |
| --- | --- |
| The latest v1.5.x release of `github.com/sshaplygin/go-socket.io` | Yes |
| v2 (the `v2/` module) | No release yet: it is pre-release, and reports are welcome |
| Earlier versions of this module, v0.9.1 through v1.4.2 | No; [go.mod](go.mod) retracts them |
| The archived [googollee/go-socket.io](https://github.com/googollee/go-socket.io) | No |

A fix is released as a new v1.5.x version. To get it, upgrade to the latest v1.5.x:

```sh
go get github.com/sshaplygin/go-socket.io@v1.5
```

## Reporting a vulnerability

Do not open a public issue for a vulnerability. Report it through
[GitHub private vulnerability reporting](https://github.com/sshaplygin/go-socket.io/security/advisories/new).
If you cannot use it, email [semyuon@gmail.com](mailto:semyuon@gmail.com).

Include:

- the affected version or commit;
- the transport: polling, websocket, or both;
- a reproduction, such as a minimal program or client script;
- the impact: what an attacker can achieve and with which configuration.

## Response targets

- Acknowledgement within 7 days.
- An initial assessment within 14 days.
- Coordinated disclosure with a target of 90 days from the report, or earlier once a fix is
  released.

A confirmed vulnerability is published as a GitHub Security Advisory, which can request a
CVE.

## Scope

In scope are the v1 library packages of this module: the Socket.IO server in the root
package, `client`, `engineio` and its sub-packages, `parser`, `adapter`, `adapter/redis`,
`logger`, and the internal packages they use. Denial of service by a remote client is in scope, for example unbounded
memory growth, a goroutine leak, or a panic triggered by crafted packets or handshakes over
polling or websocket.

Out of scope:

- the example applications in `_examples/` and `engineio/_examples/`;
- vulnerabilities in dependencies that have no exploitable path through this module;
  report those upstream;
- issues that exist only because a deployment widened a documented limit, such as
  `engineio.WithMaxHTTPBufferSize`, `socketio.WithWriteBufferSize` or
  `client.WithMaxMessageSize`, past what it can hold.

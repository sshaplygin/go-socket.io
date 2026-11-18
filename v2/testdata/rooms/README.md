# Node room-selection corpus for the memory adapter

`rooms.json` records 22 cases captured from the Node in-memory `Adapter` of
[`socket.io-adapter@2.5.5`](https://www.npmjs.com/package/socket.io-adapter/v/2.5.5), with
packets encoded by `socket.io-parser@4.2.7`; `package-lock.json` pins the whole Node
dependency tree. Each case replays membership actions (`connect`, `addAll`, `del`, `delAll`,
`unregister`) and records the resulting `rooms`/`sids` maps and live socket IDs after every
action, `sockets` queries, `socketRooms` results (`null` for an unknown ID) and the
recipients of one broadcast with its `rooms`, `except` and `flags`. A `connect` action
registers a live socket and calls `addAll` with its own ID room plus the listed rooms, as a
Socket.IO connection does; `unregister` removes the live socket and keeps its membership.
Sets and recipient lists are sorted; no Go ordering is implied.

`TestMemoryAdapterNodeRooms` (`../../adapter_rooms_test.go`) replays every case through the
Go memory adapter against a `LocalSockets` double and compares membership after each action,
the queries, the broadcast recipients and `FetchSockets`. Lifecycle events
(`create-room`, `join-room`, ...) and the `volatile`/`compress` flags are recorded by Node
but have no Go counterpart and are not compared. One intended difference: Node skips a
recorded socket that is not live without a report, while the Go adapter tries it and
returns an error matching `ErrSocketClosed` beside the same recipients.

CI does not run Node. After a change to the cases or the pinned versions, run from `v2/`:

```sh
npm ci --ignore-scripts --no-audit --no-fund --prefix testdata/rooms
npm test --prefix testdata/rooms          # compares with rooms.json byte for byte
npm run capture --prefix testdata/rooms   # rewrites rooms.json after a reviewed change
```

`node_modules/` is ignored. The capture creates no listener and accepts no network input.

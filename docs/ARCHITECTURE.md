# V1 architecture

The public packages separate Socket.IO, Engine.IO, codecs and adapters. The table
lists allowed direct imports within the module; standard-library and external
imports are omitted. Test imports do not define runtime dependencies.

| Package | Responsibility | May import |
| --- | --- | --- |
| root `socketio` | Public server contracts, server and namespace lifecycle, adapter factories, connection queues, packet dispatch, listeners and options | `adapter`, `parser`, `engineio`, `engineio/parser`, `logger`, `internal/event` |
| `client` | Public client contracts, persistent namespace sockets, connection generations, queues, packet dispatch, listeners, retries and options | `parser`, `engineio/client`, `engineio/parser`, `engineio/transport`, its polling/websocket implementations, `logger`, `internal/event` |
| `internal/event` | Typed payload/schema/handler bridge, ACK envelope and common typed errors; no role ownership | `parser` |
| `adapter` | Room/index/broadcast contract and in-memory implementation | no other repository runtime package |
| `adapter/redis` | Redis adapter extending/composing the memory adapter | `adapter`, `logger`, `parser` |
| `parser` | Socket.IO packet codec, shared by server and client | `logger` |
| `engineio` | Engine.IO acceptance/server lifecycle, public options/hooks and session coordination | `engineio/internal/session`, `engineio/parser`, `engineio/transport`, its polling/websocket implementations, `logger` |
| `engineio/internal/session` | Server session I/O, registry/IDs, heartbeat, close/upgrade and polling response observation | `engineio/parser`, `engineio/transport`, `engineio/transport/polling`, `logger` |
| `engineio/client` | Engine.IO dial, heartbeat, upgrade and client connection | `engineio/parser`, `engineio/transport`, its polling/websocket implementations, `logger` |
| `engineio/parser` | Public Engine.IO codec entry points and OPEN parameters | `engineio/parser/internal/packet`, `engineio/parser/internal/payload` |
| `engineio/parser/internal/packet` | Canonical frame/packet types and packet encoder/decoder | `logger` |
| `engineio/parser/internal/payload` | Streaming payload encoder/decoder, framing lengths and lease/error state | `engineio/parser/internal/packet` |
| `engineio/transport` | Transport contracts and registry | `engineio/parser`, `logger` |
| `engineio/transport/polling` | HTTP polling, streaming feed/flush, pause/deadline ownership | `engineio/transport`, `engineio/parser`, `logger` |
| `engineio/transport/websocket` | WebSocket transport | `engineio/transport`, `engineio/parser`, `logger` |
| `logger` | Logging fallback | no other repository runtime package |

The server and client have independent connection contracts. Neither client
imports its server. Codecs do not depend on roles, sessions, transports or adapters.
Redis composes the memory adapter; the root server does not import Redis.
`internal/event` shares typed payload and ACK encoding between the Socket.IO roles.

`make architecture-check` verifies direct imports, transitive role and codec
boundaries, package privacy and the absence of unsupported import paths.

## Runtime

The Engine.IO server accepts HTTP requests and yields sessions to Socket.IO.
Each server Socket.IO connection has a read dispatcher, a header reader and a
writer. A separate observer cancels typed handlers when the engine closes, even
if the dispatcher is inside a blocking application handler; Engine.IO removes closed
sessions through a completion callback, without an idle registry watcher. The server error worker starts
only when a report needs delivery. The client role retains its error worker.
The server uses one ordered outbound queue with an integer capacity; there is no
unused buffered delivery channel. Incoming event handlers run on the dispatcher;
blocking one also blocks the header worker's handoff for that connection.
Application-initiated disconnection can invoke lifecycle callbacks concurrently.
The client manager owns namespace sockets, connection generations, retries and
pre-connect buffering. Queues and ACK registrations belong to their connection.

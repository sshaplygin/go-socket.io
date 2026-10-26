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
Each server Socket.IO connection has up to five workers: a header reader, a
read dispatcher, a writer, an observer that cancels typed handlers when the
engine closes, and an error worker that starts on the first `OnError` delivery
or outbound queue overflow. The writer drains one ordered outbound queue whose
capacity is the write buffer size. Engine.IO removes a closed session from its
registry in the session's close path, without a per-session watcher. Incoming
event handlers run on the dispatcher; blocking one stalls that connection and
also blocks the header reader's handoff of the next packet. A client connection
starts its error worker with the connection.
Application-initiated disconnection can invoke lifecycle callbacks concurrently.
The client manager owns namespace sockets, connection generations, retries and
pre-connect buffering. Queues and ACK registrations belong to their connection.

### Connection close and drain

Socket.IO orders close transitions and outbound admission under the connection's
`mu`. Its explicit states are open, closing, draining, sealed, discarded and closed.
An application close follows draining → sealed; a failure can move any active phase
to discarded. Completion moves to closed. The `draining` flag retains the origin of
an application close for callbacks even after a forced discard. The first close
closes `closing`, removes its namespace generations from
admission and cancels their typed-handler contexts. Later closes are idempotent.
A retained namespace connection cannot send to a replacement generation.

An application `Conn.Close` starts an orderly drain and its deadline. Namespace
cleanup queues DISCONNECT, emits `disconnecting` while rooms are visible, removes
rooms, then calls `OnDisconnect`. Sends admitted during those close callbacks can
join the drain. Namespace admission or room operations already in progress may
defer cleanup; their claimed control packets must finish before sealing.

After the close's callbacks return, application admission is sealed (`appSealed`).
When all claimed control packets have been queued, `seal` tells the writer there
will be no more drain packets. The writer empties the queue, then closes the
engine. The public Close does not wait for that transport drain.

A transport/dispatch failure, queue overflow or drain timeout closes `discard`
and aborts Engine.IO, abandoning queued packets. This can cut short an orderly
drain. After successful connection setup, the read dispatcher completes pending
namespace callbacks and closes `done`; failed initial setup finishes close
synchronously. Server shutdown also waits for all disconnects claimed before close.

### Client transport probe and upgrade

The Engine.IO client dials a candidate transport with the current session ID and
applies the message limit before probe I/O. It exchanges PING/PONG carrying
`probe` under a deadline. The current transport remains active during the probe.
After the matching PONG, the client holds its write gate and pauses the current
transport so polling operations can finish before the switch.

Before commit, a failed or canceled probe closes the candidate and resumes the
current transport before releasing the write gate. During commit it sends
UPGRADE on the candidate, installs the connection and heartbeat deadlines under
the client mutex, then closes the old transport. A failure after commit starts
terminates the connection because transport ownership may already have changed.
The candidate and old connection are closed by the paths that own them; an
abandoned late dial result is closed rather than installed.

Tests live beside their implementation. Public package tests exercise consumers;
tests in those packages also cover private state and failure paths. Compile fixtures in
`internal/parity` check role separation, public contracts and canonical `go doc`
method/field lookup. Runtime helpers remain unexported in their owning public
package. Node interoperability
fixtures live in `internal/conformance` behind the `conformance` build tag.

### Client upgrade cancellation

The client dials the candidate transport, exchanges the probe PING/PONG, then
holds its write gate while pausing the current transport and committing UPGRADE.
The built-in polling connection offers a cancellable pause: canceling the probe
also wakes an in-progress pause, including one whose goroutine has not started.
Rollback waits for that pause to finish and calls Resume once before releasing
the write gate. Legacy custom transports implementing only Pause/Resume retain
the retry fallback because their pause cannot observe cancellation.

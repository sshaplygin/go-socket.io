package engineio

import (
	"net/http"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// Hooks are the engine events as observer callbacks, the shape of v2/engineio/hooks.go
// without its contexts (D13, D16). Connection is engine.io 3.6.2's connection event and
// Headers its transports' headers event; 3.6.2 has no InitialHeaders or ConnectionError:
// they are the D16 choice from the owner's list, named after the later engine.io's
// initial_headers and connection_error and defined over the 3.6.2 behaviour they observe
// (ConnectionError carries 3.6.2's codes and messages). The socket-level fields are
// 3.6.2's socket events, except Error, which reports the cause 3.6.2 passes as the
// description of its close event. A nil field is ignored; a set
// one is called synchronously on the goroutine of the event. Header values are
// borrowed for the call: a hook that keeps them copies them. Options.Hooks holds one;
// nil means no observer.
type Hooks struct {
	// Connection fires once per session, after its handshake response and before
	// Accept returns it; c.ReadyState() is StateOpen in the hook.
	Connection func(c Conn)
	// InitialHeaders fires once per session with the handshake request and the
	// headers of its response, before they are written; a hook may add to h.
	InitialHeaders func(r *http.Request, h http.Header)
	// Headers fires with every response of a session (each polling response, the
	// websocket upgrade response once), after InitialHeaders on the handshake, before
	// the headers are written; V1-15's cookie is in h.
	Headers func(r *http.Request, h http.Header)
	// ConnectionError fires when ServeHTTP rejects a request, with the engine.io 3.6.2
	// code and message (0 Transport unknown, 1 Session ID unknown, 2 Bad handshake
	// method, 3 Bad request, 4 Forbidden) and, for a request checker rejection, its
	// error.
	ConnectionError func(r *http.Request, code int, message string, err error)

	// Packet fires for every packet read from the client, PacketCreate for every
	// packet about to be written, each with the packet and frame types.
	Packet       func(c Conn, pt packet.Type, ft frame.Type)
	PacketCreate func(c Conn, pt packet.Type, ft frame.Type)
	// Heartbeat fires when a PING is received, before the PONG is written.
	Heartbeat func(c Conn)
	// Upgrading fires when the probe is accepted, Upgrade when the transport has
	// switched; transport is the name of the new one.
	Upgrading func(c Conn, transport string)
	Upgrade   func(c Conn, transport string)
	// Flush fires when a polling write batch starts, Drain when it has been written.
	Flush func(c Conn)
	Drain func(c Conn)
	// Error fires with the transport error that closes the session (V1-4's cause
	// transport error), before the close; a peer close or a ping timeout fires nothing.
	Error func(c Conn, err error)
}

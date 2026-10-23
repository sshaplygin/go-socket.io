// Package client implements a Go Socket.IO protocol v4 client over Engine.IO v3.
//
// New configures a client; Connect opens its connection. Register handlers before
// connecting, and keep the client open until the application has finished:
//
//	cl, err := client.New("http://127.0.0.1:8000")
//	if err != nil {
//		return err
//	}
//	defer cl.Close()
//	cl.OnConnect(func(c client.Conn) error { return nil })
//	if err := cl.Connect(); err != nil {
//		return err
//	}
//	replies := make(chan string, 1)
//	cl.Emit("echo", "hello", func(value string) { replies <- value })
//	select {
//	case value := <-replies:
//		fmt.Println(value)
//	case <-time.After(5 * time.Second):
//		return errors.New("echo acknowledgement timed out")
//	}
//
// Connect returns when Engine.IO is open and namespace CONNECT packets are queued;
// OnConnect reports namespace readiness. Events emitted before readiness are
// buffered. The peer must handle echo and return a value for the ACK above.
// Callbacks run on the connection dispatcher: do not block one while waiting for
// another event or ACK on the same connection. Close disconnects all namespaces
// and drains queued packets within the configured close timeout.
//
// Typed Event and AckEvent descriptors are defined in the root socketio package,
// so programs using typed client events import that package as well as client.
// The client runtime itself does not import the server. Client and Socket methods
// RegisterEvent, SendEvent and RequestAck are internal descriptor bridges, not an
// application extension API; use socketio.Event and socketio.AckEvent instead.
package client

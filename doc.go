// Package socketio implements a Socket.IO protocol v4 server over Engine.IO v3.
//
// Register handlers before starting Serve and mount the server as an HTTP handler:
//
//	server := socketio.NewServer()
//	defer server.Close()
//	server.OnConnect("/", func(c socketio.Conn) error { return nil })
//	server.OnEvent("/", "echo", func(c socketio.Conn, value string) string {
//		return value // Sent as an ACK when the client requests one.
//	})
//	http.Handle("/socket.io/", server)
//	go func() {
//		if err := server.Serve(); err != nil {
//			log.Print(err)
//		}
//	}()
//	// Run the application's HTTP server and wait for shutdown here.
//
// Serve must run for connections and events to reach their handlers. HTTP
// handshakes can succeed without Serve, with no error reported to the client.
// Close releases Socket.IO sessions and adapters; the application separately
// owns HTTP server startup and shutdown. See ExampleNewServer for HTTP startup
// and the client package for connecting and sending events with acknowledgements.
package socketio
